package tlsprofile

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type profileState struct {
	profile   *configv1.TLSSecurityProfile
	adherence string
}

type Result struct {
	TLSOpts    []func(*tls.Config)
	Profile    *configv1.TLSSecurityProfile
	Adherence  string
	Watch      bool
	StateKnown bool
}

func Resolve(ctx context.Context, config *rest.Config) (Result, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return Result{}, fmt.Errorf("creating bootstrap client for TLS profile: %w", err)
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	state, err := readTLSProfileState(fetchCtx, config, dynamicClient)
	return resolveProfileState(state, err)
}

func resolveProfileState(state profileState, readErr error) (Result, error) {
	if readErr != nil {
		switch {
		case apierrors.IsForbidden(readErr), apierrors.IsUnauthorized(readErr):
			return Result{}, fmt.Errorf("permission denied reading APIServer TLS profile: %w", readErr)
		case errors.Is(readErr, context.Canceled):
			return Result{}, fmt.Errorf("TLS profile bootstrap canceled: %w", readErr)
		case meta.IsNoMatchError(readErr):
			log.Info("TLS profile API is not served, retaining Go TLS defaults")
			return Result{}, nil
		case apierrors.IsNotFound(readErr), isTransientReadError(readErr):
			log.Info("TLS profile is unavailable, applying Intermediate until the watcher recovers", "error", readErr)
			intermediate := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
			opts, err := optionsForProfile(intermediate, AdherenceNoOpinion, true)
			if err != nil {
				return Result{}, err
			}
			return Result{TLSOpts: opts, Profile: intermediate, Adherence: AdherenceNoOpinion, Watch: true}, nil
		default:
			return Result{}, fmt.Errorf("failed to read APIServer TLS profile: %w", readErr)
		}
	}
	profile := state.profile
	if profile == nil {
		profile = &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
	}
	opts, err := optionsForProfile(profile, state.adherence, false)
	if err != nil {
		return Result{}, err
	}
	return Result{TLSOpts: opts, Profile: state.profile, Adherence: state.adherence, Watch: true, StateKnown: true}, nil
}

func readTLSProfileState(ctx context.Context, config *rest.Config, client dynamic.Interface) (profileState, error) {
	discoveryConfig := rest.CopyConfig(config)
	discoveryConfig.Timeout = 10 * time.Second
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(discoveryConfig)
	if err != nil {
		return profileState{}, fmt.Errorf("creating discovery client for TLS profile API: %w", err)
	}
	resources, err := discoveryClient.ServerResourcesForGroupVersion(configv1.GroupVersion.String())
	if err != nil {
		if apierrors.IsNotFound(err) {
			return profileState{}, &meta.NoResourceMatchError{PartialResource: configv1.GroupVersion.WithResource("apiservers")}
		}
		return profileState{}, err
	}
	served := false
	for _, resource := range resources.APIResources {
		if resource.Name == "apiservers" {
			served = true
			break
		}
	}
	if !served {
		return profileState{}, &meta.NoResourceMatchError{PartialResource: configv1.GroupVersion.WithResource("apiservers")}
	}
	apiServer, err := client.Resource(
		configv1.GroupVersion.WithResource("apiservers"),
	).Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return profileState{}, err
	}
	state := profileState{adherence: AdherenceNoOpinion}
	profile, found, err := unstructured.NestedMap(apiServer.Object, "spec", "tlsSecurityProfile")
	if err != nil {
		return profileState{}, fmt.Errorf("reading APIServer spec.tlsSecurityProfile: %w", err)
	}
	if found {
		state.profile = &configv1.TLSSecurityProfile{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(profile, state.profile); err != nil {
			return profileState{}, fmt.Errorf("decoding APIServer spec.tlsSecurityProfile: %w", err)
		}
	}
	state.adherence, found, err = unstructured.NestedString(apiServer.Object, "spec", "tlsAdherence")
	if err != nil {
		return profileState{}, fmt.Errorf("reading APIServer spec.tlsAdherence: %w", err)
	}
	if !found {
		state.adherence = AdherenceNoOpinion
	}
	return state, nil
}

func isTransientReadError(err error) bool {
	return apierrors.IsServiceUnavailable(err) || apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) ||
		errors.Is(err, context.DeadlineExceeded)
}
