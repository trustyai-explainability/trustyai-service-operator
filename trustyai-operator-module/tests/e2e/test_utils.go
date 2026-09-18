//go:build e2e

//nolint:testpackage
package e2e

import (
	"context"
	"fmt"
	"time"

	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	"github.com/trustyai-explainability/trustyai-operator-module/pkg/trustyaimodule"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// OperatorNamespace is the namespace the module operator is deployed into
	// by config/default (namePrefix "trustyai-operator-module-" is applied to
	// resource names, not the "system" namespace itself).
	OperatorNamespace = "system"

	// DeploymentName is the module operator's own Deployment name.
	DeploymentName = "trustyai-operator-module-controller-manager"

	// InstanceName is the only name the singleton CRD's CEL rule accepts.
	InstanceName = "default-trustyai"

	// WorkloadOperatorDeploymentName is the trustyai-service-operator Deployment
	// the module operator deploys into the applications namespace.
	WorkloadOperatorDeploymentName = trustyaimodule.OperatorDeploymentName

	// WorkloadOperatorMetricsServiceName is the metrics Service fronting the
	// deployed workload operator's kube-rbac-proxy.
	WorkloadOperatorMetricsServiceName = "trustyai-service-operator-controller-manager-metrics-service"

	pollInterval = 2 * time.Second
	pollTimeout  = 2 * time.Minute
)

var trustyAIServiceGVK = schema.GroupVersionKind{
	Group:   "trustyai.opendatahub.io",
	Version: "v1",
	Kind:    "TrustyAIService",
}

// prometheusGVK matches the dependency precondition checked by the module.
var prometheusGVK = schema.GroupVersionKind{
	Group:   "monitoring.coreos.com",
	Version: "v1",
	Kind:    "Prometheus",
}

// ensurePrometheusInstance reuses a workflow-owned resource when present. It
// returns true only when this test created the resource, so cleanup cannot
// delete an object owned by the surrounding test environment.
func ensurePrometheusInstance(ctx context.Context, namespace, name string) (bool, error) {
	prom := &unstructured.Unstructured{}
	prom.SetGroupVersionKind(prometheusGVK)
	prom.SetName(name)
	prom.SetNamespace(namespace)

	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, prom)
	if err == nil {
		return false, nil
	}
	if !errors.IsNotFound(err) {
		return false, err
	}

	if err := k8sClient.Create(ctx, prom); err != nil {
		if errors.IsAlreadyExists(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
		Spec: platformv1alpha1.TrustyAISpec{
			ManagementSpec: common.ManagementSpec{
				ManagementState: common.Managed,
			},
		},
	}
}

func newTASOnlyModule(name string) *platformv1alpha1.TrustyAI {
	module := newManagedTrustyAI(name)
	module.Spec.EnabledServices = platformv1alpha1.EnabledServices{TAS: true}
	return module
}

func createHealthyTrustyAIService(ctx context.Context, namespace, name string) error {
	operand := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "trustyai.opendatahub.io/v1",
		"kind":       "TrustyAIService",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]interface{}{
			"metrics": map[string]interface{}{"schedule": "0 0 * * *"},
			"storage": map[string]interface{}{"format": "PVC"},
		},
	}}
	operand.SetGroupVersionKind(trustyAIServiceGVK)
	if err := k8sClient.Create(ctx, operand); err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		status := &unstructured.Unstructured{}
		status.SetGroupVersionKind(trustyAIServiceGVK)
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, status); err != nil {
			return err
		}
		if err := unstructured.SetNestedSlice(status.Object, []interface{}{
			map[string]interface{}{
				"type":   "Ready",
				"status": "True",
				"reason": "Available",
			},
		}, "status", "conditions"); err != nil {
			return err
		}
		return k8sClient.Status().Update(ctx, status)
	})
}

func waitForModulePhase(ctx context.Context, phase common.Phase) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, pollTimeout, true, func(ctx context.Context) (bool, error) {
		module, err := getModule(ctx)
		if err != nil {
			return false, err
		}
		return module.Status.Phase == phase, nil
	})
}

func waitForModuleCondition(ctx context.Context, condType string, status metav1.ConditionStatus) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, pollTimeout, true, func(ctx context.Context) (bool, error) {
		module, err := getModule(ctx)
		if err != nil {
			return false, err
		}
		for i := range module.Status.Conditions {
			cond := module.Status.Conditions[i]
			if cond.Type == condType && cond.Status == status {
				return true, nil
			}
		}
		return false, nil
	})
}

func waitForResource(ctx context.Context, key types.NamespacedName, obj client.Object) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, pollTimeout, true, func(ctx context.Context) (bool, error) {
		err := k8sClient.Get(ctx, key, obj)
		if errors.IsNotFound(err) {
			return false, nil
		}
		return err == nil, err
	})
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
