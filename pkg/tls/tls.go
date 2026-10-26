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
	"strings"
	"sync"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	openshifttls "github.com/openshift/controller-runtime-common/pkg/tls"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("tls")

// implementedCipherSuites returns the IDs of every cipher suite implemented
// by the running Go build, excluding RC4 suites prohibited by RFC 7465.
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

func normalizeTLSProfileSpec(spec configv1.TLSProfileSpec) configv1.TLSProfileSpec {
	if spec.MinTLSVersion == "" {
		spec.MinTLSVersion = configv1.VersionTLS12
	}
	return spec
}

// Result holds the resolved TLS configuration.
type Result struct {
	TLSOpts      []func(*tls.Config)
	APIAvailable bool
	ProfileSpec  *configv1.TLSSecurityProfile
	TLSAdherence string
	ProxyArgs    ProxyTLSArguments
}

var proxyArgsState = struct {
	sync.RWMutex
	args ProxyTLSArguments
}{
	// Seeded from ResolveProxyTLSArguments so curve IDs stay aligned with
	// defaultCurveGroups. init() re-resolves with the host FIPS setting.
	//
	// 4588 (X25519MLKEM768) requires a kube-rbac-proxy that accepts
	// --tls-curve-preferences and CurveID 4588; unknown IDs fail startup
	// (CrashLoopBackOff). Override the kube-rbac-proxy ConfigMap image
	// only with a build that supports both.
	args: mustDefaultProxyTLSArguments(false),
}

func mustDefaultProxyTLSArguments(fips bool) ProxyTLSArguments {
	args, err := ResolveProxyTLSArguments(nil, TLSAdherenceNoOpinion, nil, fips)
	if err != nil {
		panic(fmt.Sprintf("default proxy TLS arguments: %v", err))
	}
	return args
}

// SetProxyTLSArguments publishes the arguments used by workload builders.
// Callers must resolve and validate the complete value before publishing it.
func SetProxyTLSArguments(args ProxyTLSArguments) {
	proxyArgsState.Lock()
	defer proxyArgsState.Unlock()
	proxyArgsState.args = copyProxyTLSArguments(args)
}

// CurrentProxyTLSArguments returns a copy of the arguments currently used by
// workload builders.
func CurrentProxyTLSArguments() ProxyTLSArguments {
	proxyArgsState.RLock()
	defer proxyArgsState.RUnlock()
	return copyProxyTLSArguments(proxyArgsState.args)
}

func copyProxyTLSArguments(args ProxyTLSArguments) ProxyTLSArguments {
	args.CipherSuites = append([]string(nil), args.CipherSuites...)
	args.CurvePreferences = append([]uint16(nil), args.CurvePreferences...)
	args.Args = append([]string(nil), args.Args...)
	return args
}

func init() {
	if args, err := ResolveProxyTLSArguments(nil, TLSAdherenceNoOpinion, nil, fipsModeEnabled()); err == nil {
		SetProxyTLSArguments(args)
	}
}

// Resolve reads the cluster TLS profile from apiservers.config.openshift.io/cluster
// and returns TLS option functions for controller-runtime. On non-OpenShift
// clusters or transient API errors, it returns hardened Intermediate defaults.
// Authorization and unexpected failures return errors to prevent startup.
func Resolve(ctx context.Context, cfg *rest.Config) (Result, error) {
	var result Result

	apiClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return result, fmt.Errorf("creating bootstrap client for TLS profile: %w", err)
	}

	// Use a bounded context to avoid blocking startup indefinitely if
	// the API server is slow to respond.
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	state, err := readTLSProfileState(fetchCtx, apiClient)
	if err != nil {
		switch {
		case meta.IsNoMatchError(err):
			log.Info("TLS profile not available (non-OpenShift cluster)")
		case apierrors.IsNotFound(err):
			log.Info("APIServer resource not found, using hardened defaults")
		case apierrors.IsServiceUnavailable(err),
			apierrors.IsTimeout(err),
			apierrors.IsServerTimeout(err),
			apierrors.IsTooManyRequests(err),
			errors.Is(err, context.DeadlineExceeded):
			log.Info("Transient API error reading TLS profile, using hardened defaults", "error", err)
			result.APIAvailable = true
		case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
			return result, fmt.Errorf("permission denied reading APIServer TLS profile: %w", err)
		default:
			return result, fmt.Errorf("failed to read APIServer TLS profile: %w", err)
		}
		result.TLSOpts, err = tlsOptsForProfile(nil, TLSAdherenceNoOpinion)
		if err != nil {
			return result, err
		}
		if result.APIAvailable {
			// A transient read failure must not replace a previously published
			// strict profile with NoOpinion arguments. Keep the last known-good
			// value until the watcher can confirm the current APIServer state.
			result.ProxyArgs = CurrentProxyTLSArguments()
		} else {
			result.ProxyArgs, _ = ResolveProxyTLSArguments(nil, TLSAdherenceNoOpinion, nil, fipsModeEnabled())
		}
		return result, nil //nolint:nilerr // intentional fail-open: use hardened defaults for transient/expected errors
	}

	result.APIAvailable = true
	result.ProfileSpec = state.profile
	result.TLSAdherence = state.adherence

	result.TLSOpts, err = tlsOptsForProfile(state.profile, state.adherence)
	if err != nil {
		return result, err
	}
	// Resolve the APIServer profile and adherence together before publishing
	// the complete configuration to workload builders.
	result.ProxyArgs, err = ResolveProxyTLSArguments(
		state.profile,
		result.TLSAdherence,
		nil,
		fipsModeEnabled(),
	)
	if err != nil {
		return result, err
	}
	return result, nil
}

func tlsOptsForProfile(profile *configv1.TLSSecurityProfile, adherence string) ([]func(*tls.Config), error) {
	profileSpec, err := openshifttls.GetTLSProfileSpec(profile)
	if err != nil {
		return nil, fmt.Errorf("resolving OpenShift TLS profile: %w", err)
	}
	profileSpec = normalizeTLSProfileSpec(profileSpec)
	if _, err := tlsVersion(string(profileSpec.MinTLSVersion)); err != nil {
		return nil, fmt.Errorf("unsupported minimum TLS version %q: %w", profileSpec.MinTLSVersion, err)
	}

	tlsConfig, unsupported := openshifttls.NewTLSConfigFromProfile(profileSpec)
	if len(unsupported) > 0 {
		log.Info("TLS profile contains settings unsupported by Go", "unsupported", unsupported)
	}
	if profileSpec.MinTLSVersion != configv1.VersionTLS13 {
		strict := adherence != TLSAdherenceNoOpinion && adherence != TLSAdherenceLegacyAdheringComponentsOnly
		if len(profileSpec.Ciphers) == 0 && strict {
			return nil, fmt.Errorf("TLS profile has no cipher suites for TLS <= 1.2")
		}
		if len(profileSpec.Ciphers) > 0 {
			goConfig := &tls.Config{}
			tlsConfig(goConfig)
			implementedCiphers := implementedCipherSuites()
			usableCiphers := make([]uint16, 0, len(goConfig.CipherSuites))
			for _, suite := range goConfig.CipherSuites {
				if suite >= 0x1300 && suite <= 0x13ff {
					continue
				}
				if _, ok := implementedCiphers[suite]; ok {
					usableCiphers = append(usableCiphers, suite)
				}
			}
			if len(usableCiphers) == 0 {
				return nil, fmt.Errorf("failed to find usable cipher suites for TLS <= 1.2: all %d ciphers in TLS profile are TLS 1.3-only, unimplemented by Go, or excluded by policy", len(profileSpec.Ciphers))
			}
			profileTLSConfig := tlsConfig
			tlsConfig = func(c *tls.Config) {
				profileTLSConfig(c)
				c.CipherSuites = usableCiphers
			}
		}
	}

	return []func(*tls.Config){tlsConfig, setALPN}, nil
}

func setALPN(c *tls.Config) {
	c.NextProtos = []string{"h2", "http/1.1"}
}
