package tlsprofile

import (
	"crypto/tls"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	openshifttls "github.com/openshift/controller-runtime-common/pkg/tls"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	AdherenceNoOpinion = ""
	AdherenceLegacy    = "LegacyAdheringComponentsOnly"
	AdherenceStrict    = "StrictAllComponents"
)

func optionsForProfile(
	profile *configv1.TLSSecurityProfile,
	adherence string,
	force bool,
) ([]func(*tls.Config), error) {
	if !force && (adherence == AdherenceNoOpinion || adherence == AdherenceLegacy) {
		return nil, nil
	}
	if adherence != AdherenceNoOpinion && adherence != AdherenceLegacy && adherence != AdherenceStrict {
		log.Info("unknown TLS adherence value, treating as StrictAllComponents", "adherence", adherence)
	}
	if profile == nil || profile.Type == "" {
		profile = &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
	}

	var spec configv1.TLSProfileSpec
	if profile.Type == configv1.TLSProfileCustomType {
		if profile.Custom == nil {
			return nil, fmt.Errorf("custom TLS profile has no custom settings")
		}
		spec = profile.Custom.TLSProfileSpec
	} else {
		builtin, ok := configv1.TLSProfiles[profile.Type]
		if !ok {
			return nil, fmt.Errorf("unknown TLS profile type %q", profile.Type)
		}
		spec = *builtin
	}
	if spec.MinTLSVersion == "" {
		return nil, fmt.Errorf("TLS profile has an empty minimum version")
	}
	minVersion, err := tlsVersion(string(spec.MinTLSVersion))
	if err != nil {
		return nil, fmt.Errorf("unsupported minimum TLS version %q: %w", spec.MinTLSVersion, err)
	}

	configureTLS, unsupported := openshifttls.NewTLSConfigFromProfile(spec)
	if len(unsupported) > 0 {
		log.Info("TLS profile contains unsupported cipher suites", "ciphers", unsupported)
	}
	resolved := &tls.Config{}
	configureTLS(resolved)
	if minVersion != tls.VersionTLS13 {
		if len(spec.Ciphers) == 0 {
			return nil, fmt.Errorf("TLS profile has no cipher suites for TLS <= 1.2")
		}
		implemented := implementedCipherSuites()
		usable := make([]uint16, 0, len(resolved.CipherSuites))
		for _, suite := range resolved.CipherSuites {
			if suite >= 0x1300 && suite <= 0x13ff {
				continue
			}
			if _, ok := implemented[suite]; ok {
				usable = append(usable, suite)
			}
		}
		if len(usable) == 0 {
			return nil, fmt.Errorf("TLS profile has no usable cipher suites for TLS <= 1.2")
		}
		resolved.CipherSuites = usable
	}
	setALPN(resolved, minVersion, spec.Ciphers)

	return []func(*tls.Config){func(dst *tls.Config) {
		dst.MinVersion = resolved.MinVersion
		dst.CipherSuites = append([]uint16(nil), resolved.CipherSuites...)
		dst.NextProtos = append([]string(nil), resolved.NextProtos...)
	}}, nil
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

func implementedCipherSuites() map[uint16]struct{} {
	implemented := make(map[uint16]struct{})
	for _, suite := range tls.CipherSuites() {
		implemented[suite.ID] = struct{}{}
	}
	for _, suite := range tls.InsecureCipherSuites() {
		if !strings.Contains(suite.Name, "RC4") {
			implemented[suite.ID] = struct{}{}
		}
	}
	return implemented
}

func setALPN(config *tls.Config, minVersion uint16, names []string) {
	hasHTTP2Cipher := hasCipher(config.CipherSuites, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256) ||
		hasCipher(config.CipherSuites, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256) ||
		hasHTTP2CipherName(names)
	canServeHTTP2 := minVersion >= tls.VersionTLS12 && hasHTTP2Cipher
	if canServeHTTP2 {
		config.NextProtos = []string{"h2", "http/1.1"}
		return
	}
	config.NextProtos = []string{"http/1.1"}
}

func hasCipher(suites []uint16, wanted uint16) bool {
	for _, suite := range suites {
		if suite == wanted {
			return true
		}
	}
	return false
}

func hasHTTP2CipherName(names []string) bool {
	for _, name := range names {
		switch strings.ToUpper(name) {
		case "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "ECDHE-RSA-AES128-GCM-SHA256",
			"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", "ECDHE-ECDSA-AES128-GCM-SHA256":
			return true
		}
	}
	return false
}

var log = ctrl.Log.WithName("tls-profile")
