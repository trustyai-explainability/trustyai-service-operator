package lmes

import (
	"context"
	"fmt"

	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func buildLMEvalJobNetworkPolicy(job *lmesv1alpha1.LMEvalJob, scheme *runtime.Scheme) (*networkingv1.NetworkPolicy, error) {
	if job == nil {
		return nil, fmt.Errorf("LMEvalJob must not be nil")
	}
	if job.UID == "" {
		return nil, fmt.Errorf("LMEvalJob %s/%s has no UID", job.Namespace, job.Name)
	}
	if job.Namespace == "" {
		return nil, fmt.Errorf("LMEvalJob %s has no namespace", job.Name)
	}

	policyName, err := utils.NetworkPolicyName("lmevaljob-" + string(job.UID))
	if err != nil {
		return nil, fmt.Errorf("generate NetworkPolicy name for LMEvalJob %s/%s: %w", job.Namespace, job.Name, err)
	}
	ownerLabels, err := utils.NetworkPolicyOwnerLabels(job)
	if err != nil {
		return nil, fmt.Errorf("build NetworkPolicy owner labels for LMEvalJob %s/%s: %w", job.Namespace, job.Name, err)
	}

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: job.Namespace,
			Labels:    ownerLabels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
				LMEvalJobUIDLabel: string(job.UID),
			}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			// No ingress rules intentionally denies all pod-network ingress to this job.
		},
	}
	if err := utils.SetNetworkPolicyOwnerReference(policy, job, scheme); err != nil {
		return nil, fmt.Errorf("set NetworkPolicy owner for LMEvalJob %s/%s: %w", job.Namespace, job.Name, err)
	}
	if err := utils.ValidateNetworkPolicy(policy); err != nil {
		return nil, fmt.Errorf("validate NetworkPolicy for LMEvalJob %s/%s: %w", job.Namespace, job.Name, err)
	}
	return policy, nil
}

func reconcileLMEvalJobNetworkPolicy(ctx context.Context, c client.Client, job *lmesv1alpha1.LMEvalJob, scheme *runtime.Scheme) error {
	desired, err := buildLMEvalJobNetworkPolicy(job, scheme)
	if err != nil {
		return err
	}
	if err := utils.ReconcileNetworkPolicy(ctx, c, desired); err != nil {
		return fmt.Errorf("reconcile NetworkPolicy for LMEvalJob %s/%s: %w", job.Namespace, job.Name, err)
	}
	return nil
}

// setLMEvalJobPodIdentityLabel overwrites user-controlled metadata with the
// immutable identity of the persisted LMEvalJob that owns this pod.
func setLMEvalJobPodIdentityLabel(pod *corev1.Pod, job *lmesv1alpha1.LMEvalJob) error {
	if pod == nil {
		return fmt.Errorf("LMEvalJob pod must not be nil")
	}
	if job == nil || job.UID == "" {
		return fmt.Errorf("LMEvalJob must have a UID before its pod can be labeled")
	}
	if pod.Namespace != job.Namespace {
		return fmt.Errorf("LMEvalJob pod namespace %q does not match job namespace %q", pod.Namespace, job.Namespace)
	}
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[LMEvalJobUIDLabel] = string(job.UID)
	return nil
}

// reconcileExistingLMEvalJobPodLabel repairs old or drifted managed pods. The
// owner UID check prevents modifying an unrelated pod that happens to share the
// conventional job pod name.
func (r *LMEvalJobReconciler) reconcileExistingLMEvalJobPodLabel(ctx context.Context, job *lmesv1alpha1.LMEvalJob) error {
	if job == nil || job.UID == "" {
		return fmt.Errorf("LMEvalJob must have a UID before its pod can be reconciled")
	}
	podName := job.GetPodName()
	if job.Status.PodName != "" {
		podName = job.Status.PodName
	}
	pod := &corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: job.Namespace, Name: podName}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get pod %s/%s for LMEvalJob identity repair: %w", job.Namespace, podName, err)
	}
	if !metav1.IsControlledBy(pod, job) {
		return fmt.Errorf("pod %s/%s is not controlled by LMEvalJob UID %s", pod.Namespace, pod.Name, job.UID)
	}
	if pod.Labels[LMEvalJobUIDLabel] == string(job.UID) {
		return nil
	}

	before := pod.DeepCopy()
	if err := setLMEvalJobPodIdentityLabel(pod, job); err != nil {
		return err
	}
	if err := r.Patch(ctx, pod, client.MergeFrom(before)); err != nil {
		return fmt.Errorf("repair LMEvalJob identity label on pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	return nil
}
