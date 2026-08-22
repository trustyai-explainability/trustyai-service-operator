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
	"errors"
	"fmt"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = ctrl.Log.WithName("tls")

// openSSLToGoCipher maps OpenSSL cipher suite names to Go crypto/tls constants.
var openSSLToGoCipher = map[string]uint16{
	"TLS_AES_128_GCM_SHA256":               tls.TLS_AES_128_GCM_SHA256,
	"TLS_AES_256_GCM_SHA384":               tls.TLS_AES_256_GCM_SHA384,
	"TLS_CHACHA20_POLY1305_SHA256":         tls.TLS_CHACHA20_POLY1305_SHA256,
	"ECDHE-ECDSA-AES128-GCM-SHA256":        tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	"ECDHE-RSA-AES128-GCM-SHA256":          tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	"ECDHE-ECDSA-AES256-GCM-SHA384":        tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	"ECDHE-RSA-AES256-GCM-SHA384":          tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	"ECDHE-ECDSA-CHACHA20-POLY1305-SHA256": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	"ECDHE-RSA-CHACHA20-POLY1305-SHA256":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	"ECDHE-ECDSA-CHACHA20-POLY1305":        tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	"ECDHE-RSA-CHACHA20-POLY1305":          tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
}

// IntermediateCiphers is the Mozilla Intermediate cipher suite set.
var IntermediateCiphers = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
}

var tlsVersionMap = map[configv1.TLSProtocolVersion]uint16{
	"VersionTLS10": tls.VersionTLS10,
	"VersionTLS11": tls.VersionTLS11,
	"VersionTLS12": tls.VersionTLS12,
	"VersionTLS13": tls.VersionTLS13,
}

// Result holds the resolved TLS configuration.
type Result struct {
	TLSOpts []func(*tls.Config)
}

// Resolve reads the cluster TLS profile from apiservers.config.openshift.io/cluster
// and returns TLS option functions for controller-runtime.
// On non-OpenShift clusters or when the profile cannot be read, it returns
// hardened Intermediate defaults. Returns an error only on unexpected failures
// that should prevent startup (fail-closed).
func Resolve(ctx context.Context, cfg *rest.Config) (Result, error) {
	var result Result

	scheme := runtime.NewScheme()
	if err := configv1.Install(scheme); err != nil {
		return result, fmt.Errorf("installing OpenShift config scheme: %w", err)
	}

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return result, fmt.Errorf("creating bootstrap client for TLS profile: %w", err)
	}

	// Use a bounded context to avoid blocking startup indefinitely if
	// the API server is slow to respond.
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	apiServer := &configv1.APIServer{}
	if err := k8sClient.Get(fetchCtx, client.ObjectKey{Name: "cluster"}, apiServer); err != nil {
		switch {
		case meta.IsNoMatchError(err):
			log.Info("TLS profile not available, using hardened defaults (non-OpenShift cluster)")
		case apierrors.IsNotFound(err):
			log.Info("APIServer resource not found, using hardened defaults")
		case apierrors.IsServiceUnavailable(err):
			log.Info("API server unavailable, using hardened defaults", "error", err)
		case apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
			log.Info("API server request timed out, using hardened defaults", "error", err)
		case apierrors.IsTooManyRequests(err):
			log.Info("API server throttled request, using hardened defaults", "error", err)
		case errors.Is(err, context.DeadlineExceeded):
			log.Info("API server request deadline exceeded, using hardened defaults", "error", err)
		default:
			return result, fmt.Errorf("failed to read APIServer TLS profile: %w", err)
		}
		result.TLSOpts = append(result.TLSOpts, intermediateWithALPN)
		return result, nil //nolint:nilerr // intentional fail-open: use hardened defaults for transient/expected errors
	}

	minVersion, ciphers := parseProfile(apiServer.Spec.TLSSecurityProfile)
	if ciphers != nil && len(ciphers) == 0 {
		return result, fmt.Errorf("custom TLS profile specified ciphers but none are supported by Go")
	}

	result.TLSOpts = append(result.TLSOpts, func(c *tls.Config) {
		c.MinVersion = minVersion
		if len(ciphers) > 0 {
			c.CipherSuites = ciphers
		}
		c.NextProtos = []string{"h2", "http/1.1"}
	})
	return result, nil
}

func intermediateWithALPN(c *tls.Config) {
	c.MinVersion = tls.VersionTLS12
	c.CipherSuites = IntermediateCiphers
	c.NextProtos = []string{"h2", "http/1.1"}
}

func parseProfile(profile *configv1.TLSSecurityProfile) (uint16, []uint16) {
	if profile == nil {
		return tls.VersionTLS12, IntermediateCiphers
	}

	switch profile.Type {
	case configv1.TLSProfileIntermediateType, "":
		return tls.VersionTLS12, IntermediateCiphers
	case configv1.TLSProfileModernType:
		return tls.VersionTLS13, nil
	case configv1.TLSProfileOldType:
		return tls.VersionTLS10, nil
	case configv1.TLSProfileCustomType:
		if profile.Custom == nil {
			log.Info("Custom TLS profile type specified but custom block is nil, falling back to Intermediate")
			return tls.VersionTLS12, IntermediateCiphers
		}
		return parseCustomProfile(profile.Custom)
	default:
		log.Info("Unknown TLS profile type, falling back to Intermediate", "type", profile.Type)
		return tls.VersionTLS12, IntermediateCiphers
	}
}

func parseCustomProfile(custom *configv1.CustomTLSProfile) (uint16, []uint16) {
	minVersion, ok := tlsVersionMap[custom.MinTLSVersion]
	if !ok {
		log.Info("Unknown minTLSVersion in custom profile, defaulting to TLS 1.2", "minTLSVersion", custom.MinTLSVersion)
		minVersion = tls.VersionTLS12
	}

	if len(custom.Ciphers) == 0 {
		return minVersion, nil
	}

	ciphers := make([]uint16, 0, len(custom.Ciphers))
	for _, name := range custom.Ciphers {
		if id, ok := openSSLToGoCipher[name]; ok {
			ciphers = append(ciphers, id)
		} else {
			log.Info("Dropping unsupported cipher from custom TLS profile", "cipher", name)
		}
	}
	return minVersion, ciphers
}
