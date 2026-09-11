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
	"crypto/tls"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

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
			name: "Unknown type falls back to Intermediate",
			profile: &configv1.TLSSecurityProfile{
				Type: "SuperSecure",
			},
			wantMinVersion: tls.VersionTLS12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := tlsOptsForProfile(tt.profile)
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

func TestTLSOptsForProfileRejectsNilCustomProfile(t *testing.T) {
	_, err := tlsOptsForProfile(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType})
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

	opts, err := tlsOptsForProfile(profile)
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
