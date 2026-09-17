package trustyaimodule

import (
	"context"
	"fmt"

	odhlabels "github.com/opendatahub-io/odh-platform-utilities/pkg/metadata/labels"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// trustyAIModulePartOf is the platform.opendatahub.io/part-of label value stamped
// on resources deployed by the module operator (lowercase TrustyAI kind).
const trustyAIModulePartOf = "trustyai"

// cleanupModuleResources tears down everything the module operator deployed:
// namespace-scoped operands, the DSC ConfigMap, and cluster-scoped RBAC that
// cannot rely on owner-reference GC.
func (r *TrustyAIModuleReconciler) cleanupModuleResources(ctx context.Context) error {
	if err := r.deleteDeployedOperands(ctx); err != nil {
		return err
	}
	if err := r.deleteDSCConfigMap(ctx); err != nil {
		return err
	}
	if err := r.deleteClusterScopedRBAC(ctx); err != nil {
		return err
	}
	return nil
}

func (r *TrustyAIModuleReconciler) deleteDeployedOperands(ctx context.Context) error {
	if r.Deployer == nil {
		return nil
	}

	if err := r.deleteRenderedOperands(ctx); err != nil {
		return err
	}
	return r.deleteLabeledOperands(ctx)
}

func (r *TrustyAIModuleReconciler) deleteRenderedOperands(ctx context.Context) error {
	logger := log.FromContext(ctx)

	resources, err := RenderManifests(ctx, r.ManifestsTemplatePath, r.Namespace, false)
	if err != nil {
		return fmt.Errorf("rendering manifests for operand cleanup: %w", err)
	}

	for i := len(resources) - 1; i >= 0; i-- {
		resource := &resources[i]
		if shouldSkipOperandDeletion(resource) {
			continue
		}

		lookup := &unstructured.Unstructured{}
		lookup.SetGroupVersionKind(resource.GroupVersionKind())
		key := types.NamespacedName{Name: resource.GetName(), Namespace: resource.GetNamespace()}
		if err := r.Get(ctx, key, lookup); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get %s %s for deletion: %w", resource.GetKind(), resource.GetName(), err)
		}

		logger.Info("Deleting deployed operand",
			"kind", resource.GetKind(),
			"name", resource.GetName(),
			"namespace", resource.GetNamespace(),
		)
		if err := r.Delete(ctx, lookup); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete %s %s: %w", resource.GetKind(), resource.GetName(), err)
		}
	}

	return nil
}

func (r *TrustyAIModuleReconciler) deleteLabeledOperands(ctx context.Context) error {
	logger := log.FromContext(ctx)
	selector := labels.SelectorFromSet(labels.Set{odhlabels.PlatformPartOf: trustyAIModulePartOf})
	listOpts := []client.ListOption{
		client.InNamespace(r.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	}

	deployments := &appsv1.DeploymentList{}
	if err := r.List(ctx, deployments, listOpts...); err != nil {
		return fmt.Errorf("listing Deployments for cleanup: %w", err)
	}
	for i := range deployments.Items {
		dep := &deployments.Items[i]
		logger.Info("Deleting labeled operand", "kind", "Deployment", "name", dep.Name, "namespace", dep.Namespace)
		if err := r.Delete(ctx, dep); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete Deployment %s: %w", dep.Name, err)
		}
	}

	services := &corev1.ServiceList{}
	if err := r.List(ctx, services, listOpts...); err != nil {
		return fmt.Errorf("listing Services for cleanup: %w", err)
	}
	for i := range services.Items {
		svc := &services.Items[i]
		logger.Info("Deleting labeled operand", "kind", "Service", "name", svc.Name, "namespace", svc.Namespace)
		if err := r.Delete(ctx, svc); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete Service %s: %w", svc.Name, err)
		}
	}

	serviceAccounts := &corev1.ServiceAccountList{}
	if err := r.List(ctx, serviceAccounts, listOpts...); err != nil {
		return fmt.Errorf("listing ServiceAccounts for cleanup: %w", err)
	}
	for i := range serviceAccounts.Items {
		sa := &serviceAccounts.Items[i]
		logger.Info("Deleting labeled operand", "kind", "ServiceAccount", "name", sa.Name, "namespace", sa.Namespace)
		if err := r.Delete(ctx, sa); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete ServiceAccount %s: %w", sa.Name, err)
		}
	}

	configMaps := &corev1.ConfigMapList{}
	if err := r.List(ctx, configMaps, listOpts...); err != nil {
		return fmt.Errorf("listing ConfigMaps for cleanup: %w", err)
	}
	for i := range configMaps.Items {
		cm := &configMaps.Items[i]
		if cm.Name == DSCConfigMapName {
			continue
		}
		logger.Info("Deleting labeled operand", "kind", "ConfigMap", "name", cm.Name, "namespace", cm.Namespace)
		if err := r.Delete(ctx, cm); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete ConfigMap %s: %w", cm.Name, err)
		}
	}

	roleBindings := &rbacv1.RoleBindingList{}
	if err := r.List(ctx, roleBindings, listOpts...); err != nil {
		return fmt.Errorf("listing RoleBindings for cleanup: %w", err)
	}
	for i := range roleBindings.Items {
		rb := &roleBindings.Items[i]
		logger.Info("Deleting labeled operand", "kind", "RoleBinding", "name", rb.Name, "namespace", rb.Namespace)
		if err := r.Delete(ctx, rb); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete RoleBinding %s: %w", rb.Name, err)
		}
	}

	return nil
}

func shouldSkipOperandDeletion(resource *unstructured.Unstructured) bool {
	switch resource.GetKind() {
	case "CustomResourceDefinition", "ClusterRole", "ClusterRoleBinding":
		return true
	default:
		return false
	}
}
