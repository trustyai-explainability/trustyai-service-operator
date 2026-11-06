package evalhub

import (
	"context"
	"reflect"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// mapEvalHubPodToRequest resolves a Pod to the EvalHub that owns it, through the
// Pod's ReplicaSet and Deployment. Each hop is verified by UID, so a Pod that
// merely reproduces a Deployment's selector cannot steer a reconcile.
func (r *EvalHubReconciler) mapEvalHubPodToRequest(ctx context.Context, object client.Object) []reconcile.Request {
	if object == nil || object.GetNamespace() == "" {
		return nil
	}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	namespace := object.GetNamespace()

	replicaSetRef := metav1.GetControllerOf(object)
	if replicaSetRef == nil || replicaSetRef.Kind != "ReplicaSet" {
		return nil
	}
	if gv, err := schema.ParseGroupVersion(replicaSetRef.APIVersion); err != nil || gv.Group != "apps" {
		return nil
	}
	replicaSet := &appsv1.ReplicaSet{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: replicaSetRef.Name}, replicaSet); err != nil ||
		replicaSet.UID != replicaSetRef.UID {
		return nil
	}

	deploymentRef := metav1.GetControllerOf(replicaSet)
	if deploymentRef == nil || deploymentRef.Kind != "Deployment" {
		return nil
	}
	if gv, err := schema.ParseGroupVersion(deploymentRef.APIVersion); err != nil || gv.Group != "apps" {
		return nil
	}
	deployment := &appsv1.Deployment{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: deploymentRef.Name}, deployment); err != nil ||
		deployment.UID != deploymentRef.UID {
		return nil
	}

	ownerRef := metav1.GetControllerOf(deployment)
	if ownerRef == nil || ownerRef.Kind != evalhubv1.KindName {
		return nil
	}
	if gv, err := schema.ParseGroupVersion(ownerRef.APIVersion); err != nil || gv.Group != evalhubv1.GroupVersion.Group {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: namespace, Name: ownerRef.Name}}}
}

// hasEvalHubPodIdentityHint recognises a Pod this controller may have labelled.
// The reserved owner label is checked as well as the workload labels, because a
// strip that removes the display labels must still reach the reconciler.
func hasEvalHubPodIdentityHint(object client.Object) bool {
	if object == nil {
		return false
	}
	labels := object.GetLabels()
	if labels[utils.NetworkPolicyComponentLabel] == "evalhub" {
		return true
	}
	if labels[utils.NetworkPolicyOwnerUIDLabel] != "" {
		return true
	}
	return labels["app"] == "eval-hub" && labels["instance"] != ""
}

// evalHubPodLabelsChanged fires when the labels of a Pod we might own change in
// either direction, so removal is as interesting as mutation.
func evalHubPodLabelsChanged(oldObject, newObject client.Object) bool {
	return (hasEvalHubPodIdentityHint(oldObject) || hasEvalHubPodIdentityHint(newObject)) &&
		!reflect.DeepEqual(podLabels(oldObject), podLabels(newObject))
}

func podLabels(object client.Object) map[string]string {
	if object == nil {
		return nil
	}
	return object.GetLabels()
}
