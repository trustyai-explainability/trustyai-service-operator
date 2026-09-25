package tls

import (
	"reflect"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestResolveProxyTLSArguments(t *testing.T) {
	tests := []struct {
		name       string
		profile    *configv1.TLSSecurityProfile
		adherence  string
		groups     []string
		fips       bool
		min        string
		cipherFlag bool
		curves     []uint16
	}{
		{"no opinion uses intermediate", &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}, "", nil, false, "VersionTLS12", true, []uint16{23, 24, 25, 29}},
		{"legacy uses intermediate", &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}, TLSAdherenceLegacyAdheringComponentsOnly, nil, false, "VersionTLS12", true, []uint16{23, 24, 25, 29}},
		{"strict uses modern", &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}, TLSAdherenceStrictAllComponents, nil, false, "VersionTLS13", false, []uint16{23, 24, 25, 29}},
		{"unknown adherence is strict", &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}, "FutureValue", nil, false, "VersionTLS13", false, []uint16{23, 24, 25, 29}},
		{"fips removes x25519", nil, TLSAdherenceStrictAllComponents, []string{"X25519", "secp256r1", "not-a-curve"}, true, "VersionTLS12", true, []uint16{23}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveProxyTLSArguments(tt.profile, tt.adherence, tt.groups, tt.fips)
			if err != nil {
				t.Fatal(err)
			}
			if got.MinVersion != tt.min || !reflect.DeepEqual(got.CurvePreferences, tt.curves) {
				t.Fatalf("got version %q curves %v", got.MinVersion, got.CurvePreferences)
			}
			hasCipher := strings.Contains(strings.Join(got.Args, " "), "--tls-cipher-suites=")
			if hasCipher != tt.cipherFlag {
				t.Errorf("cipher flag present=%v, want %v; args=%v", hasCipher, tt.cipherFlag, got.Args)
			}
		})
	}
}

func TestResolveProxyTLSArgumentsUsesFIPSDefaultCurves(t *testing.T) {
	got, err := ResolveProxyTLSArguments(nil, TLSAdherenceStrictAllComponents, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.CurvePreferences, []uint16{23, 24, 25}) {
		t.Fatalf("FIPS CurvePreferences = %v, want [23 24 25]", got.CurvePreferences)
	}
}

func TestResolveProxyTLSArgumentsRejectsStrictProfiles(t *testing.T) {
	_, err := ResolveProxyTLSArguments(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType}, TLSAdherenceStrictAllComponents, nil, false)
	if err == nil {
		t.Fatal("expected nil custom profile to fail")
	}

	_, err = ResolveProxyTLSArguments(nil, TLSAdherenceStrictAllComponents, []string{"X25519"}, true)
	if err == nil {
		t.Fatal("expected no usable FIPS curves to fail")
	}
}

func TestResolveProxyTLSArgumentsOldProfileRetainsUsableCiphers(t *testing.T) {
	got, err := ResolveProxyTLSArguments(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType}, TLSAdherenceStrictAllComponents, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.MinVersion != string(configv1.VersionTLS10) {
		t.Fatalf("MinVersion = %q, want %q", got.MinVersion, configv1.VersionTLS10)
	}
	if len(got.CipherSuites) == 0 || got.CipherSuites[0] != "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256" {
		t.Fatalf("CipherSuites = %v, want TLS 1.2 suites retained for a TLS 1.0 minimum", got.CipherSuites)
	}
	for _, suite := range got.CipherSuites {
		if strings.HasPrefix(suite, "TLS_AES_") || strings.HasPrefix(suite, "TLS_CHACHA20_POLY1305_") {
			t.Fatalf("TLS 1.3-only suite %q should not be passed to kube-rbac-proxy", suite)
		}
	}
}

func TestResolveProxyTLSArgumentsUsesApplicableIANACiphers(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType, Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
		MinTLSVersion: configv1.VersionTLS12,
		Ciphers: []string{
			"TLS_AES_128_GCM_SHA256",
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		},
	}}}
	got, err := ResolveProxyTLSArguments(profile, TLSAdherenceStrictAllComponents, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.CipherSuites, []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"}) {
		t.Fatalf("CipherSuites = %v", got.CipherSuites)
	}
	if !strings.Contains(strings.Join(got.Args, " "), "--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256") {
		t.Fatalf("expected plural cipher flag in args: %v", got.Args)
	}
}

func TestResolveProxyTLSArgumentsRejectsUnknownMinimumVersion(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType, Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
		MinTLSVersion: "VersionTLS99",
	}}}
	if _, err := ResolveProxyTLSArguments(profile, TLSAdherenceStrictAllComponents, nil, false); err == nil {
		t.Fatal("expected unknown TLS version to fail")
	}
}

func TestResolveProxyTLSArgumentsOmitsTLS13CipherFlag(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType, Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
		MinTLSVersion: configv1.VersionTLS13,
		Ciphers:       []string{"not-used-for-TLS13"},
	}}}
	got, err := ResolveProxyTLSArguments(profile, TLSAdherenceStrictAllComponents, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(got.Args, " "), "cipher") {
		t.Fatalf("TLS 1.3 must not receive a cipher restriction: %v", got.Args)
	}
}
