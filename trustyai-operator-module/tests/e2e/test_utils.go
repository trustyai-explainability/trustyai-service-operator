//go:build e2e

//nolint:testpackage
package e2e

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
)

const (
	// OperatorNamespace is the namespace the module operator is deployed into
	// by config/default (namePrefix "trustyai-operator-module-" is applied to
	// resource names, not the "system" namespace itself).
	OperatorNamespace = "system"

	// DeploymentName is the module operator's own Deployment name.
	DeploymentName = "trustyai-operator-module-controller-manager"

	// InstanceName is the only name the singleton CRD's CEL rule accepts.
	InstanceName = "default"

	pollInterval = 2 * time.Second
	pollTimeout  = 2 * time.Minute
)

// prometheusGVK matches dependencies.go's required-dependency check. The live
// cluster has no Prometheus operator, so the lifecycle test seeds a bare
// instance via the same minimal CRD fixture used by the envtest suite
// (tests/crds/monitoring.coreos.com_prometheuses.yaml) to clear that gate -
// otherwise reconciliation never proceeds far enough to create/delete the
// DSC ConfigMap this test exercises.
var prometheusGVK = schema.GroupVersionKind{
	Group:   "monitoring.coreos.com",
	Version: "v1",
	Kind:    "Prometheus",
}

func createPrometheusInstance(ctx context.Context, namespace, name string) error {
	prom := &unstructured.Unstructured{}
	prom.SetGroupVersionKind(prometheusGVK)
	prom.SetName(name)
	prom.SetNamespace(namespace)
	return k8sClient.Create(ctx, prom)
}

func deletePrometheusInstance(ctx context.Context, namespace, name string) error {
	prom := &unstructured.Unstructured{}
	prom.SetGroupVersionKind(prometheusGVK)
	prom.SetName(name)
	prom.SetNamespace(namespace)
	err := k8sClient.Delete(ctx, prom)
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

// k8sClient is the real cluster client shared by every e2e test in this package.
var k8sClient client.Client

// SetupTestEnv builds a real client.Client against the cluster pointed to by
// the ambient kubeconfig (KUBECONFIG env var or in-cluster config). Unlike the
// envtest-based suite in pkg/trustyaimodule, this drives an actual Kind (or
// any real) cluster, so Deployments/Pods genuinely run.
func SetupTestEnv() error {
	scheme := clientgoscheme.Scheme
	utilruntime.Must(platformv1alpha1.AddToScheme(scheme))

	cfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	return nil
}

func newManagedTrustyAI(name string) *platformv1alpha1.TrustyAI {
	return &platformv1alpha1.TrustyAI{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

// requireDeploymentReady polls until the named Deployment has at least one
// available replica, or the timeout elapses.
func requireDeploymentReady(ctx context.Context, namespace, name string) error {
	key := types.NamespacedName{Name: name, Namespace: namespace}
	return wait.PollUntilContextTimeout(ctx, pollInterval, pollTimeout, true, func(ctx context.Context) (bool, error) {
		dep := &appsv1.Deployment{}
		if err := k8sClient.Get(ctx, key, dep); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return dep.Status.AvailableReplicas > 0, nil
	})
}

// getModule fetches the singleton TrustyAI CR.
func getModule(ctx context.Context) (*platformv1alpha1.TrustyAI, error) {
	module := &platformv1alpha1.TrustyAI{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: InstanceName}, module)
	return module, err
}

// waitForModuleGone polls until the singleton TrustyAI CR no longer exists.
func waitForModuleGone(ctx context.Context) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, pollTimeout, true, func(ctx context.Context) (bool, error) {
		_, err := getModule(ctx)
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}
