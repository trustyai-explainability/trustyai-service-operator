package lmes

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// LMEvalJobUIDLabel is retained for ingress-only policy upgrade compatibility.
	LMEvalJobUIDLabel           = "trustyai.opendatahub.io/lmevaljob-uid"
	NetworkPolicyReadyCondition = "NetworkPolicyReady"
)

func lmevalNetworkPolicyIdentity(job *lmesv1alpha1.LMEvalJob) (utils.NetworkPolicyIdentity, error) {
	if job == nil || job.Namespace == "" || job.Name == "" || job.UID == "" {
		return utils.NetworkPolicyIdentity{}, fmt.Errorf("persisted namespaced LMEvalJob identity is required")
	}
	return utils.NetworkPolicyIdentity{
		OwnerKind: schema.GroupKind{Group: lmesv1alpha1.GroupVersion.Group, Kind: lmesv1alpha1.KindName},
		OwnerUID:  job.UID, Component: "lmes", Role: "evaluation",
	}, nil
}

func lmevalNetworkPolicyLabels(job *lmesv1alpha1.LMEvalJob) (map[string]string, error) {
	identity, err := lmevalNetworkPolicyIdentity(job)
	if err != nil {
		return nil, err
	}
	labels, err := identity.Labels()
	if err != nil {
		return nil, err
	}
	// Kubernetes-generated CR UIDs fit the legacy label. Refuse malformed input
	// instead of changing the old selector's UID semantics during migration.
	if len(validation.IsValidLabelValue(string(job.UID))) > 0 {
		return nil, fmt.Errorf("invalid legacy LMEvalJob UID label")
	}
	labels[LMEvalJobUIDLabel] = string(job.UID)
	return labels, nil
}

func buildLMEvalJobNetworkPolicy(job *lmesv1alpha1.LMEvalJob) (utils.WorkloadNetworkPolicy, error) {
	identity, err := lmevalNetworkPolicyIdentity(job)
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	labels, err := lmevalNetworkPolicyLabels(job)
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	name, err := identity.Name("execution")
	if err != nil {
		return utils.WorkloadNetworkPolicy{}, err
	}
	desired := utils.WorkloadNetworkPolicy{
		Policy: &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: job.Namespace},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: labels},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
				// The driver listens on loopback and is managed using pods/exec. Reply
				// traffic for outbound connections needs no Pod-network ingress grant.
				Egress: []networkingv1.NetworkPolicyEgressRule{{}},
			},
		},
		Egress: &utils.NetworkPolicyEgressIntent{
			Mode:         utils.NetworkPolicyAllowAll,
			Rationale:    "LMEval supports arbitrary model endpoints and ports, inferred tokenizer/dataset/Git downloads, S3 preparation, OCI exports and companion-container dependencies. Application offline flags do not establish a complete network allowlist.",
			ResidualRisk: "Outbound traffic is unrestricted, including in application-offline mode. This provides no exfiltration protection and can broaden customer NetworkPolicies because ordinary grants are additive. All containers share the Pod network boundary.",
		},
	}
	return desired, utils.ValidateWorkloadNetworkPolicySet([]utils.WorkloadNetworkPolicy{desired}, identity)
}

// controlledByLMEvalJob compares immutable ownership, not display name or the
// owner's API version (which can change within the same group/kind).
func controlledByLMEvalJob(obj client.Object, job *lmesv1alpha1.LMEvalJob) bool {
	if obj == nil || job == nil || job.UID == "" || obj.GetNamespace() != job.Namespace {
		return false
	}
	ref := metav1.GetControllerOf(obj)
	if ref == nil {
		return false
	}
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	return err == nil && gv.Group == lmesv1alpha1.GroupVersion.Group && ref.Kind == lmesv1alpha1.KindName && ref.Name == job.Name && ref.UID == job.UID
}

func setLMEvalJobPodIdentityLabels(pod *corev1.Pod, job *lmesv1alpha1.LMEvalJob) error {
	if !controlledByLMEvalJob(pod, job) {
		return fmt.Errorf("execution Pod has a foreign controller identity")
	}
	labels, err := lmevalNetworkPolicyLabels(job)
	if err != nil {
		return err
	}
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	maps.Copy(pod.Labels, labels)
	return nil
}

// prepareExecutionPod gates both initial and resumed launches. Keep the API
// gate here as well as Reconcile so direct handler calls cannot prepare a Pod
// after a policy, identity, migration or readiness-write failure.
func (r *LMEvalJobReconciler) prepareExecutionPod(ctx context.Context, job *lmesv1alpha1.LMEvalJob, permissions *PermissionConfig, caBundle *corev1.ConfigMap, caKey string, logger logr.Logger) (*corev1.Pod, error) {
	if err := r.reconcileLMEvalJobNetworkPolicy(ctx, job); err != nil {
		return nil, err
	}
	// API clients may clear TypeMeta when applying a status patch. Normalize a
	// copy for Pod construction instead of relying on that incidental metadata.
	podJob := job.DeepCopy()
	podJob.SetGroupVersionKind(lmesv1alpha1.GroupVersion.WithKind(lmesv1alpha1.KindName))
	pod := CreatePod(Options, podJob, permissions, caBundle, caKey, logger)
	if err := setLMEvalJobPodIdentityLabels(pod, job); err != nil {
		return nil, errors.Join(err, r.reportNetworkPolicyStatus(ctx, job, false))
	}
	return pod, nil
}

func (r *LMEvalJobReconciler) reconcileExistingLMEvalJobPodLabels(ctx context.Context, job *lmesv1alpha1.LMEvalJob) error {
	pod := &corev1.Pod{}
	name := job.GetPodName()
	if job.Status.PodName != "" {
		name = job.Status.PodName
	}
	if err := r.Get(ctx, client.ObjectKey{Namespace: job.Namespace, Name: name}, pod); err != nil {
		return client.IgnoreNotFound(err)
	}
	before := pod.DeepCopy()
	if err := setLMEvalJobPodIdentityLabels(pod, job); err != nil {
		return err
	}
	if maps.Equal(before.Labels, pod.Labels) {
		return nil
	}
	// Include resourceVersion to avoid relabeling a replacement at the same name.
	return r.Patch(ctx, pod, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

func (r *LMEvalJobReconciler) reportNetworkPolicyStatus(ctx context.Context, job *lmesv1alpha1.LMEvalJob, ready bool) error {
	before := job.DeepCopy()
	condition := metav1.Condition{Type: NetworkPolicyReadyCondition, ObservedGeneration: job.Generation,
		Status: metav1.ConditionTrue, Reason: "Reconciled", Message: "Execution policy and owned Pod identity are reconciled; outbound traffic is unrestricted."}
	if !ready {
		// Do not publish raw API errors or endpoint/Secret-backed data in CR status.
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ReconciliationFailed"
		condition.Message = "Execution policy, owned Pod identity or legacy-policy migration could not be reconciled; retrying."
	}
	apimeta.SetStatusCondition(&job.Status.Conditions, condition)
	if reflect.DeepEqual(before.Status.Conditions, job.Status.Conditions) {
		return nil
	}
	return r.Status().Patch(ctx, job, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

func (r *LMEvalJobReconciler) reconcileLMEvalJobNetworkPolicy(ctx context.Context, job *lmesv1alpha1.LMEvalJob) error {
	identity, err := lmevalNetworkPolicyIdentity(job)
	if err != nil {
		return err
	}
	desired, err := buildLMEvalJobNetworkPolicy(job)
	if err != nil {
		return errors.Join(err, r.reportNetworkPolicyStatus(ctx, job, false))
	}
	// LMES watches LMEvalJobs and executes directly in the CR namespace; it does
	// not authorize a namespace override or derive authority from tenant labels.
	authority := utils.NetworkPolicyAuthority{Namespaces: map[string]bool{job.Namespace: true}}
	return utils.ReconcileWorkloadNetworkPolicySet(ctx, r.Client, r.Scheme, job, identity,
		[]utils.WorkloadNetworkPolicy{desired}, authority, func(ready bool, cause error) error {
			if ready {
				cause = r.reconcileExistingLMEvalJobPodLabels(ctx, job)
				if cause == nil {
					cause = r.removeLegacyLMEvalJobNetworkPolicy(ctx, job, authority)
				}
			}
			return errors.Join(cause, r.reportNetworkPolicyStatus(ctx, job, ready && cause == nil))
		})
}

// legacyLMEvalPolicyName reproduces only the old per-UID #967 name. It is not
// shared naming or a namespace sweep. Verified old protection stays in place
// until the replacement policy exists and the owned Pod has been relabeled.
func legacyLMEvalPolicyName(job *lmesv1alpha1.LMEvalJob) string {
	input := "lmevaljob-" + string(job.UID)
	var slug strings.Builder
	dash := false
	for _, ch := range strings.ToLower(input) {
		if ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' {
			slug.WriteRune(ch)
			dash = false
		} else if !dash && slug.Len() > 0 {
			slug.WriteByte('-')
			dash = true
		}
	}
	name := strings.Trim(slug.String(), "-")
	if len(name) > 43 {
		name = strings.TrimRight(name[:43], "-")
	}
	digest := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%s-np-%x", name, digest[:8])
}

func (r *LMEvalJobReconciler) removeLegacyLMEvalJobNetworkPolicy(ctx context.Context, job *lmesv1alpha1.LMEvalJob, authority utils.NetworkPolicyAuthority) error {
	legacy := &networkingv1.NetworkPolicy{}
	key := client.ObjectKey{Namespace: job.Namespace, Name: legacyLMEvalPolicyName(job)}
	if err := r.Get(ctx, key, legacy); err != nil {
		return client.IgnoreNotFound(err)
	}
	// The exact old controller reference, managed labels and selector establish
	// provenance. Never adopt or delete a customer/ownerless same-name object.
	if !controlledByLMEvalJob(legacy, job) || legacy.Labels["app.kubernetes.io/managed-by"] != "trustyai-service-operator" || legacy.Labels["trustyai.opendatahub.io/network-policy-owner-uid"] != string(job.UID) ||
		!reflect.DeepEqual(legacy.Spec.PodSelector, metav1.LabelSelector{MatchLabels: map[string]string{LMEvalJobUIDLabel: string(job.UID)}}) ||
		len(legacy.Spec.PolicyTypes) != 1 || legacy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress || len(legacy.Spec.Ingress) != 0 || len(legacy.Spec.Egress) != 0 {
		return fmt.Errorf("legacy policy has conflicting ownership or unexpected content; refusing cleanup")
	}
	if legacy.UID == "" {
		return fmt.Errorf("legacy policy UID is required for cleanup")
	}
	live := &lmesv1alpha1.LMEvalJob{}
	if !authority.Namespaces[job.Namespace] {
		return fmt.Errorf("legacy cleanup is outside authorized scope")
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(job), live); err != nil {
		return err
	}
	if live.UID != job.UID || !live.DeletionTimestamp.IsZero() {
		return fmt.Errorf("legacy policy owner was replaced or is deleting")
	}
	// Use the resourceVersion from the provenance check above. Re-reading via
	// the generic cleanup helper would discard that check's content precondition
	// if somebody changed this same-UID legacy policy between the two reads.
	return client.IgnoreNotFound(r.Delete(ctx, legacy, client.Preconditions{UID: &legacy.UID, ResourceVersion: &legacy.ResourceVersion}))
}

func lmevalPodIdentityPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return false
			}
			if !reflect.DeepEqual(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences()) {
				return true
			}
			for _, key := range []string{LMEvalJobUIDLabel, utils.NetworkPolicyComponentLabel, utils.NetworkPolicyRoleLabel, utils.NetworkPolicyOwnerUIDLabel} {
				if e.ObjectOld.GetLabels()[key] != e.ObjectNew.GetLabels()[key] {
					return true
				}
			}
			return false
		},
	}
}

func lmevalPolicyPredicate() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		old, oldOK := e.ObjectOld.(*networkingv1.NetworkPolicy)
		next, nextOK := e.ObjectNew.(*networkingv1.NetworkPolicy)
		return oldOK && nextOK && (!reflect.DeepEqual(old.Spec, next.Spec) || !maps.Equal(old.Labels, next.Labels) || !maps.Equal(old.Annotations, next.Annotations) || !reflect.DeepEqual(old.OwnerReferences, next.OwnerReferences) || !reflect.DeepEqual(old.DeletionTimestamp, next.DeletionTimestamp))
	}}
}

// Policy failure must stop launches, not required polling or termination. A
// completed job with a changed spec is a rerun request, not just cleanup.
func mayContinueWithoutNetworkPolicy(job *lmesv1alpha1.LMEvalJob) bool {
	if job.Spec.Suspend || job.Status.State == lmesv1alpha1.CancelledJobState || job.Status.State == lmesv1alpha1.RunningJobState || job.Status.State == lmesv1alpha1.ScheduledJobState {
		return true
	}
	return job.Status.State == lmesv1alpha1.CompleteJobState && !(getLastScheduledGeneration(job) > 0 && job.Generation > getLastScheduledGeneration(job))
}
