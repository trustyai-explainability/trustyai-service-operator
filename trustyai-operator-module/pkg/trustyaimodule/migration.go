package trustyaimodule

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
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
// It is a one-time migration; subsequent calls are no-ops once the annotation is set.
func (r *TrustyAIModuleReconciler) adoptInTreeResources(ctx context.Context, module *platformv1alpha1.TrustyAI) error {
	logger := log.FromContext(ctx)

	if _, ok := module.Annotations[SSAAdoptionAnnotationKey]; ok {
		logger.V(1).Info("SSA adoption already completed, skipping")
		return nil
	}

	logger.Info("Starting SSA adoption of in-tree resources")

	adoptedCount := 0

	count, err := r.adoptConfigMaps(ctx)
	if err != nil {
		logger.Error(err, "Failed to adopt ConfigMaps")
		return err
	}
	adoptedCount += count

	count, err = r.adoptDeployments(ctx)
	if err != nil {
		logger.Error(err, "Failed to adopt Deployments")
		return err
	}
	adoptedCount += count

	count, err = r.adoptRBACResources(ctx)
	if err != nil {
		logger.Error(err, "Failed to adopt RBAC resources")
		return err
	}
	adoptedCount += count

	count, err = r.adoptServices(ctx)
	if err != nil {
		logger.Error(err, "Failed to adopt Services")
		return err
	}
	adoptedCount += count

	logger.Info("SSA adoption completed", "resourcesAdopted", adoptedCount)
	r.EventRecorder.Event(module, "Normal", "MigrationCompleted", fmt.Sprintf("Successfully adopted %d in-tree resources", adoptedCount))

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

func (r *TrustyAIModuleReconciler) adoptConfigMaps(ctx context.Context) (int, error) {
	logger := log.FromContext(ctx)

	configMapList := &corev1.ConfigMapList{}
	labelSelector := labels.SelectorFromSet(labels.Set{InTreeManagedByLabel: "true"})

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

func (r *TrustyAIModuleReconciler) adoptDeployments(ctx context.Context) (int, error) {
	logger := log.FromContext(ctx)

	deploymentList := &appsv1.DeploymentList{}
	labelSelector := labels.SelectorFromSet(labels.Set{InTreeManagedByLabel: "true"})

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

// adoptRBACResources adopts RoleBindings managed by the in-tree component.
func (r *TrustyAIModuleReconciler) adoptRBACResources(ctx context.Context) (int, error) {
	logger := log.FromContext(ctx)
	labelSelector := labels.SelectorFromSet(labels.Set{InTreeManagedByLabel: "true"})
	count := 0

	roleBindingList := &rbacv1.RoleBindingList{}
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

	return count, nil
}

func (r *TrustyAIModuleReconciler) adoptServices(ctx context.Context) (int, error) {
	logger := log.FromContext(ctx)

	serviceList := &corev1.ServiceList{}
	labelSelector := labels.SelectorFromSet(labels.Set{InTreeManagedByLabel: "true"})

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

func (r *TrustyAIModuleReconciler) adoptResource(ctx context.Context, obj client.Object) error {
	key := types.NamespacedName{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}

	current := obj.DeepCopyObject().(client.Object)
	if err := r.Get(ctx, key, current); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get resource: %w", err)
	}

	annotations := current.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[AdoptedFromAnnotationKey] = "in-tree-component"
	current.SetAnnotations(annotations)

	current.SetManagedFields(nil)
	current.SetResourceVersion("")

	gvk, err := r.Client.GroupVersionKindFor(current)
	if err != nil {
		return fmt.Errorf("failed to get GVK for resource: %w", err)
	}
	current.GetObjectKind().SetGroupVersionKind(gvk)

	applyOpts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(FieldManagerModule),
	}

	if err := r.Patch(ctx, current, client.Apply, applyOpts...); err != nil {
		return fmt.Errorf("failed to apply with force ownership: %w", err)
	}

	return nil
}
