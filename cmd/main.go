/*
Copyright 2023.

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

package main

import (
	"flag"
	"fmt"
	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	tasv1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	tasv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	kueuev1beta1 "sigs.k8s.io/kueue/apis/kueue/v1beta1"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tasv1alpha1.AddToScheme(scheme))
	utilruntime.Must(tasv1.AddToScheme(scheme))
	utilruntime.Must(lmesv1alpha1.AddToScheme(scheme))
	utilruntime.Must(evalhubv1alpha1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(kservev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kservev1beta1.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(kueuev1beta1.AddToScheme(scheme))
	utilruntime.Must(gorchv1alpha1.AddToScheme(scheme))
	utilruntime.Must(nemoguardrailsv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var configMap string
	var enabledServices controllers.EnabledServices
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address and port the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Var(&enabledServices, "enable-services", "Specify a list of services to enable and use ',' as the separator")
	flag.StringVar(&configMap, "configmap", constants.ConfigMap, "The configmap that stores settings for the operator")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if enabledServices.Empty() {
		setupLog.Error(fmt.Errorf("no service is specified"), "please specify at least one service")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                server.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "b7e9931f.trustyai.opendatahub.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	recorder := mgr.GetEventRecorderFor("trustyai-service-operator")

	ns, err := utils.GetNamespace()
	if err != nil {
		setupLog.Error(err, "unable to operator's namespace")
	}

	if err = controllers.SetupControllers(enabledServices, mgr, ns, configMap, recorder); err != nil {
		setupLog.Error(err, "unable to initialize controller(s)")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
