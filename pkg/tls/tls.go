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
	"sync"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	openshifttls "github.com/openshift/controller-runtime-common/pkg/tls"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = ctrl.Log.WithName("tls")

// Result holds the resolved TLS configuration.
type Result struct {
	TLSOpts      []func(*tls.Config)
	APIAvailable bool
	ProfileSpec  *configv1.TLSSecurityProfile
	ProxyArgs    ProxyTLSArguments
}

var proxyArgsState = struct {
	sync.RWMutex
	args ProxyTLSArguments
}{
	args: ProxyTLSArguments{MinVersion: string(configv1.VersionTLS12), Args: []string{
		"--tls-min-version=" + string(configv1.VersionTLS12),
		"--tls-curve-preferences=23,24,25,29",
	}},
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
	if args, err := ResolveProxyTLSArguments(nil, TLSAdherenceNoOpinion, nil, false); err == nil {
		SetProxyTLSArguments(args)
	}
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
			log.Info("Permission denied reading TLS profile, using hardened defaults; watcher will retry", "error", err)
			result.APIAvailable = true
		default:
			return result, fmt.Errorf("failed to read APIServer TLS profile: %w", err)
		}
		result.TLSOpts, _ = tlsOptsForProfile(nil)
		result.ProxyArgs, _ = ResolveProxyTLSArguments(nil, TLSAdherenceNoOpinion, nil, false)
		return result, nil //nolint:nilerr // intentional fail-open: use hardened defaults for transient/expected errors
	}

	result.APIAvailable = true
	result.ProfileSpec = apiServer.Spec.TLSSecurityProfile

	result.TLSOpts, err = tlsOptsForProfile(apiServer.Spec.TLSSecurityProfile)
	if err != nil {
		return result, err
	}
	// The pinned APIServerSpec API has no TLS adherence field, so strict proxy adherence cannot be selected here.
	result.ProxyArgs, err = ResolveProxyTLSArguments(apiServer.Spec.TLSSecurityProfile, TLSAdherenceNoOpinion, nil, false)
	if err != nil {
		return result, err
	}
	return result, nil
}

func tlsOptsForProfile(profile *configv1.TLSSecurityProfile) ([]func(*tls.Config), error) {
	profileSpec, err := openshifttls.GetTLSProfileSpec(profile)
	if err != nil {
		return nil, fmt.Errorf("resolving OpenShift TLS profile: %w", err)
	}

	tlsConfig, unsupported := openshifttls.NewTLSConfigFromProfile(profileSpec)
	if len(unsupported) > 0 {
		log.Info("TLS profile contains settings unsupported by Go", "unsupported", unsupported)
	}

	return []func(*tls.Config){tlsConfig, setALPN}, nil
}

func setALPN(c *tls.Config) {
	c.NextProtos = []string{"h2", "http/1.1"}
}
