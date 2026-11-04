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
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	apiServerName         = "cluster"
	profileRetryInterval  = 5 * time.Second
	tlsAdherenceNoOpinion = ""
	tlsAdherenceLegacy    = "LegacyAdheringComponentsOnly"
	tlsAdherenceStrict    = "StrictAllComponents"
)

var (
	apiServerResource   = configv1.GroupVersion.WithResource("apiservers")
	errAPIServerMissing = errors.New("APIServer object not found")
	tlsProfileLog       = ctrl.Log.WithName("tls-profile")
)

type Result struct {
	TLSOpts   []func(*tls.Config)
	Watchable bool
	Profile   *configv1.TLSSecurityProfile
	Adherence string
}

type profileState struct {
	profile   *configv1.TLSSecurityProfile
	adherence string
}

func Resolve(ctx context.Context, cfg *rest.Config) (Result, error) {
	apiClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return Result{}, fmt.Errorf("creating dynamic TLS profile client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return Result{}, fmt.Errorf("creating TLS profile discovery client: %w", err)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	state, readErr := readWithRetry(fetchCtx, func(ctx context.Context) (profileState, error) {
		return readProfileState(ctx, apiClient, discoveryClient)
	}, wait.Backoff{Duration: 250 * time.Millisecond, Factor: 2, Jitter: 0.1, Steps: 5})
	return resolveState(state, readErr)
}

func readWithRetry(
	ctx context.Context,
	read func(context.Context) (profileState, error),
	backoff wait.Backoff,
) (profileState, error) {
	var state profileState
	var lastErr error
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		var err error
		state, err = read(ctx)
		if err == nil {
			return true, nil
		}
		lastErr = err
		if isTransient(err) || errors.Is(err, errAPIServerMissing) {
			return false, nil
		}
		return false, err
	})
	if err != nil {
		return profileState{}, errors.Join(err, lastErr)
	}
	return state, nil
}

func resolveState(state profileState, readErr error) (Result, error) {
	if readErr != nil {
		if apierrors.IsForbidden(readErr) || apierrors.IsUnauthorized(readErr) {
			return Result{}, fmt.Errorf("permission denied reading APIServer TLS profile: %w", readErr)
		}
		if errors.Is(readErr, context.Canceled) {
			return Result{}, fmt.Errorf("TLS profile bootstrap canceled: %w", readErr)
		}
		if isAPIUnavailable(readErr) {
			ctrl.Log.WithName("tls-profile").Info("APIServer TLS profile API is unavailable, using Go TLS defaults")
			return Result{}, nil
		}
		if isTransient(readErr) || errors.Is(readErr, errAPIServerMissing) || errors.Is(readErr, context.DeadlineExceeded) {
			tlsProfileLog.Info("TLS profile read failed, using Intermediate until the watcher recovers", "error", readErr)
			options, err := tlsOptions(configv1.TLSProfiles[configv1.TLSProfileIntermediateType])
			if err != nil {
				return Result{}, fmt.Errorf("configuring Intermediate TLS fallback: %w", err)
			}
			return Result{
				TLSOpts:   options,
				Watchable: true,
			}, nil
		}
		return Result{}, fmt.Errorf("failed to read APIServer TLS profile: %w", readErr)
	}

	effective := state.profile
	if state.adherence == tlsAdherenceNoOpinion || state.adherence == tlsAdherenceLegacy {
		effective = &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
	} else if state.adherence != tlsAdherenceStrict {
		tlsProfileLog.Info("unknown TLS adherence policy, applying the configured profile", "adherence", state.adherence)
	}
	spec, err := profileSpec(effective)
	if err != nil {
		return Result{}, err
	}
	options, err := tlsOptions(spec)
	if err != nil {
		return Result{}, err
	}
	return Result{TLSOpts: options, Watchable: true, Profile: state.profile, Adherence: state.adherence}, nil
}

func readProfileState(
	ctx context.Context,
	apiClient dynamic.Interface,
	discoveryClient discovery.DiscoveryInterface,
) (profileState, error) {
	object, err := apiClient.Resource(apiServerResource).Get(ctx, apiServerName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, discoveryErr := discoveryClient.ServerResourcesForGroupVersion(configv1.GroupVersion.String())
			if apierrors.IsNotFound(discoveryErr) || meta.IsNoMatchError(discoveryErr) {
				return profileState{}, fmt.Errorf("%w: %v", errAPIUnavailable, err)
			}
			if discoveryErr != nil {
				return profileState{}, fmt.Errorf("checking APIServer API availability: %w", discoveryErr)
			}
			return profileState{}, fmt.Errorf("%w: %v", errAPIServerMissing, err)
		}
		return profileState{}, err
	}

	state := profileState{adherence: tlsAdherenceNoOpinion}
	profile, found, err := unstructured.NestedMap(object.Object, "spec", "tlsSecurityProfile")
	if err != nil {
		return profileState{}, fmt.Errorf("reading APIServer spec.tlsSecurityProfile: %w", err)
	}
	if found {
		state.profile = &configv1.TLSSecurityProfile{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(profile, state.profile); err != nil {
			return profileState{}, fmt.Errorf("decoding APIServer spec.tlsSecurityProfile: %w", err)
		}
	}
	state.adherence, found, err = unstructured.NestedString(object.Object, "spec", "tlsAdherence")
	if err != nil {
		return profileState{}, fmt.Errorf("reading APIServer spec.tlsAdherence: %w", err)
	}
	if !found {
		state.adherence = tlsAdherenceNoOpinion
	}
	return state, nil
}

var errAPIUnavailable = errors.New("APIServer API unavailable")

func isAPIUnavailable(err error) bool {
	return errors.Is(err, errAPIUnavailable) || meta.IsNoMatchError(err)
}

func isTransient(err error) bool {
	return apierrors.IsServiceUnavailable(err) || apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) ||
		errors.Is(err, context.DeadlineExceeded)
}

func profileSpec(profile *configv1.TLSSecurityProfile) (*configv1.TLSProfileSpec, error) {
	if profile == nil || profile.Type == "" {
		return configv1.TLSProfiles[configv1.TLSProfileIntermediateType], nil
	}
	if profile.Type == configv1.TLSProfileCustomType {
		if profile.Custom == nil {
			return nil, errors.New("Custom TLS profile is missing custom settings")
		}
		return &profile.Custom.TLSProfileSpec, nil
	}
	spec, found := configv1.TLSProfiles[profile.Type]
	if !found {
		return nil, fmt.Errorf("unsupported TLS profile type %q", profile.Type)
	}
	return spec, nil
}

func tlsOptions(spec *configv1.TLSProfileSpec) ([]func(*tls.Config), error) {
	version, err := tlsVersion(spec.MinTLSVersion)
	if err != nil {
		return nil, err
	}
	ciphers, unsupported := cipherSuites(spec.Ciphers)
	if len(unsupported) > 0 {
		ctrl.Log.WithName("tls-profile").Info("TLS profile contains unsupported cipher suites", "ciphers", unsupported)
	}
	if version < tls.VersionTLS13 && len(ciphers) == 0 {
		if len(spec.Ciphers) == 0 {
			return nil, errors.New("TLS profile has no cipher suites for TLS <= 1.2")
		}
		return nil, errors.New("TLS profile has no supported cipher suites for TLS <= 1.2")
	}
	curves, unsupportedGroups := curvePreferences(spec.Groups)
	if len(unsupportedGroups) > 0 {
		ctrl.Log.WithName("tls-profile").Info("TLS profile contains unsupported groups", "groups", unsupportedGroups)
	}
	if len(spec.Groups) > 0 && len(curves) == 0 {
		return nil, errors.New("TLS profile has no supported groups")
	}
	return []func(*tls.Config){func(config *tls.Config) {
		config.MinVersion = version
		if version < tls.VersionTLS13 && len(spec.Ciphers) > 0 {
			config.CipherSuites = ciphers
		}
		if len(spec.Groups) > 0 {
			config.CurvePreferences = curves
		}
		config.NextProtos = []string{"h2", "http/1.1"}
	}}, nil
}

func tlsVersion(version configv1.TLSProtocolVersion) (uint16, error) {
	if version == "" {
		version = configv1.VersionTLS12
	}
	switch version {
	case configv1.VersionTLS10:
		return tls.VersionTLS10, nil
	case configv1.VersionTLS11:
		return tls.VersionTLS11, nil
	case configv1.VersionTLS12:
		return tls.VersionTLS12, nil
	case configv1.VersionTLS13:
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported minimum TLS version %q", version)
	}
}

func cipherSuites(names []string) ([]uint16, []string) {
	known := make(map[string]uint16)
	for _, suite := range append(tls.CipherSuites(), tls.InsecureCipherSuites()...) {
		known[suite.Name] = suite.ID
	}
	aliases := map[string]string{
		"ECDHE-ECDSA-AES128-GCM-SHA256": "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
		"ECDHE-RSA-AES128-GCM-SHA256":   "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		"ECDHE-ECDSA-AES256-GCM-SHA384": "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
		"ECDHE-RSA-AES256-GCM-SHA384":   "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
		"ECDHE-ECDSA-CHACHA20-POLY1305": "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
		"ECDHE-RSA-CHACHA20-POLY1305":   "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
		"ECDHE-ECDSA-AES128-SHA256":     "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256",
		"ECDHE-RSA-AES128-SHA256":       "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256",
		"AES128-GCM-SHA256":             "TLS_RSA_WITH_AES_128_GCM_SHA256",
		"AES256-GCM-SHA384":             "TLS_RSA_WITH_AES_256_GCM_SHA384",
		"AES128-SHA256":                 "TLS_RSA_WITH_AES_128_CBC_SHA256",
		"ECDHE-ECDSA-AES128-SHA":        "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA",
		"ECDHE-RSA-AES128-SHA":          "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA",
		"ECDHE-ECDSA-AES256-SHA":        "TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA",
		"ECDHE-RSA-AES256-SHA":          "TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA",
		"AES128-SHA":                    "TLS_RSA_WITH_AES_128_CBC_SHA",
		"AES256-SHA":                    "TLS_RSA_WITH_AES_256_CBC_SHA",
		"DES-CBC3-SHA":                  "TLS_RSA_WITH_3DES_EDE_CBC_SHA",
		"ECDHE-RSA-DES-CBC3-SHA":        "TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA",
		"TLS_AES_128_GCM_SHA256":        "TLS_AES_128_GCM_SHA256",
		"TLS_AES_256_GCM_SHA384":        "TLS_AES_256_GCM_SHA384",
		"TLS_CHACHA20_POLY1305_SHA256":  "TLS_CHACHA20_POLY1305_SHA256",
	}
	var suites []uint16
	var unsupported []string
	for _, name := range names {
		ianaName := name
		if alias, found := aliases[name]; found {
			ianaName = alias
		}
		id, found := known[ianaName]
		if !found {
			unsupported = append(unsupported, name)
			continue
		}
		if id >= 0x1300 && id <= 0x13ff {
			continue
		}
		suites = append(suites, id)
	}
	return suites, unsupported
}

func curvePreferences(groups []configv1.TLSGroup) ([]tls.CurveID, []string) {
	known := map[configv1.TLSGroup]tls.CurveID{
		configv1.TLSGroupX25519MLKEM768:     tls.X25519MLKEM768,
		configv1.TLSGroupX25519:             tls.X25519,
		configv1.TLSGroupSecP256r1:          tls.CurveP256,
		configv1.TLSGroupSecP384r1:          tls.CurveP384,
		configv1.TLSGroupSecP521r1:          tls.CurveP521,
		configv1.TLSGroupSecP256r1MLKEM768:  tls.SecP256r1MLKEM768,
		configv1.TLSGroupSecP384r1MLKEM1024: tls.SecP384r1MLKEM1024,
	}
	var curves []tls.CurveID
	var unsupported []string
	for _, group := range groups {
		curve, found := known[group]
		if !found {
			unsupported = append(unsupported, string(group))
			continue
		}
		curves = append(curves, curve)
	}
	return curves, unsupported
}
