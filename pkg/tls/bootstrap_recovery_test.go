/*
Copyright 2026.

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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func assertIntermediateBootstrap(t *testing.T, result Result, watch bool) {
	t.Helper()
	if result.APIAvailable != watch || result.AdherenceFetched || result.TLSAdherence != TLSAdherenceNoOpinion || result.ProfileSpec == nil || result.ProfileSpec.Type != configv1.TLSProfileIntermediateType {
		t.Fatalf("incorrect fallback state: %#v", result)
	}
	config := &tls.Config{}
	for _, opt := range result.TLSOpts {
		opt(config)
	}
	want, err := ResolveProxyTLSArguments(result.ProfileSpec, TLSAdherenceNoOpinion, nil, fipsModeEnabled())
	if err != nil {
		t.Fatal(err)
	}
	if config.MinVersion != tls.VersionTLS12 || !slices.Equal(config.NextProtos, []string{"h2", "http/1.1"}) || !reflect.DeepEqual(result.ProxyArgs, want) {
		t.Fatalf("unvalidated Intermediate defaults: %#v / %#v", config, result.ProxyArgs)
	}
}

func TestBootstrapClassifiesReadErrors(t *testing.T) {
	resource := schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}
	for _, transient := range []error{
		apierrors.NewServiceUnavailable("temporary"), apierrors.NewTooManyRequests("throttled", 0),
		apierrors.NewTimeoutError("timeout", 0), apierrors.NewServerTimeout(resource, "get", 0), context.DeadlineExceeded,
	} {
		t.Run(transient.Error(), func(t *testing.T) {
			result, err := resolveTLSProfileState(profileState{}, fmt.Errorf("retry budget exhausted: %w", transient))
			if err != nil {
				t.Fatal(err)
			}
			assertIntermediateBootstrap(t, result, true)
		})
	}
	for _, absent := range []error{apierrors.NewNotFound(resource, "cluster"), &apimeta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{Group: "config.openshift.io", Resource: "apiservers"}}} {
		result, err := resolveTLSProfileState(profileState{}, absent)
		if err != nil {
			t.Fatal(err)
		}
		assertIntermediateBootstrap(t, result, false)
	}
	for _, fatal := range []error{
		apierrors.NewForbidden(resource, "cluster", errors.New("denied")), apierrors.NewUnauthorized("denied"),
		errors.New("decode failure"), context.Canceled, errors.Join(context.Canceled, apierrors.NewServiceUnavailable("previous attempt")),
	} {
		result, err := resolveTLSProfileState(profileState{}, fatal)
		if !errors.Is(err, fatal) || !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("fatal error %v was swallowed: %#v, %v", fatal, result, err)
		}
	}
}

func TestDegradedBootstrapWatcherRecovery(t *testing.T) {
	for _, tt := range []struct {
		name, profile, adherence string
		restart                  bool
	}{
		{"implicit Intermediate", ``, "", false},
		{"explicit Intermediate", `"tlsSecurityProfile":{"type":"Intermediate"},`, "", false},
		{"adherence only", `"tlsSecurityProfile":{"type":"Intermediate"},`, TLSAdherenceStrictAllComponents, true},
		{"Modern strict", `"tlsSecurityProfile":{"type":"Modern"},`, TLSAdherenceStrictAllComponents, true},
		{"Custom strict", `"tlsSecurityProfile":{"type":"Custom","custom":{"minTLSVersion":"VersionTLS12","ciphers":["ECDHE-RSA-AES128-GCM-SHA256"]}},`, TLSAdherenceStrictAllComponents, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var recovered atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if !recovered.Load() {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"apiVersion":"config.openshift.io/v1","kind":"APIServer","metadata":{"name":"cluster"},"spec":{%s"tlsAdherence":%q}}`, tt.profile, tt.adherence)
			}))
			defer server.Close()
			cfg := &rest.Config{Host: server.URL}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			initial, err := Resolve(ctx, cfg)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			assertIntermediateBootstrap(t, initial, true)
			restarted := false
			watcher := NewProfileWatcherWithAdherence(nil, initial.ProfileSpec, initial.TLSAdherence, func() { restarted = true })
			apiClient, err := dynamic.NewForConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			watcher.readProfileState = func(ctx context.Context) (profileState, error) { return readTLSProfileState(ctx, apiClient) }
			// Use a fresh runtime context, not the expired bootstrap context.
			result, err := watcher.Reconcile(context.Background(), reconcile.Request{})
			if err != nil || result.RequeueAfter != profileRetryInterval || restarted {
				t.Fatalf("transient runtime recovery: %#v %v", result, err)
			}
			recovered.Store(true)
			result, err = watcher.Reconcile(context.Background(), reconcile.Request{})
			if err != nil || result.RequeueAfter != 0 || restarted != tt.restart {
				t.Fatalf("recovery restart=%v want=%v result=%#v err=%v", restarted, tt.restart, result, err)
			}
			resolved, err := Resolve(context.Background(), cfg)
			if err != nil || !resolved.AdherenceFetched {
				t.Fatalf("recovered adherence not observed: %#v %v", resolved, err)
			}
		})
	}
}

func TestWatcherNormalizesIntermediateProfiles(t *testing.T) {
	for _, initial := range []*configv1.TLSSecurityProfile{nil, {}, {Type: configv1.TLSProfileIntermediateType}} {
		for _, current := range []*configv1.TLSSecurityProfile{nil, {}, {Type: configv1.TLSProfileIntermediateType}} {
			called := false
			watcher := NewProfileWatcherWithAdherence(nil, initial, TLSAdherenceNoOpinion, func() { called = true })
			watcher.readProfileState = func(context.Context) (profileState, error) { return profileState{profile: current}, nil }
			mustReconcile(t, watcher)
			if called {
				t.Fatalf("equivalent Intermediate states caused restart: %#v -> %#v", initial, current)
			}
		}
	}
}
