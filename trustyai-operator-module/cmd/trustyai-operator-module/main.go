package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	pkgtls "github.com/opendatahub-io/odh-platform-utilities/framework/tls"
	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	"github.com/trustyai-explainability/trustyai-operator-module/pkg/trustyaimodule"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))
	utilruntime.Must(platformv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8443", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	manifestsPath := "/opt/manifests-template"
	if info, err := os.Stat(manifestsPath); err != nil || !info.IsDir() {
		setupErr := fmt.Errorf("manifests template directory not found or not a directory: %s", manifestsPath)
		setupLog.Error(setupErr, "startup check failed")
		os.Exit(1)
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		setupLog.Error(fmt.Errorf("POD_NAMESPACE environment variable is not set"), "startup check failed")
		os.Exit(1)
	}

	config := ctrl.GetConfigOrDie()
	bootstrapClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to create bootstrap client for TLS profile")
		os.Exit(1)
	}
	tlsCtx, tlsCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer tlsCancel()
	tlsResult, err := pkgtls.LoadWithAdherence(tlsCtx, bootstrapClient)
	if err != nil {
		setupLog.Error(err, "unable to resolve cluster TLS profile")
		os.Exit(1)
	}

	// Only apply the cluster profile under adhering policies (Strict and
	// unknown). NoOpinion and Legacy keep the component's serving defaults:
	// SecureServing with Go's TLS 1.2 minimum and default cipher suites.
	var metricsTLSOpts []func(*tls.Config)
	if pkgtls.ShouldHonorClusterTLSProfile(tlsResult.AdherencePolicy) {
		tlsOption, unsupported, err := pkgtls.ConfigFromProfileStrict(tlsResult.Spec)
		if err != nil {
			setupLog.Error(err, "unable to configure metrics TLS")
			os.Exit(1)
		}
		if len(unsupported) > 0 {
			setupLog.Info("TLS profile contains settings unsupported by Go", "unsupported", unsupported)
		}
		metricsTLSOpts = append(metricsTLSOpts, tlsOption)
	}

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: true,
			TLSOpts:       metricsTLSOpts,
		},
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "trustyai-operator-module-controller-manager",
		LeaderElectionNamespace: namespace,
		HealthProbeBindAddress:  probeAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := (&trustyaimodule.TrustyAIModuleReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Namespace:             namespace,
		ManifestsTemplatePath: manifestsPath,
		Deployer:              trustyaimodule.NewModuleDeployer(),
		EventRecorder:         mgr.GetEventRecorderFor("trustyai-module"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TrustyAI")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	managerCtx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
	defer cancel()
	if tlsResult.Watchable {
		watcher := &pkgtls.SecurityProfileWatcher{
			Client:                    mgr.GetClient(),
			InitialTLSProfileSpec:     tlsResult.Spec,
			InitialTLSAdherencePolicy: tlsResult.AdherencePolicy,
			OnProfileChange: func(context.Context, configv1.TLSProfileSpec, configv1.TLSProfileSpec) {
				setupLog.Info("TLS security profile changed, restarting for metrics TLS refresh")
				cancel()
			},
			OnAdherencePolicyChange: func(context.Context, configv1.TLSAdherencePolicy, configv1.TLSAdherencePolicy) {
				setupLog.Info("TLS adherence policy changed, restarting for metrics TLS refresh")
				cancel()
			},
		}
		if err := watcher.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to set up TLS profile watcher")
			os.Exit(1)
		}
	}

	setupLog.Info("starting trustyai-operator-module manager")
	if err := mgr.Start(managerCtx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
