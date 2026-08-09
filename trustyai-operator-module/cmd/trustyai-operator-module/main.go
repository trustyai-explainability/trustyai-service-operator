package main

import (
	"flag"
	"fmt"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/gc"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/deploy"
	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	"github.com/trustyai-explainability/trustyai-operator-module/pkg/trustyaimodule"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(platformv1alpha1.AddToScheme(scheme))
	utilruntime.Must(extv1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	manifestsPath := "/opt/manifests-template"
	if info, err := os.Stat(manifestsPath); err != nil || !info.IsDir() {
		setupLog.Error(fmt.Errorf("manifests template directory not found or not a directory: %s", manifestsPath), "startup check failed")
		os.Exit(1)
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		setupLog.Error(fmt.Errorf("POD_NAMESPACE environment variable is not set"), "startup check failed")
		os.Exit(1)
	}

	// Applications namespace is where the workload operator is deployed.
	// Defaults to the operator namespace when not explicitly set.
	applicationsNamespace := os.Getenv("APPLICATIONS_NAMESPACE")
	if applicationsNamespace == "" {
		applicationsNamespace = namespace
	}

	// Scope the cache to the namespaces we actually care about. Cluster-scoped
	// resources (e.g. the TrustyAI CR itself) are always watched cluster-wide
	// regardless of this setting.
	watchNamespaces := map[string]cache.Config{
		namespace:              {},
		applicationsNamespace:  {},
	}

	restConfig := ctrl.GetConfigOrDie()

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                  scheme,
		Metrics:                 metricsserver.Options{BindAddress: metricsAddr},
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "trustyai-operator-module-controller-manager",
		LeaderElectionNamespace: namespace,
		HealthProbeBindAddress:  probeAddr,
		Cache:                   cache.Options{DefaultNamespaces: watchNamespaces},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	dynamicCli, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		setupLog.Error(err, "unable to create dynamic client")
		os.Exit(1)
	}

	discoveryCli, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		setupLog.Error(err, "unable to create discovery client")
		os.Exit(1)
	}

	deployer := deploy.NewDeployer(
		deploy.WithFieldOwner(trustyaimodule.FieldManagerModule),
		deploy.WithApplyOrder(),
	)

	garbageCollector := gc.New(
		gc.InNamespace(applicationsNamespace),
	)

	if err := (&trustyaimodule.TrustyAIModuleReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		Namespace:             namespace,
		ApplicationsNamespace: applicationsNamespace,
		ManifestsTemplatePath: manifestsPath,
		Deployer:              deployer,
		DynamicClient:         dynamicCli,
		DiscoveryClient:       discoveryCli,
		GarbageCollector:      garbageCollector,
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

	setupLog.Info("starting trustyai-operator-module manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
