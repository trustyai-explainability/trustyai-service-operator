package tlsprofile

import (
	"context"
	"crypto/tls"
	"errors"
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func resolvedConfig(t *testing.T, profile *configv1.TLSSecurityProfile, adherence string, force bool) *tls.Config {
	t.Helper()
	opts, err := optionsForProfile(profile, adherence, force)
	if err != nil {
		t.Fatalf("optionsForProfile() error = %v", err)
	}
	config := &tls.Config{}
	for _, option := range opts {
		option(config)
	}
	return config
}

func TestOptionsForProfileAdherence(t *testing.T) {
	modern := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	for _, adherence := range []string{AdherenceNoOpinion, AdherenceLegacy} {
		opts, err := optionsForProfile(modern, adherence, false)
		if err != nil || len(opts) != 0 {
			t.Fatalf("adherence %q: options=%d err=%v, want component defaults", adherence, len(opts), err)
		}
	}
	if got := resolvedConfig(t, nil, AdherenceStrict, false).MinVersion; got != tls.VersionTLS12 {
		t.Fatalf("nil strict profile minimum = %d, want Intermediate TLS 1.2", got)
	}
	if got := resolvedConfig(t, modern, "FuturePolicy", false).MinVersion; got != tls.VersionTLS13 {
		t.Fatalf("unknown adherence minimum = %d, want strict Modern TLS 1.3", got)
	}
}

func TestOptionsForProfileValidationAndCipherFiltering(t *testing.T) {
	tests := []struct {
		name    string
		profile *configv1.TLSSecurityProfile
	}{
		{name: "unknown type", profile: &configv1.TLSSecurityProfile{Type: "future-profile"}},
		{name: "custom profile absent", profile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType}},
		{name: "empty minimum", profile: &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType, Custom: &configv1.CustomTLSProfile{},
		}},
		{name: "unknown minimum", profile: &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: "VersionTLS99",
			}},
		}},
		{name: "empty TLS 1.2 ciphers", profile: &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
			}},
		}},
		{name: "unusable ciphers", profile: &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
			}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := optionsForProfile(tt.profile, AdherenceStrict, false); err == nil {
				t.Fatal("optionsForProfile() accepted an invalid profile")
			}
		})
	}

	oldProfile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType}
	old := resolvedConfig(t, oldProfile, AdherenceStrict, false)
	if old.MinVersion != tls.VersionTLS10 || !hasCipher(old.CipherSuites, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA) {
		t.Fatalf("Old profile was not preserved: min=%d ciphers=%v", old.MinVersion, old.CipherSuites)
	}

	for _, name := range []string{"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", "ECDHE-ECDSA-AES128-GCM-SHA256"} {
		profile := &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileCustomType,
			Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"TLS_AES_128_GCM_SHA256", name},
			}},
		}
		config := resolvedConfig(t, profile, AdherenceStrict, false)
		wantProtocols := []string{"h2", "http/1.1"}
		if !reflect.DeepEqual(config.NextProtos, wantProtocols) ||
			!hasCipher(config.CipherSuites, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256) {
			t.Fatalf("cipher name %q did not enable h2 after filtering: %#v", name, config)
		}
	}

	tls13 := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS13,
			Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
		}},
	}
	config := resolvedConfig(t, tls13, AdherenceStrict, false)
	wantProtocols := []string{"http/1.1"}
	if config.MinVersion != tls.VersionTLS13 || config.CipherSuites != nil ||
		!reflect.DeepEqual(config.NextProtos, wantProtocols) {
		t.Fatalf("TLS 1.3 configuration should leave suites to Go and disable h2: %#v", config)
	}
}

func TestOptionsForProfileGroups(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS13,
			Groups: []configv1.TLSGroup{
				configv1.TLSGroup("FutureGroup"),
				configv1.TLSGroupSecP256r1,
				configv1.TLSGroupX25519,
			},
		}},
	}
	config := resolvedConfig(t, profile, AdherenceStrict, false)
	want := []tls.CurveID{tls.CurveP256, tls.X25519}
	if !reflect.DeepEqual(config.CurvePreferences, want) {
		t.Fatalf("CurvePreferences = %v, want %v", config.CurvePreferences, want)
	}

	emptyGroups := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS13,
		}},
	}
	if got := resolvedConfig(t, emptyGroups, AdherenceStrict, false).CurvePreferences; got != nil {
		t.Fatalf("empty groups CurvePreferences = %v, want nil Go defaults", got)
	}

	allUnusable := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
			MinTLSVersion: configv1.VersionTLS13,
			Groups:        []configv1.TLSGroup{configv1.TLSGroup("FutureGroup")},
		}},
	}
	if _, err := optionsForProfile(allUnusable, AdherenceStrict, false); err == nil {
		t.Fatal("optionsForProfile() accepted a profile with no usable groups")
	}
}

func TestResolveProfileStateFallbacks(t *testing.T) {
	resource := schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}
	for _, readErr := range []error{
		apierrors.NewNotFound(resource, "cluster"),
		apierrors.NewServiceUnavailable("unavailable"),
		apierrors.NewTimeoutError("timeout", 0),
		apierrors.NewServerTimeout(resource, "get", 0),
		apierrors.NewTooManyRequests("throttled", 0),
		context.DeadlineExceeded,
	} {
		result, err := resolveProfileState(profileState{}, readErr)
		if err != nil || !result.Watch || result.StateKnown || len(result.TLSOpts) == 0 ||
			result.Profile.Type != configv1.TLSProfileIntermediateType {
			t.Fatalf("fallback for %v: result=%#v err=%v", readErr, result, err)
		}
	}
	noMatch := &meta.NoResourceMatchError{
		PartialResource: configv1.GroupVersion.WithResource("apiservers"),
	}
	result, err := resolveProfileState(profileState{}, noMatch)
	if err != nil || result.Watch || len(result.TLSOpts) != 0 {
		t.Fatalf("NoMatch fallback: result=%#v err=%v", result, err)
	}
	for _, denied := range []error{
		apierrors.NewForbidden(resource, "cluster", errors.New("denied")),
		apierrors.NewUnauthorized("denied"),
	} {
		if result, err := resolveProfileState(profileState{}, denied); !errors.Is(err, denied) ||
			len(result.TLSOpts) != 0 {
			t.Fatalf("access denial was not fail-closed: result=%#v err=%v", result, err)
		}
	}
}

func TestProfileWatcherRecoveryAndChanges(t *testing.T) {
	intermediate := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
	watcher := NewProfileWatcher(Result{Profile: intermediate, Adherence: AdherenceNoOpinion}, func() {})
	watcher.readProfileState = func(context.Context) (profileState, error) {
		return profileState{adherence: AdherenceNoOpinion}, nil
	}
	called := false
	watcher.onRestart = func() { called = true }
	if _, err := watcher.Reconcile(context.Background(), reconcile.Request{}); err != nil || !called {
		t.Fatalf("fallback recovery must restart: called=%v err=%v", called, err)
	}

	called = false
	watcher.stateKnown = true
	watcher.lastState = profileState{profile: intermediate, adherence: AdherenceNoOpinion}
	watcher.readProfileState = func(context.Context) (profileState, error) {
		return profileState{}, apierrors.NewForbidden(
			schema.GroupResource{Resource: "apiservers"}, "cluster", errors.New("denied"),
		)
	}
	if _, err := watcher.Reconcile(context.Background(), reconcile.Request{}); err != nil || !called {
		t.Fatalf("watcher permission denial must restart: called=%v err=%v", called, err)
	}

	watcher.readProfileState = func(context.Context) (profileState, error) {
		return profileState{}, apierrors.NewServiceUnavailable("temporary")
	}
	result, err := watcher.Reconcile(context.Background(), reconcile.Request{})
	if err != nil || result.RequeueAfter != retryInterval {
		t.Fatalf("transient watcher read must retry: result=%#v err=%v", result, err)
	}
}
