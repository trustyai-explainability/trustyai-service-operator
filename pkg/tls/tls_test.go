/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tls

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
)

func TestResolveDegradesOnTransientAPIServerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	profile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	lastKnownGood, err := ResolveProxyTLSArguments(profile, TLSAdherenceStrictAllComponents, nil, false)
	if err != nil {
		t.Fatalf("ResolveProxyTLSArguments() returned unexpected error: %v", err)
	}
	previous := CurrentProxyTLSArguments()
	defer SetProxyTLSArguments(previous)
	for _, tt := range []struct {
		name string
		args ProxyTLSArguments
	}{
		{name: "cold start with process defaults", args: mustDefaultProxyTLSArguments(fipsModeEnabled())},
		{name: "previously published strict policy", args: lastKnownGood},
	} {
		t.Run(tt.name, func(t *testing.T) {
			SetProxyTLSArguments(tt.args)
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			result, err := Resolve(ctx, &rest.Config{Host: server.URL})
			if err != nil {
				t.Fatalf("Resolve() error = %v, want validated fallback", err)
			}
			assertIntermediateBootstrap(t, result, true)
			if !reflect.DeepEqual(CurrentProxyTLSArguments(), tt.args) {
				t.Fatal("Resolve() changed published arguments on failure")
			}
		})
	}
}

func TestResolveRetriesThenUsesStrictModernPolicy(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"config.openshift.io/v1","kind":"APIServer","metadata":{"name":"cluster"},"spec":{"tlsSecurityProfile":{"type":"Modern"},"tlsAdherence":"StrictAllComponents"}}`))
	}))
	defer server.Close()

	previous := CurrentProxyTLSArguments()
	result, err := Resolve(context.Background(), &rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 || !result.APIAvailable || !result.AdherenceFetched || result.TLSAdherence != TLSAdherenceStrictAllComponents {
		t.Fatalf("unexpected resolved state after %d requests: %#v", attempts.Load(), result)
	}
	cfg := &tls.Config{}
	for _, opt := range result.TLSOpts {
		opt(cfg)
	}
	wantArgs, err := ResolveProxyTLSArguments(result.ProfileSpec, TLSAdherenceStrictAllComponents, nil, fipsModeEnabled())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinVersion != tls.VersionTLS13 || !reflect.DeepEqual(result.ProxyArgs, wantArgs) {
		t.Fatalf("resolved configuration weakened Strict/Modern: %#v, %#v", cfg, result.ProxyArgs)
	}
	if !reflect.DeepEqual(CurrentProxyTLSArguments(), previous) {
		t.Fatal("Resolve() must leave publishing to the caller after successful resolution")
	}
}

func TestReadTLSProfileStateWithRetry(t *testing.T) {
	for _, transient := range []error{
		apierrors.NewServiceUnavailable("unavailable"),
		apierrors.NewTooManyRequests("throttled", 0),
		apierrors.NewTimeoutError("timeout", 0),
		apierrors.NewServerTimeout(schema.GroupResource{Resource: "apiservers"}, "get", 0),
		context.DeadlineExceeded,
	} {
		t.Run(transient.Error(), func(t *testing.T) {
			for _, recover := range []bool{false, true} {
				attempts := 0
				want := profileState{profile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}, adherence: TLSAdherenceStrictAllComponents}
				state, err := readTLSProfileStateWithRetry(context.Background(), func(context.Context) (profileState, error) {
					attempts++
					if recover && attempts == 2 {
						return want, nil
					}
					return profileState{}, transient
				}, wait.Backoff{Steps: 3})
				if recover {
					if err != nil || attempts != 2 || !reflect.DeepEqual(state, want) {
						t.Fatalf("recovery: attempts=%d state=%#v err=%v", attempts, state, err)
					}
				} else if !errors.Is(err, transient) || attempts != 3 || !reflect.DeepEqual(state, profileState{}) {
					t.Fatalf("exhaustion: attempts=%d state=%#v err=%v", attempts, state, err)
				}
			}
		})
	}
}

func TestReadTLSProfileStateDoesNotRetryPermanentErrors(t *testing.T) {
	for _, permanent := range []error{
		apierrors.NewForbidden(schema.GroupResource{Resource: "apiservers"}, "cluster", errors.New("denied")),
		apierrors.NewUnauthorized("denied"),
		apierrors.NewNotFound(schema.GroupResource{Resource: "apiservers"}, "cluster"),
		errors.New("invalid profile"),
		context.Canceled,
	} {
		attempts := 0
		_, err := readTLSProfileStateWithRetry(context.Background(), func(context.Context) (profileState, error) {
			attempts++
			return profileState{}, permanent
		}, wait.Backoff{Steps: 3})
		if !errors.Is(err, permanent) || attempts != 1 {
			t.Fatalf("permanent error %v: attempts=%d err=%v", permanent, attempts, err)
		}
	}
}

func TestResolveNotFoundUsesNonOpenShiftDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	result, err := Resolve(context.Background(), &rest.Config{Host: server.URL})
	if err != nil || result.APIAvailable {
		t.Fatalf("non-OpenShift fallback: result=%#v err=%v", result, err)
	}
	cfg := &tls.Config{}
	for _, opt := range result.TLSOpts {
		opt(cfg)
	}
	if cfg.MinVersion != tls.VersionTLS12 || len(result.ProxyArgs.Args) == 0 {
		t.Fatalf("missing hardened defaults: %#v, %#v", cfg, result.ProxyArgs)
	}
}

func TestTLSOptsForProfile(t *testing.T) {
	tests := []struct {
		name           string
		profile        *configv1.TLSSecurityProfile
		wantMinVersion uint16
	}{
		{
			name:           "nil profile returns Intermediate defaults",
			profile:        nil,
			wantMinVersion: tls.VersionTLS12,
		},
		{
			name:           "empty profile returns Intermediate defaults",
			profile:        &configv1.TLSSecurityProfile{},
			wantMinVersion: tls.VersionTLS12,
		},
		{
			name: "Intermediate returns TLS 1.2",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			wantMinVersion: tls.VersionTLS12,
		},
		{
			name: "Modern returns TLS 1.3",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			},
			wantMinVersion: tls.VersionTLS13,
		},
		{
			name: "Old honors TLS 1.0",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileOldType,
			},
			wantMinVersion: tls.VersionTLS10,
		},
		{
			name: "Custom honors TLS 1.1",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{
						MinTLSVersion: configv1.VersionTLS11,
					},
				},
			},
			wantMinVersion: tls.VersionTLS11,
		},
		{
			name: "Custom empty minimum version defaults to TLS 1.2",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: "",
				}},
			},
			wantMinVersion: tls.VersionTLS12,
		},
		{
			name: "Unknown type falls back to Intermediate",
			profile: &configv1.TLSSecurityProfile{
				Type: "SuperSecure",
			},
			wantMinVersion: tls.VersionTLS12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := tlsOptsForProfile(tt.profile, TLSAdherenceNoOpinion)
			if err != nil {
				t.Fatalf("tlsOptsForProfile() returned unexpected error: %v", err)
			}

			cfg := &tls.Config{}
			for _, opt := range opts {
				opt(cfg)
			}

			if cfg.MinVersion != tt.wantMinVersion {
				t.Errorf("MinVersion = %d, want %d", cfg.MinVersion, tt.wantMinVersion)
			}
			if len(cfg.NextProtos) != 2 || cfg.NextProtos[0] != "h2" || cfg.NextProtos[1] != "http/1.1" {
				t.Errorf("NextProtos = %v, want [h2 http/1.1]", cfg.NextProtos)
			}
		})
	}
}

func TestTLSOptsForProfileOldSuiteSurvival(t *testing.T) {
	opts, err := tlsOptsForProfile(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType}, TLSAdherenceNoOpinion)
	if err != nil {
		t.Fatalf("tlsOptsForProfile() returned unexpected error: %v", err)
	}
	cfg := &tls.Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	// The Old profile's 3DES and RSA key-exchange suites only appear in
	// tls.InsecureCipherSuites(); their presence proves the usable-cipher
	// lookup includes them instead of silently dropping Old's suites.
	if !slices.Contains(cfg.CipherSuites, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA) {
		t.Errorf("Old cipher list = %v, want the 3DES suite present", cfg.CipherSuites)
	}
}

func TestTLSOptsForProfileRejectsNilCustomProfile(t *testing.T) {
	_, err := tlsOptsForProfile(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType}, TLSAdherenceNoOpinion)
	if err == nil {
		t.Fatal("tlsOptsForProfile() expected an error for a nil custom profile")
	}
}

func TestTLSOptsForProfileDropsUnsupportedCiphers(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
			Ciphers: []string{
				"ECDHE-ECDSA-AES128-GCM-SHA256",
				"UNSUPPORTED-CIPHER",
			},
		}},
	}

	opts, err := tlsOptsForProfile(profile, TLSAdherenceNoOpinion)
	if err != nil {
		t.Fatalf("tlsOptsForProfile() returned unexpected error: %v", err)
	}
	cfg := &tls.Config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if len(cfg.CipherSuites) != 1 || cfg.CipherSuites[0] != tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 {
		t.Errorf("CipherSuites = %v, want supported cipher only", cfg.CipherSuites)
	}
}

func TestTLSOptsForProfileDropsRC4Ciphers(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
			Ciphers: []string{
				"TLS_RSA_WITH_RC4_128_SHA",
				"TLS_ECDHE_RSA_WITH_RC4_128_SHA",
				"TLS_ECDHE_ECDSA_WITH_RC4_128_SHA",
				"ECDHE-ECDSA-AES128-GCM-SHA256",
			},
		}},
	}

	opts, err := tlsOptsForProfile(profile, TLSAdherenceNoOpinion)
	if err != nil {
		t.Fatalf("tlsOptsForProfile() returned unexpected error: %v", err)
	}
	cfg := &tls.Config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if len(cfg.CipherSuites) != 1 || cfg.CipherSuites[0] != tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 {
		t.Errorf("CipherSuites = %v, want the RC4 entries dropped and the usable suite kept", cfg.CipherSuites)
	}

	for _, cipher := range []string{
		"TLS_RSA_WITH_RC4_128_SHA",
		"TLS_ECDHE_RSA_WITH_RC4_128_SHA",
		"TLS_ECDHE_ECDSA_WITH_RC4_128_SHA",
	} {
		rc4Only := &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{cipher},
			}},
		}
		if _, err := tlsOptsForProfile(rc4Only, TLSAdherenceNoOpinion); err == nil {
			t.Errorf("tlsOptsForProfile() accepted all-RC4 cipher %q", cipher)
		}
	}
}

func TestTLSOptsForProfileRetainsImplementedCustomCiphers(t *testing.T) {
	// Custom profiles may request any cipher implemented by the runtime. Built-in
	// profile membership is not a valid proxy for runtime support.
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
			Ciphers: []string{
				"TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA",
				"ECDHE-ECDSA-AES128-GCM-SHA256",
			},
		}},
	}

	opts, err := tlsOptsForProfile(profile, TLSAdherenceNoOpinion)
	if err != nil {
		t.Fatalf("tlsOptsForProfile() returned unexpected error: %v", err)
	}
	cfg := &tls.Config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if !slices.Equal(cfg.CipherSuites, []uint16{tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}) {
		t.Errorf("CipherSuites = %v, want both runtime-supported custom suites", cfg.CipherSuites)
	}

	threeDESOnly := profile.DeepCopy()
	threeDESOnly.Custom.Ciphers = []string{"TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA"}
	opts, err = tlsOptsForProfile(threeDESOnly, TLSAdherenceNoOpinion)
	if err != nil {
		t.Fatalf("tlsOptsForProfile() rejected a runtime-supported 3DES cipher: %v", err)
	}
	cfg = &tls.Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	if !slices.Equal(cfg.CipherSuites, []uint16{tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA}) {
		t.Errorf("CipherSuites = %v, want the runtime-supported 3DES suite", cfg.CipherSuites)
	}
}

func TestTLSOptsForProfileRejectsUnusableCiphers(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
			Ciphers:       []string{"TLS_AES_128_GCM_SHA256", "UNSUPPORTED-CIPHER"},
		}},
	}

	_, err := tlsOptsForProfile(profile, TLSAdherenceNoOpinion)
	if err == nil {
		t.Fatal("tlsOptsForProfile() expected an error when no requested cipher is usable")
	}
	if !strings.Contains(err.Error(), "all 2 ciphers in TLS profile") || !strings.Contains(err.Error(), "excluded by policy") {
		t.Errorf("error = %q, want all-unusable cipher count", err)
	}
}

func TestTLSOptsForProfileEmptyCiphersFollowAdherence(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
		}},
	}

	for _, tt := range []struct {
		name      string
		adherence string
		wantErr   bool
	}{
		{name: "no opinion remains lenient", adherence: TLSAdherenceNoOpinion},
		{name: "legacy remains lenient", adherence: TLSAdherenceLegacyAdheringComponentsOnly},
		{name: "strict rejects", adherence: TLSAdherenceStrictAllComponents, wantErr: true},
		{name: "unknown adherence rejects", adherence: "FutureValue", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tlsOptsForProfile(profile, tt.adherence)
			if (err != nil) != tt.wantErr {
				t.Fatalf("tlsOptsForProfile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	profile.Custom.MinTLSVersion = configv1.VersionTLS13
	if _, err := tlsOptsForProfile(profile, TLSAdherenceStrictAllComponents); err != nil {
		t.Fatalf("TLS 1.3 profile with an empty cipher list returned an error: %v", err)
	}
}

func TestTLSOptsForProfileRejectsInvalidMinimumVersion(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: "VersionTLS99",
		}},
	}

	if _, err := tlsOptsForProfile(profile, TLSAdherenceNoOpinion); err == nil {
		t.Fatal("tlsOptsForProfile() expected an error for an invalid minimum TLS version")
	}
}

func TestTLSOptsForProfileAppliesSupportedRestrictedProfile(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS12,
			Ciphers: []string{
				"ECDHE-ECDSA-AES128-GCM-SHA256",
				"TLS_AES_128_GCM_SHA256",
				"UNSUPPORTED-CIPHER",
			},
		}},
	}

	opts, err := tlsOptsForProfile(profile, TLSAdherenceNoOpinion)
	if err != nil {
		t.Fatalf("tlsOptsForProfile() returned unexpected error: %v", err)
	}
	cfg := &tls.Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d", cfg.MinVersion, tls.VersionTLS12)
	}
	if len(cfg.CipherSuites) != 1 || cfg.CipherSuites[0] != tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 {
		t.Errorf("CipherSuites = %v, want only the supported requested suite", cfg.CipherSuites)
	}

	modern := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS13,
			Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
		}},
	}
	modernOpts, err := tlsOptsForProfile(modern, TLSAdherenceNoOpinion)
	if err != nil {
		t.Fatalf("tlsOptsForProfile(Modern) returned unexpected error: %v", err)
	}
	modernConfig := &tls.Config{}
	for _, opt := range modernOpts {
		opt(modernConfig)
	}
	if modernConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf("Modern MinVersion = %d, want %d", modernConfig.MinVersion, tls.VersionTLS13)
	}
	if modernConfig.CipherSuites != nil {
		t.Errorf("Modern CipherSuites = %v, want nil so Go manages TLS 1.3 suites", modernConfig.CipherSuites)
	}
}

func TestResolveRejectsUnauthorizedAPIServerReads(t *testing.T) {
	tests := []struct {
		name   string
		code   int
		reason metav1.StatusReason
		isErr  func(error) bool
	}{
		{name: "unauthorized", code: http.StatusUnauthorized, reason: metav1.StatusReasonUnauthorized, isErr: apierrors.IsUnauthorized},
		{name: "forbidden", code: http.StatusForbidden, reason: metav1.StatusReasonForbidden, isErr: apierrors.IsForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.code)
				_ = json.NewEncoder(w).Encode(metav1.Status{
					TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
					Status:   metav1.StatusFailure,
					Reason:   tt.reason,
					Code:     int32(tt.code),
					Message:  "denied",
				})
			}))
			defer server.Close()

			result, err := Resolve(context.Background(), &rest.Config{Host: server.URL})
			if err == nil {
				t.Fatal("Resolve() expected an authorization error")
			}
			if !tt.isErr(err) {
				t.Errorf("Resolve() error = %v, want wrapped %s status", err, tt.reason)
			}
			if result.TLSOpts != nil {
				t.Errorf("Resolve() returned fallback TLS options after authorization failure: %v", result.TLSOpts)
			}
		})
	}
}
