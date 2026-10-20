package tas

import (
	"context"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	trustyAINamespaceLabel           = "trustyai.opendatahub.io/enabled"
	trustyAINamespaceLabelAnnotation = "trustyai.opendatahub.io/namespace-label-managed"
)

func (r *TrustyAIServiceReconciler) ensureTrustyAINamespaceLabel(ctx context.Context, namespaceName string) error {
	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace); err != nil {
		return err
	}

	if _, exists := namespace.Labels[trustyAINamespaceLabel]; exists {
		return nil
	}
	original := namespace.DeepCopy()
	if namespace.Labels == nil {
		namespace.Labels = make(map[string]string)
	}
	namespace.Labels[trustyAINamespaceLabel] = "true"
	if namespace.Annotations == nil {
		namespace.Annotations = make(map[string]string)
	}
	namespace.Annotations[trustyAINamespaceLabelAnnotation] = "true"
	return r.Patch(ctx, namespace, client.MergeFrom(original))
}

func (r *TrustyAIServiceReconciler) removeTrustyAINamespaceLabel(
	ctx context.Context,
	instance *trustyaiopendatahubiov1.TrustyAIService,
) error {
	var services trustyaiopendatahubiov1.TrustyAIServiceList
	if err := r.List(ctx, &services, client.InNamespace(instance.Namespace)); err != nil {
		return err
	}
	for _, service := range services.Items {
		if service.Name != instance.Name && service.DeletionTimestamp.IsZero() {
			return nil
		}
	}

	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Namespace}, namespace); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if namespace.Annotations[trustyAINamespaceLabelAnnotation] != "true" {
		return nil
	}

	original := namespace.DeepCopy()
	if namespace.Labels[trustyAINamespaceLabel] == "true" {
		delete(namespace.Labels, trustyAINamespaceLabel)
	}
	delete(namespace.Annotations, trustyAINamespaceLabelAnnotation)
	return r.Patch(ctx, namespace, client.MergeFrom(original))
}
