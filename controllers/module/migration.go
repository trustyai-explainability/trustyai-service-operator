package module

import (
	"context"
	"fmt"

	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// adoptInTreeResources performs Server-Side Apply adoption of in-tree managed resources.
// This is a one-time migration that takes ownership from the in-tree TrustyAI component.
// Returns error if adoption fails; no-op if already adopted or no resources found.
func (r *TrustyAIReconciler) adoptInTreeResources(ctx context.Context, module *modulev1alpha1.TrustyAI) error {
	logger := log.FromContext(ctx)

	// Check if adoption already completed
	if _, ok := module.Annotations[SSAAdoptionAnnotationKey]; ok {
		logger.V(1).Info("SSA adoption already completed, skipping")
		return nil
	}

	logger.Info("Starting SSA adoption of in-tree resources")

	adoptedCount := 0

	// Adopt ConfigMaps with in-tree labels
	count, err := r.adoptConfigMaps(ctx)
	if err != nil {
		logger.Error(err, "Failed to adopt ConfigMaps")
		return err
	}
	adoptedCount += count

	// Adopt Deployments
	count, err = r.adoptDeployments(ctx)
	if err != nil {
		logger.Error(err, "Failed to adopt Deployments")
		return err
	}
	adoptedCount += count

	// Adopt RBAC resources
	count, err = r.adoptRBACResources(ctx)
	if err != nil {
		logger.Error(err, "Failed to adopt RBAC resources")
		return err
	}
	adoptedCount += count

	// Adopt Services
	count, err = r.adoptServices(ctx)
	if err != nil {
		logger.Error(err, "Failed to adopt Services")
		return err
	}
	adoptedCount += count

	logger.Info("SSA adoption completed", "resourcesAdopted", adoptedCount)
	r.EventRecorder.Event(module, "Normal", "MigrationCompleted", fmt.Sprintf("Successfully adopted %d in-tree resources", adoptedCount))

	// Mark adoption as completed
	if module.Annotations == nil {
		module.Annotations = make(map[string]string)
	}
	module.Annotations[SSAAdoptionAnnotationKey] = "true"

	if err := r.Update(ctx, module); err != nil {
		logger.Error(err, "Failed to mark adoption as completed")
		return fmt.Errorf("failed to update module with adoption annotation: %w", err)
	}

	return nil
}

// adoptConfigMaps adopts ConfigMaps created by in-tree component
func (r *TrustyAIReconciler) adoptConfigMaps(ctx context.Context) (int, error) {
	logger := log.FromContext(ctx)

	// List ConfigMaps with in-tree label
	configMapList := &corev1.ConfigMapList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		InTreeManagedByLabel: "true",
	})

	if err := r.List(ctx, configMapList, &client.ListOptions{
		Namespace:     r.Namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return 0, fmt.Errorf("failed to list in-tree ConfigMaps: %w", err)
	}

	count := 0
	for i := range configMapList.Items {
		cm := &configMapList.Items[i]
		logger.Info("Adopting ConfigMap", "name", cm.Name)

		if err := r.adoptResource(ctx, cm); err != nil {
			return count, fmt.Errorf("failed to adopt ConfigMap %s: %w", cm.Name, err)
		}
		count++
	}

	return count, nil
}

// adoptDeployments adopts Deployments created by in-tree component
func (r *TrustyAIReconciler) adoptDeployments(ctx context.Context) (int, error) {
	logger := log.FromContext(ctx)

	// List Deployments with in-tree label
	deploymentList := &appsv1.DeploymentList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		InTreeManagedByLabel: "true",
	})

	if err := r.List(ctx, deploymentList, &client.ListOptions{
		Namespace:     r.Namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return 0, fmt.Errorf("failed to list in-tree Deployments: %w", err)
	}

	count := 0
	for i := range deploymentList.Items {
		deploy := &deploymentList.Items[i]
		logger.Info("Adopting Deployment", "name", deploy.Name)

		if err := r.adoptResource(ctx, deploy); err != nil {
			return count, fmt.Errorf("failed to adopt Deployment %s: %w", deploy.Name, err)
		}
		count++
	}

	return count, nil
}

// adoptRBACResources adopts RoleBindings and ClusterRoleBindings
func (r *TrustyAIReconciler) adoptRBACResources(ctx context.Context) (int, error) {
	logger := log.FromContext(ctx)
	count := 0

	// Adopt RoleBindings
	roleBindingList := &rbacv1.RoleBindingList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		InTreeManagedByLabel: "true",
	})

	if err := r.List(ctx, roleBindingList, &client.ListOptions{
		Namespace:     r.Namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return 0, fmt.Errorf("failed to list in-tree RoleBindings: %w", err)
	}

	for i := range roleBindingList.Items {
		rb := &roleBindingList.Items[i]
		logger.Info("Adopting RoleBinding", "name", rb.Name)

		if err := r.adoptResource(ctx, rb); err != nil {
			return count, fmt.Errorf("failed to adopt RoleBinding %s: %w", rb.Name, err)
		}
		count++
	}

	// Adopt ClusterRoleBindings
	clusterRoleBindingList := &rbacv1.ClusterRoleBindingList{}

	if err := r.List(ctx, clusterRoleBindingList, &client.ListOptions{
		LabelSelector: labelSelector,
	}); err != nil {
		return count, fmt.Errorf("failed to list in-tree ClusterRoleBindings: %w", err)
	}

	for i := range clusterRoleBindingList.Items {
		crb := &clusterRoleBindingList.Items[i]
		logger.Info("Adopting ClusterRoleBinding", "name", crb.Name)

		if err := r.adoptResource(ctx, crb); err != nil {
			return count, fmt.Errorf("failed to adopt ClusterRoleBinding %s: %w", crb.Name, err)
		}
		count++
	}

	return count, nil
}

// adoptServices adopts Services created by in-tree component
func (r *TrustyAIReconciler) adoptServices(ctx context.Context) (int, error) {
	logger := log.FromContext(ctx)

	// List Services with in-tree label
	serviceList := &corev1.ServiceList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		InTreeManagedByLabel: "true",
	})

	if err := r.List(ctx, serviceList, &client.ListOptions{
		Namespace:     r.Namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return 0, fmt.Errorf("failed to list in-tree Services: %w", err)
	}

	count := 0
	for i := range serviceList.Items {
		svc := &serviceList.Items[i]
		logger.Info("Adopting Service", "name", svc.Name)

		if err := r.adoptResource(ctx, svc); err != nil {
			return count, fmt.Errorf("failed to adopt Service %s: %w", svc.Name, err)
		}
		count++
	}

	return count, nil
}

// adoptResource performs Server-Side Apply adoption for a generic resource
func (r *TrustyAIReconciler) adoptResource(ctx context.Context, obj client.Object) error {
	// Re-fetch the resource to get the latest version
	key := types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}

	// Create a new instance of the same type
	current := obj.DeepCopyObject().(client.Object)

	if err := r.Get(ctx, key, current); err != nil {
		if errors.IsNotFound(err) {
			return nil // Resource already gone, nothing to adopt
		}
		return fmt.Errorf("failed to get resource: %w", err)
	}

	// Set metadata to indicate module operator ownership
	annotations := current.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[AdoptedFromAnnotationKey] = "in-tree-component"
	current.SetAnnotations(annotations)

	// Clear managed fields (required for SSA)
	current.SetManagedFields(nil)
	// Clear resource version (SSA will set it)
	current.SetResourceVersion("")

	// Set GVK explicitly based on the object type
	// This is required because client.Apply needs TypeMeta populated
	gvk, err := r.Client.GroupVersionKindFor(current)
	if err != nil {
		return fmt.Errorf("failed to get GVK for resource: %w", err)
	}
	current.GetObjectKind().SetGroupVersionKind(gvk)

	// Use Server-Side Apply with ForceOwnership to take control
	applyOpts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(FieldManagerModule),
	}

	if err := r.Patch(ctx, current, client.Apply, applyOpts...); err != nil {
		return fmt.Errorf("failed to apply with force ownership: %w", err)
	}

	return nil
}
