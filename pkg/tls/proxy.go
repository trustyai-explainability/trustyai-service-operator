/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package tls

import (
	"crypto/tls"
	"fmt"
	"strconv"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	openshifttls "github.com/openshift/controller-runtime-common/pkg/tls"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

const (
	TLSAdherenceNoOpinion                    = ""
	TLSAdherenceLegacyAdheringComponentsOnly = "LegacyAdheringComponentsOnly"
	TLSAdherenceStrictAllComponents          = "StrictAllComponents"
)

// ProxyTLSArguments is the complete TLS argument set for kube-rbac-proxy.
// Args is suitable for appending to an existing proxy command line. The value
// is immutable from the resolver's perspective and has no reconciliation side
// effects.
type ProxyTLSArguments struct {
	MinVersion       string
	CipherSuites     []string
	CurvePreferences []uint16
	Args             []string
}

// ResolveProxyTLSArguments converts an OpenShift profile into kube-rbac-proxy
// arguments. A missing profile, or a profile when adherence is unset or
// LegacyAdheringComponentsOnly, resolves to the hardened Intermediate profile.
// Unknown adherence values are deliberately treated as strict.
//
// groups contains OpenShift curve names from a profile source that supports
// them. The current OpenShift API version used by this module does not yet
// expose groups on TLSProfileSpec, so nil uses the hardened default groups.
func ResolveProxyTLSArguments(profile *configv1.TLSSecurityProfile, adherence string, groups []string, fips bool) (ProxyTLSArguments, error) {
	strict := adherence != TLSAdherenceNoOpinion && adherence != TLSAdherenceLegacyAdheringComponentsOnly
	selected := profile
	if !strict {
		selected = nil
	}

	spec, err := openshifttls.GetTLSProfileSpec(selected)
	if err != nil {
		return ProxyTLSArguments{}, fmt.Errorf("resolving strict TLS profile: %w", err)
	}

	minVersion := string(spec.MinTLSVersion)
	min, err := tlsVersion(minVersion)
	if err != nil {
		return ProxyTLSArguments{}, fmt.Errorf("TLS profile has unsupported minimum version %q: %w", minVersion, err)
	}

	curves, err := resolveCurves(groups, fips, strict)
	if err != nil {
		return ProxyTLSArguments{}, err
	}

	result := ProxyTLSArguments{
		MinVersion:       minVersion,
		CurvePreferences: curves,
		Args:             []string{"--tls-min-version=" + minVersion, "--tls-curve-preferences=" + joinCurveIDs(curves)},
	}

	// Go and kube-rbac-proxy cannot restrict TLS 1.3 cipher suites. Do not
	// accidentally pass the TLS 1.2 list when the profile requires TLS 1.3.
	if min != tls.VersionTLS13 {
		result.CipherSuites = supportedCipherNames(spec.Ciphers, min)
		if len(spec.Ciphers) > 0 && len(result.CipherSuites) == 0 {
			return ProxyTLSArguments{}, fmt.Errorf("TLS profile has no supported cipher suites for TLS %s", minVersion)
		}
		if len(result.CipherSuites) > 0 {
			result.Args = append(result.Args, "--tls-cipher-suites="+strings.Join(result.CipherSuites, ","))
		}
	}

	return result, nil
}

func tlsVersion(version string) (uint16, error) {
	switch version {
	case string(configv1.VersionTLS10):
		return tls.VersionTLS10, nil
	case string(configv1.VersionTLS11):
		return tls.VersionTLS11, nil
	case string(configv1.VersionTLS12):
		return tls.VersionTLS12, nil
	case string(configv1.VersionTLS13):
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unknown TLS version")
	}
}

func supportedCipherNames(names []string, minVersion uint16) []string {
	if len(names) == 0 {
		return nil
	}
	result := make([]string, 0, len(names))
	for _, name := range names {
		ianaNames := libgocrypto.OpenSSLToIANACipherSuites([]string{name})
		if len(ianaNames) != 1 {
			ianaNames = []string{name}
		}
		for _, suite := range tls.CipherSuites() {
			if suite.Name != ianaNames[0] || !supportsVersionAtOrAbove(suite.SupportedVersions, minVersion) {
				continue
			}
			result = append(result, suite.Name)
			break
		}
	}
	return result
}

var curveIDs = map[string]uint16{
	"secp256r1":      23,
	"secp384r1":      24,
	"secp521r1":      25,
	"X25519":         29,
	"X25519MLKEM768": 4588,
}

func supportsVersionAtOrAbove(versions []uint16, wanted uint16) bool {
	for _, version := range versions {
		// TLS 1.3 cipher suites are not configurable through CipherSuites.
		if version != tls.VersionTLS13 && version >= wanted {
			return true
		}
	}
	return false
}

func resolveCurves(groups []string, fips, strict bool) ([]uint16, error) {
	if len(groups) == 0 {
		groups = []string{"secp256r1", "secp384r1", "secp521r1", "X25519"}
	}
	result := make([]uint16, 0, len(groups))
	seen := map[uint16]bool{}
	for _, group := range groups {
		id, ok := curveIDs[group]
		if !ok || (fips && (id == 29 || id == 4588)) {
			continue
		}
		if !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	if strict && len(result) == 0 {
		return nil, fmt.Errorf("strict TLS profile has no usable curve preferences")
	}
	return result, nil
}

func joinCurveIDs(curves []uint16) string {
	values := make([]string, len(curves))
	for i, curve := range curves {
		values[i] = strconv.FormatUint(uint64(curve), 10)
	}
	return strings.Join(values, ",")
}
