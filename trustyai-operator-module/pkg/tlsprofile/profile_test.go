package tlsprofile

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

type testDiscovery struct {
	discovery.DiscoveryInterface
	resources *metav1.APIResourceList
	err       error
}

func (d testDiscovery) ServerResourcesForGroupVersion(string) (*metav1.APIResourceList, error) {
	return d.resources, d.err
}

func TestResolveStateAdherence(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	for _, test := range []struct {
		name      string
		adherence string
		want      uint16
	}{
		{name: "NoOpinion uses Intermediate", adherence: tlsAdherenceNoOpinion, want: tls.VersionTLS12},
		{name: "Legacy uses Intermediate", adherence: tlsAdherenceLegacy, want: tls.VersionTLS12},
		{name: "Strict applies profile", adherence: tlsAdherenceStrict, want: tls.VersionTLS13},
		{name: "unknown applies profile", adherence: "FuturePolicy", want: tls.VersionTLS13},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := resolveState(profileState{profile: profile, adherence: test.adherence}, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := &tls.Config{}
			for _, option := range result.TLSOpts {
				option(got)
			}
			if got.MinVersion != test.want || !result.Watchable {
				t.Fatalf("MinVersion = %d, watchable = %v, want %d and true", got.MinVersion, result.Watchable, test.want)
			}
		})
	}
}

func TestTLSOptionsParseProfile(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		MinTLSVersion: configv1.VersionTLS12,
		Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
		Groups:        []configv1.TLSGroup{"X25519", "secp256r1"},
	}
	options, err := tlsOptions(spec)
	if err != nil {
		t.Fatal(err)
	}
	config := &tls.Config{}
	for _, option := range options {
		option(config)
	}
	if config.MinVersion != tls.VersionTLS12 || len(config.CipherSuites) != 1 || len(config.CurvePreferences) != 2 {
		t.Fatalf("unexpected TLS config: %#v", config)
	}

	for _, bad := range []*configv1.TLSProfileSpec{
		{MinTLSVersion: "VersionTLS99"},
		{MinTLSVersion: configv1.VersionTLS12},
		{MinTLSVersion: configv1.VersionTLS12, Ciphers: []string{"unknown-cipher"}},
		{MinTLSVersion: configv1.VersionTLS12, Groups: []configv1.TLSGroup{"unknown-group"}},
	} {
		if _, err := tlsOptions(bad); err == nil {
			t.Errorf("tlsOptions(%#v) succeeded, want an error", bad)
		}
	}
}

func TestBuiltInProfilesTranslate(t *testing.T) {
	for profileType, spec := range configv1.TLSProfiles {
		t.Run(string(profileType), func(t *testing.T) {
			options, err := tlsOptions(spec)
			if err != nil {
				t.Fatal(err)
			}
			config := &tls.Config{}
			for _, option := range options {
				option(config)
			}
			if config.MinVersion == 0 {
				t.Fatal("minimum TLS version was not configured")
			}
			if config.MinVersion < tls.VersionTLS13 && len(config.CipherSuites) == 0 {
				t.Fatal("profile did not configure a supported cipher suite")
			}
		})
	}
}

func TestProfileSpecErrors(t *testing.T) {
	for _, profile := range []*configv1.TLSSecurityProfile{
		{Type: "Unknown"},
		{Type: configv1.TLSProfileCustomType},
	} {
		if _, err := profileSpec(profile); err == nil {
			t.Errorf("profileSpec(%#v) succeeded, want an error", profile)
		}
	}
}

func TestReadProfileState(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "APIServer",
		"metadata":   map[string]interface{}{"name": apiServerName},
		"spec": map[string]interface{}{
			"tlsSecurityProfile": map[string]interface{}{"type": "Modern"},
			"tlsAdherence":       tlsAdherenceStrict,
		},
	}}
	obj.SetGroupVersionKind(configv1.GroupVersion.WithKind("APIServer"))
	c := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		apiServerResource: "APIServerList",
	}, obj)
	state, err := readProfileState(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.profile == nil || state.profile.Type != configv1.TLSProfileModernType ||
		state.adherence != tlsAdherenceStrict {
		t.Fatalf("unexpected profile state: %#v", state)
	}
}

func TestReadProfileStateClassifiesMissingAPI(t *testing.T) {
	c := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		apiServerResource: "APIServerList",
	})
	resource := schema.GroupResource{Group: configv1.GroupName, Resource: "apiservers"}
	_, err := readProfileState(
		context.Background(),
		c,
		testDiscovery{err: apierrors.NewNotFound(resource, configv1.GroupVersion.String())},
	)
	if !errors.Is(err, errAPIUnavailable) {
		t.Fatalf("missing API error = %v", err)
	}
	_, err = readProfileState(
		context.Background(),
		c,
		testDiscovery{resources: &metav1.APIResourceList{GroupVersion: configv1.GroupVersion.String()}},
	)
	if !errors.Is(err, errAPIServerMissing) || errors.Is(err, errAPIUnavailable) {
		t.Fatalf("missing APIServer object error = %v", err)
	}
}

func TestResolveStateReadErrors(t *testing.T) {
	forbidden := apierrors.NewForbidden(
		schema.GroupResource{Group: configv1.GroupName, Resource: "apiservers"},
		apiServerName,
		errors.New("denied"),
	)
	if _, err := resolveState(profileState{}, forbidden); !apierrors.IsForbidden(err) {
		t.Fatalf("Forbidden error = %v", err)
	}
	unauthorized := apierrors.NewUnauthorized("denied")
	if _, err := resolveState(profileState{}, unauthorized); !apierrors.IsUnauthorized(err) {
		t.Fatalf("Unauthorized error = %v", err)
	}
	result, err := resolveState(profileState{}, apierrors.NewServiceUnavailable("temporary"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Watchable || len(result.TLSOpts) == 0 {
		t.Fatalf("transient fallback = %#v", result)
	}
	for _, option := range result.TLSOpts {
		config := &tls.Config{}
		option(config)
		if config.MinVersion != tls.VersionTLS12 {
			t.Fatalf("fallback MinVersion = %d, want TLS 1.2", config.MinVersion)
		}
	}
	result, err = resolveState(profileState{}, errAPIUnavailable)
	if err != nil || result.Watchable || len(result.TLSOpts) != 0 {
		t.Fatalf("non-OpenShift result = %#v, %v", result, err)
	}
}
