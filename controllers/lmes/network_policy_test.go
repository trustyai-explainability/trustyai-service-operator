package lmes

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func policyTestJob() *lmesv1alpha1.LMEvalJob {
	return &lmesv1alpha1.LMEvalJob{
		TypeMeta:   metav1.TypeMeta{APIVersion: lmesv1alpha1.GroupVersion.String(), Kind: lmesv1alpha1.KindName},
		ObjectMeta: metav1.ObjectMeta{Name: "evaluation", Namespace: "tenant", UID: "job-uid", Generation: 2},
		Spec:       lmesv1alpha1.LMEvalJobSpec{Model: "local-completions", TaskList: lmesv1alpha1.TaskList{TaskNames: []string{"arc_easy"}}},
	}
}

func policyTestClient(t *testing.T, objects ...client.Object) (*LMEvalJobReconciler, *policyCountingClient) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, networkingv1.AddToScheme(scheme))
	require.NoError(t, lmesv1alpha1.AddToScheme(scheme))
	c := &policyCountingClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&lmesv1alpha1.LMEvalJob{}).WithObjects(objects...).Build()}
	r := &LMEvalJobReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(100), pullingJobs: newSyncedMap4Reconciler()}
	for _, obj := range objects {
		require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(obj), obj))
	}
	return r, c
}

type policyCountingClient struct {
	client.Client
	writes         []string
	policyErr      error
	patchErr       error
	legacyReplaced bool
	statusErr      error
}

type policyStatusClient struct {
	client.SubResourceClient
	parent *policyCountingClient
}

func (c *policyCountingClient) Status() client.SubResourceWriter { return c.SubResource("status") }

func (c *policyCountingClient) SubResource(name string) client.SubResourceClient {
	return &policyStatusClient{SubResourceClient: c.Client.SubResource(name), parent: c}
}

func (c *policyStatusClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	if c.parent.statusErr != nil {
		return c.parent.statusErr
	}
	return c.SubResourceClient.Patch(ctx, obj, patch, opts...)
}

func (c *policyCountingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if _, ok := obj.(*networkingv1.NetworkPolicy); ok {
		c.writes = append(c.writes, "policy-create")
		if c.policyErr != nil {
			return c.policyErr
		}
	}
	if _, ok := obj.(*corev1.Pod); ok {
		c.writes = append(c.writes, "pod-create")
	}
	return c.Client.Create(ctx, obj, opts...)
}
func (c *policyCountingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if _, ok := obj.(*networkingv1.NetworkPolicy); ok {
		c.writes = append(c.writes, "policy-update")
		if c.policyErr != nil {
			return c.policyErr
		}
	}
	return c.Client.Update(ctx, obj, opts...)
}
func (c *policyCountingClient) Patch(ctx context.Context, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
	if _, ok := obj.(*corev1.Pod); ok {
		c.writes = append(c.writes, "pod-patch")
		if c.patchErr != nil {
			return c.patchErr
		}
	}
	return c.Client.Patch(ctx, obj, p, opts...)
}
func (c *policyCountingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if _, ok := obj.(*networkingv1.NetworkPolicy); ok {
		c.writes = append(c.writes, "legacy-delete")
		options := &client.DeleteOptions{}
		for _, opt := range opts {
			opt.ApplyToDelete(options)
		}
		if options.Preconditions == nil || options.Preconditions.UID == nil || options.Preconditions.ResourceVersion == nil {
			return errors.New("missing delete preconditions")
		}
		if c.legacyReplaced {
			return apierrors.NewConflict(networkingv1.Resource("networkpolicies"), obj.GetName(), errors.New("replacement UID"))
		}
	}
	return c.Client.Delete(ctx, obj, opts...)
}

func ownedPolicyTestPod(job *lmesv1alpha1.LMEvalJob) *corev1.Pod {
	controller := true
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: job.Name, Namespace: job.Namespace, UID: "pod-uid",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: lmesv1alpha1.GroupVersion.String(), Kind: lmesv1alpha1.KindName, Name: job.Name, UID: job.UID, Controller: &controller}},
		Labels:          map[string]string{"custom": "preserve", LMEvalJobUIDLabel: "spoofed", utils.NetworkPolicyComponentLabel: "spoofed"},
	}}
}

func TestLMEvalNetworkPolicyBuilder(t *testing.T) {
	for _, online := range []bool{true, false} {
		job := policyTestJob()
		job.Spec.AllowOnline = &online
		desired, err := buildLMEvalJobNetworkPolicy(job)
		require.NoError(t, err)
		require.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, desired.Policy.Spec.PolicyTypes)
		require.Empty(t, desired.Policy.Spec.Ingress)
		require.Equal(t, []networkingv1.NetworkPolicyEgressRule{{}}, desired.Policy.Spec.Egress)
		require.Equal(t, utils.NetworkPolicyAllowAll, desired.Egress.Mode)
		require.NotEmpty(t, desired.Egress.Rationale)
		require.NotEmpty(t, desired.Egress.ResidualRisk)
		require.Equal(t, "lmes", desired.Policy.Spec.PodSelector.MatchLabels[utils.NetworkPolicyComponentLabel])
		require.Equal(t, "evaluation", desired.Policy.Spec.PodSelector.MatchLabels[utils.NetworkPolicyRoleLabel])
		require.Equal(t, string(job.UID), desired.Policy.Spec.PodSelector.MatchLabels[LMEvalJobUIDLabel])
		require.Empty(t, desired.Policy.OwnerReferences)
		other := job.DeepCopy()
		other.UID = "replacement-uid"
		replacement, err := buildLMEvalJobNetworkPolicy(other)
		require.NoError(t, err)
		require.NotEqual(t, desired.Policy.Name, replacement.Policy.Name)
		require.NotEqual(t, desired.Policy.Spec.PodSelector, replacement.Policy.Spec.PodSelector)
	}
	for _, job := range []*lmesv1alpha1.LMEvalJob{nil, {}, {ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "n", UID: types.UID(strings.Repeat("x", 64))}}} {
		_, err := buildLMEvalJobNetworkPolicy(job)
		require.Error(t, err)
	}
	// Exact old naming regression from the observed #967 policy.
	job := policyTestJob()
	job.UID = "c5d12c81-b5d3-4b02-b5e0-9a0f4508a89a"
	require.Equal(t, "lmevaljob-c5d12c81-b5d3-4b02-b5e0-9a0f4508a-np-3b447793f293d19d", legacyLMEvalPolicyName(job))
}

func TestLMEvalNetworkPolicyReconcileAndRepair(t *testing.T) {
	ctx := context.Background()
	job := policyTestJob()
	pod := ownedPolicyTestPod(job)
	job.Status = lmesv1alpha1.LMEvalJobStatus{State: lmesv1alpha1.CompleteJobState, Reason: lmesv1alpha1.SucceedReason, Results: "retain-result", Message: "retain-message"}
	r, c := policyTestClient(t, job, pod)
	require.NoError(t, r.reconcileLMEvalJobNetworkPolicy(ctx, job))
	require.Equal(t, []string{"policy-create", "pod-patch"}, c.writes)
	storedPod := &corev1.Pod{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), storedPod))
	labels, err := lmevalNetworkPolicyLabels(job)
	require.NoError(t, err)
	for key, value := range labels {
		require.Equal(t, value, storedPod.Labels[key])
	}
	require.Equal(t, "preserve", storedPod.Labels["custom"])
	storedJob := &lmesv1alpha1.LMEvalJob{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(job), storedJob))
	require.Equal(t, "retain-result", storedJob.Status.Results)
	require.Equal(t, lmesv1alpha1.CompleteJobState, storedJob.Status.State)
	require.Equal(t, "retain-message", storedJob.Status.Message)
	require.True(t, apimeta.IsStatusConditionTrue(storedJob.Status.Conditions, NetworkPolicyReadyCondition))
	transition := apimeta.FindStatusCondition(storedJob.Status.Conditions, NetworkPolicyReadyCondition).LastTransitionTime
	c.writes = nil
	require.NoError(t, r.reconcileLMEvalJobNetworkPolicy(ctx, storedJob))
	require.Empty(t, c.writes)
	require.Equal(t, transition, apimeta.FindStatusCondition(storedJob.Status.Conditions, NetworkPolicyReadyCondition).LastTransitionTime)
	desired, err := buildLMEvalJobNetworkPolicy(job)
	require.NoError(t, err)
	storedPolicy := &networkingv1.NetworkPolicy{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), storedPolicy))
	storedPolicy.Annotations["custom"] = "preserve"
	storedPolicy.Spec.Egress = nil
	require.NoError(t, c.Client.Update(ctx, storedPolicy))
	require.NoError(t, r.reconcileLMEvalJobNetworkPolicy(ctx, storedJob))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), storedPolicy))
	require.Len(t, storedPolicy.Spec.Egress, 1)
	require.Equal(t, "preserve", storedPolicy.Annotations["custom"])
	require.True(t, controlledByLMEvalJob(storedPolicy, job))
	require.NoError(t, c.Client.Delete(ctx, storedPolicy))
	require.NoError(t, r.reconcileLMEvalJobNetworkPolicy(ctx, storedJob))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(desired.Policy), storedPolicy))
}

func TestLMEvalNetworkPolicyFailureAndRecovery(t *testing.T) {
	for _, failure := range []string{"create", "repair", "foreign-pod", "ownerless-policy", "stale-owner"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			job := policyTestJob()
			pod := ownedPolicyTestPod(job)
			r, c := policyTestClient(t, job, pod)
			switch failure {
			case "create":
				c.policyErr = errors.New("policy API unavailable")
			case "repair":
				c.patchErr = errors.New("pod API unavailable")
			case "foreign-pod":
				pod.OwnerReferences[0].UID = "foreign"
				require.NoError(t, c.Client.Update(ctx, pod))
			case "ownerless-policy":
				p, err := buildLMEvalJobNetworkPolicy(job)
				require.NoError(t, err)
				require.NoError(t, c.Client.Create(ctx, p.Policy))
			case "stale-owner":
				replacement := job.DeepCopy()
				replacement.UID = "new-owner"
				require.NoError(t, c.Client.Update(ctx, replacement))
			}
			err := r.reconcileLMEvalJobNetworkPolicy(ctx, job)
			require.Error(t, err)
			require.NotContains(t, c.writes, "pod-create")
			stored := &lmesv1alpha1.LMEvalJob{}
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(job), stored))
			if failure == "stale-owner" {
				// Optimistic status locking must not publish the old owner's health
				// on a replacement CR. The reconcile/status error is still returned.
				require.Empty(t, stored.Status.Conditions)
				return
			}
			require.True(t, apimeta.IsStatusConditionFalse(stored.Status.Conditions, NetworkPolicyReadyCondition))
			require.Empty(t, stored.Status.State)
			require.Empty(t, stored.Status.Results)
			if failure == "create" || failure == "repair" {
				c.policyErr = nil
				c.patchErr = nil
				require.NoError(t, r.reconcileLMEvalJobNetworkPolicy(ctx, stored))
				require.True(t, apimeta.IsStatusConditionTrue(stored.Status.Conditions, NetworkPolicyReadyCondition))
			}
		})
	}
}

func TestLMEvalNetworkPolicyStatusErrorsBlockLaunch(t *testing.T) {
	job := policyTestJob()
	r, c := policyTestClient(t, job)
	policyErr, statusErr := errors.New("policy failure"), errors.New("status failure")
	c.policyErr, c.statusErr = policyErr, statusErr
	err := r.reconcileLMEvalJobNetworkPolicy(context.Background(), job)
	require.ErrorIs(t, err, policyErr)
	require.ErrorIs(t, err, statusErr)
	c.policyErr = nil
	// Refetch: an unsuccessful status write must not become an in-memory
	// success that bypasses the gate on the next normal reconciliation.
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(job), job))
	pod, err := r.prepareExecutionPod(context.Background(), job, NewDefaultPermissionConfig(), nil, "", logr.Discard())
	require.ErrorIs(t, err, statusErr)
	require.Nil(t, pod)
	require.NotContains(t, c.writes, "pod-create")
}

func TestLMEvalNetworkPolicyLaunchGate(t *testing.T) {
	ctx := context.Background()
	job := policyTestJob()
	r, c := policyTestClient(t, job)
	c.policyErr = errors.New("policy API unavailable")
	pod, err := r.prepareExecutionPod(ctx, job, NewDefaultPermissionConfig(), nil, "", logr.Discard())
	require.Error(t, err)
	require.Nil(t, pod)
	require.Empty(t, job.Status.State)
	// Reconcile must also fail before permissions/PVC/Pod handling and retry.
	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)})
	require.Error(t, err)
	require.NotContains(t, c.writes, "pod-create")
	c.policyErr = nil
	pod, err = r.prepareExecutionPod(ctx, job, NewDefaultPermissionConfig(), nil, "", logr.Discard())
	require.NoError(t, err)
	labels, err := lmevalNetworkPolicyLabels(job)
	require.NoError(t, err)
	for key, value := range labels {
		require.Equal(t, value, pod.Labels[key])
	}
	require.True(t, controlledByLMEvalJob(pod, job))
}

func legacyPolicyTestFixture(t *testing.T, job *lmesv1alpha1.LMEvalJob) *networkingv1.NetworkPolicy {
	t.Helper()
	pod := ownedPolicyTestPod(job)
	return &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: legacyLMEvalPolicyName(job), Namespace: job.Namespace, UID: "legacy-uid", OwnerReferences: pod.OwnerReferences,
		Labels: map[string]string{"app.kubernetes.io/managed-by": "trustyai-service-operator", "trustyai.opendatahub.io/network-policy-owner-uid": string(job.UID)}},
		Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{LMEvalJobUIDLabel: string(job.UID)}}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}}}
}

func TestLMEvalNetworkPolicyMigration(t *testing.T) {
	for _, variant := range []string{"owned", "foreign", "ownerless", "unexpected-spec", "replacement-race", "repair-fails"} {
		t.Run(variant, func(t *testing.T) {
			ctx := context.Background()
			job := policyTestJob()
			pod := ownedPolicyTestPod(job)
			legacy := legacyPolicyTestFixture(t, job)
			switch variant {
			case "foreign":
				legacy.OwnerReferences[0].UID = "foreign"
			case "ownerless":
				legacy.OwnerReferences = nil
			case "unexpected-spec":
				legacy.Spec.PodSelector.MatchLabels["extra"] = "unexpected"
			}
			r, c := policyTestClient(t, job, pod, legacy)
			c.legacyReplaced = variant == "replacement-race"
			if variant == "repair-fails" {
				c.patchErr = errors.New("repair failed")
			}
			err := r.reconcileLMEvalJobNetworkPolicy(ctx, job)
			if variant == "owned" {
				require.NoError(t, err)
				require.Equal(t, []string{"policy-create", "pod-patch", "legacy-delete"}, c.writes)
				require.True(t, apierrors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(legacy), &networkingv1.NetworkPolicy{})))
			} else {
				require.Error(t, err)
				require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(legacy), &networkingv1.NetworkPolicy{}))
				require.True(t, apimeta.IsStatusConditionFalse(job.Status.Conditions, NetworkPolicyReadyCondition))
				if variant != "replacement-race" {
					require.NotContains(t, c.writes, "legacy-delete")
				}
			}
		})
	}
}

func TestLMEvalNetworkPolicyWatchesAndOwnership(t *testing.T) {
	job := policyTestJob()
	pod := ownedPolicyTestPod(job)
	require.NoError(t, setLMEvalJobPodIdentityLabels(pod, job))
	pred := lmevalPodIdentityPredicate()
	require.False(t, pred.Create(event.CreateEvent{Object: pod}))
	require.True(t, pred.Delete(event.DeleteEvent{Object: pod}))
	require.False(t, pred.Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: pod.DeepCopy()}))
	for _, key := range []string{LMEvalJobUIDLabel, utils.NetworkPolicyComponentLabel, utils.NetworkPolicyRoleLabel, utils.NetworkPolicyOwnerUIDLabel} {
		next := pod.DeepCopy()
		delete(next.Labels, key)
		require.True(t, pred.Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: next}))
	}
	next := pod.DeepCopy()
	next.Status.Phase = corev1.PodRunning
	require.False(t, pred.Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: next}))
	desired, err := buildLMEvalJobNetworkPolicy(job)
	require.NoError(t, err)
	policy := desired.Policy
	nextPolicy := policy.DeepCopy()
	nextPolicy.Spec.Egress = nil
	policyPred := lmevalPolicyPredicate()
	require.True(t, policyPred.Delete(event.DeleteEvent{Object: policy}))
	require.True(t, policyPred.Update(event.UpdateEvent{ObjectOld: policy, ObjectNew: nextPolicy}))
	require.False(t, policyPred.Update(event.UpdateEvent{ObjectOld: policy, ObjectNew: policy.DeepCopy()}))
	for _, field := range []string{"uid", "kind", "group", "name", "namespace", "controller"} {
		bad := pod.DeepCopy()
		switch field {
		case "uid":
			bad.OwnerReferences[0].UID = "other"
		case "kind":
			bad.OwnerReferences[0].Kind = "Other"
		case "group":
			bad.OwnerReferences[0].APIVersion = "other.example/v1"
		case "name":
			bad.OwnerReferences[0].Name = "other"
		case "namespace":
			bad.Namespace = "other"
		case "controller":
			bad.OwnerReferences[0].Controller = nil
		}
		require.Error(t, setLMEvalJobPodIdentityLabels(bad, job), field)
	}
	older := pod.DeepCopy()
	older.OwnerReferences[0].APIVersion = lmesv1alpha1.GroupVersion.Group + "/v0"
	require.NoError(t, setLMEvalJobPodIdentityLabels(older, job))
}

func TestLMEvalNetworkPolicyDrainAndPodDeletion(t *testing.T) {
	for _, state := range []lmesv1alpha1.JobState{lmesv1alpha1.ScheduledJobState, lmesv1alpha1.RunningJobState, lmesv1alpha1.CompleteJobState, lmesv1alpha1.CancelledJobState} {
		job := policyTestJob()
		job.Status.State = state
		require.True(t, mayContinueWithoutNetworkPolicy(job))
	}
	job := policyTestJob()
	require.False(t, mayContinueWithoutNetworkPolicy(job))
	job.Status.State = lmesv1alpha1.SuspendedJobState
	require.False(t, mayContinueWithoutNetworkPolicy(job))
	job.Spec.Suspend = true
	require.True(t, mayContinueWithoutNetworkPolicy(job))
	job.Spec.Suspend = false
	job.Status.State = lmesv1alpha1.CompleteJobState
	job.Annotations = map[string]string{LastScheduledGenerationAnnotation: "1"}
	require.False(t, mayContinueWithoutNetworkPolicy(job))
	// A foreign replacement must not be exec'd or deleted by display name.
	job = policyTestJob()
	job.Status.PodName = job.Name
	pod := ownedPolicyTestPod(job)
	pod.OwnerReferences[0].UID = "other"
	r, c := policyTestClient(t, job, pod)
	ctx := context.Background()
	require.Error(t, r.deleteJobPod(ctx, job))
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))
	_, _, err := r.remoteCommand(ctx, job, "unused")
	require.Error(t, err)
}
