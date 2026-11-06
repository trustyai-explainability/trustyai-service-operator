package utils

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// WorkloadPodLabelsRestored reports the Pods whose reserved labels had to be
// rewritten. The caller owns the diagnostic, because what counts as news differs
// between components.
type WorkloadPodLabelsRestored struct {
	Namespace string
	Name      string
}

// RepairOwnedWorkloadNetworkPolicyPodLabels re-stamps the reserved identity
// labels on the Pods of a Deployment the owner controls.
//
// The labels are written to the Deployment template, so every Pod created from
// that template is selected by the authored NetworkPolicy. A running Pod whose
// labels are removed leaves that selector and would keep serving outside the
// policy it was meant to be governed by, so the selection is repaired rather
// than treated as authoritative drift. Selection labels are ordinary Pod
// metadata: this restores the intended state, it cannot prevent a principal with
// Pod write access from changing it again.
//
// Only Pods whose ownership chain is proven are touched. A Pod is relabelled
// when its controller is a ReplicaSet whose UID matches the reference and whose
// own controller is the supplied Deployment; anything else is skipped, because a
// principal with Pod-create permission can reproduce a Deployment selector and
// such a Pod must never inherit this workload's grants.
//
// The caller is responsible for the ordering invariant: reconcile the
// NetworkPolicy first, then repair Pod labels, so a Pod never enters a selector
// whose allow rules have not converged in the API server yet.
func RepairOwnedWorkloadNetworkPolicyPodLabels(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	deployment *appsv1.Deployment,
	scheme *runtime.Scheme,
	owner client.Object,
	identity NetworkPolicyIdentity,
) ([]WorkloadPodLabelsRestored, error) {
	if deployment == nil || owner == nil || scheme == nil {
		return nil, fmt.Errorf("deployment, owner and scheme are required")
	}
	if c == nil {
		return nil, fmt.Errorf("a client is required to repair Pod labels")
	}
	if reader == nil {
		reader = c
	}

	ownerWithGVK, err := ownerWithResolvedGVK(owner, scheme)
	if err != nil {
		return nil, err
	}
	if !identityOwnedBy(identity, ownerWithGVK) {
		return nil, fmt.Errorf("identity does not belong to the supplied policy owner")
	}
	if deployment.Namespace != owner.GetNamespace() {
		return nil, fmt.Errorf("deployment %s/%s is outside the policy owner namespace", deployment.Namespace, deployment.Name)
	}
	if !matchesController(deployment, ownerWithGVK) {
		return nil, fmt.Errorf("deployment %s/%s is not controlled by this policy owner", deployment.Namespace, deployment.Name)
	}
	if deployment.Spec.Selector == nil {
		return nil, fmt.Errorf("deployment %s/%s has no selector to find its Pods", deployment.Namespace, deployment.Name)
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("convert deployment selector for %s/%s: %w", deployment.Namespace, deployment.Name, err)
	}
	identityLabels, err := identity.Labels()
	if err != nil {
		return nil, err
	}

	pods := &corev1.PodList{}
	if err := reader.List(ctx, pods, client.InNamespace(deployment.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("list Pods for %s/%s: %w", deployment.Namespace, deployment.Name, err)
	}

	logger := log.FromContext(ctx)
	restored := make([]WorkloadPodLabelsRestored, 0, len(pods.Items))
	for index := range pods.Items {
		pod := &pods.Items[index]
		if !podBelongsToDeployment(reader, ctx, pod, deployment) {
			logger.Info("Skipping Pod whose ownership chain does not reach the workload Deployment",
				"pod", client.ObjectKeyFromObject(pod), "deployment", client.ObjectKeyFromObject(deployment))
			continue
		}
		before := pod.DeepCopy()
		if pod.Labels == nil {
			pod.Labels = map[string]string{}
		}
		changed := false
		for key, value := range identityLabels {
			if pod.Labels[key] != value {
				pod.Labels[key] = value
				changed = true
			}
		}
		if !changed {
			continue
		}
		// Optimistic lock: never relabel a replacement that took the same name.
		if err := c.Patch(ctx, pod, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return restored, fmt.Errorf("restore NetworkPolicy identity on Pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
		restored = append(restored, WorkloadPodLabelsRestored{Namespace: pod.Namespace, Name: pod.Name})
	}
	return restored, nil
}

// podBelongsToDeployment proves Pod -> ReplicaSet -> Deployment before any label
// is written. Production reads bypass the metadata-only Pod informer, so the
// caller passes an uncached reader where one is available.
func podBelongsToDeployment(reader client.Reader, ctx context.Context, pod *corev1.Pod, deployment *appsv1.Deployment) bool {
	ref := metav1.GetControllerOf(pod)
	if ref == nil || ref.Kind != "ReplicaSet" {
		return false
	}
	refGV, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil || refGV.Group != "apps" {
		return false
	}
	replicaSet := &appsv1.ReplicaSet{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: ref.Name}, replicaSet); err != nil {
		if !apierrors.IsNotFound(err) {
			log.FromContext(ctx).Error(err, "Could not read the ReplicaSet of a selected Pod",
				"pod", client.ObjectKeyFromObject(pod), "replicaset", ref.Name)
		}
		return false
	}
	if replicaSet.UID != ref.UID {
		return false
	}
	ownerRef := metav1.GetControllerOf(replicaSet)
	if ownerRef == nil || ownerRef.Kind != "Deployment" {
		return false
	}
	ownerGV, err := schema.ParseGroupVersion(ownerRef.APIVersion)
	if err != nil || ownerGV.Group != "apps" {
		return false
	}
	return ownerRef.UID == deployment.UID && ownerRef.Name == deployment.Name
}

func ownerWithResolvedGVK(owner client.Object, scheme *runtime.Scheme) (client.Object, error) {
	gvk, err := apiutil.GVKForObject(owner, scheme)
	if err != nil {
		return nil, fmt.Errorf("resolve policy owner GVK: %w", err)
	}
	resolved, ok := owner.DeepCopyObject().(client.Object)
	if !ok {
		return nil, fmt.Errorf("policy owner is not a client.Object")
	}
	resolved.GetObjectKind().SetGroupVersionKind(gvk)
	return resolved, nil
}
