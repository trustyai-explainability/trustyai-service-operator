package lmes

import (
	"context"
	"errors"
	"testing"

	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newLMEvalPolicyTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		networkingv1.AddToScheme,
		lmesv1alpha1.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatalf("register test API types: %v", err)
		}
	}
	return scheme
}

func newLMEvalPolicyTestJob(name, uid string) *lmesv1alpha1.LMEvalJob {
	return &lmesv1alpha1.LMEvalJob{
		TypeMeta: metav1.TypeMeta{APIVersion: lmesv1alpha1.Version, Kind: lmesv1alpha1.KindName},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "evals",
			UID:       types.UID(uid),
		},
	}
}

func TestBuildLMEvalJobNetworkPolicyIsPerUIDIngressDeny(t *testing.T) {
	scheme := newLMEvalPolicyTestScheme(t)
	first := newLMEvalPolicyTestJob("job", "uid-one")
	second := newLMEvalPolicyTestJob("job", "uid-two")

	firstPolicy, err := buildLMEvalJobNetworkPolicy(first, scheme)
	if err != nil {
		t.Fatalf("build first policy: %v", err)
	}
	secondPolicy, err := buildLMEvalJobNetworkPolicy(second, scheme)
	if err != nil {
		t.Fatalf("build second policy: %v", err)
	}

	if firstPolicy.Name == secondPolicy.Name {
		t.Fatalf("different job UIDs produced the same policy name %q", firstPolicy.Name)
	}
	if got := firstPolicy.Spec.PodSelector.MatchLabels[LMEvalJobUIDLabel]; got != string(first.UID) {
		t.Errorf("first policy selector UID = %q, want %q", got, first.UID)
	}
	if got := secondPolicy.Spec.PodSelector.MatchLabels[LMEvalJobUIDLabel]; got != string(second.UID) {
		t.Errorf("second policy selector UID = %q, want %q", got, second.UID)
	}
	if len(firstPolicy.Spec.PodSelector.MatchLabels) != 1 {
		t.Errorf("policy selector labels = %#v, want only the per-job UID", firstPolicy.Spec.PodSelector.MatchLabels)
	}
	if len(firstPolicy.Spec.PolicyTypes) != 1 || firstPolicy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Errorf("policy types = %v, want [Ingress]", firstPolicy.Spec.PolicyTypes)
	}
	if len(firstPolicy.Spec.Ingress) != 0 {
		t.Errorf("ingress rules = %#v, want none (deny all ingress)", firstPolicy.Spec.Ingress)
	}
	if len(firstPolicy.Spec.Egress) != 0 {
		t.Errorf("egress rules = %#v, want none", firstPolicy.Spec.Egress)
	}
	if len(firstPolicy.OwnerReferences) != 1 || firstPolicy.OwnerReferences[0].UID != first.UID || firstPolicy.OwnerReferences[0].Controller == nil || !*firstPolicy.OwnerReferences[0].Controller {
		t.Errorf("policy owner references = %#v, want controller reference to the job UID", firstPolicy.OwnerReferences)
	}
}

func TestBuildLMEvalJobNetworkPolicyRequiresPersistedUID(t *testing.T) {
	job := newLMEvalPolicyTestJob("job", "")
	if _, err := buildLMEvalJobNetworkPolicy(job, newLMEvalPolicyTestScheme(t)); err == nil {
		t.Fatal("buildLMEvalJobNetworkPolicy() accepted a job without a UID")
	}
}

func TestSetLMEvalJobPodIdentityLabelOverridesUserValue(t *testing.T) {
	job := newLMEvalPolicyTestJob("job", "uid-authoritative")
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      job.Name,
		Namespace: job.Namespace,
		Labels: map[string]string{
			LMEvalJobUIDLabel: "user-spoofed-uid",
		},
	}}

	if err := setLMEvalJobPodIdentityLabel(pod, job); err != nil {
		t.Fatalf("set controller-owned pod label: %v", err)
	}
	if got := pod.Labels[LMEvalJobUIDLabel]; got != string(job.UID) {
		t.Errorf("pod UID label = %q, want controller-owned UID %q", got, job.UID)
	}
}

func TestReconcileExistingLMEvalJobPodLabelRepairsOnlyOwnedPod(t *testing.T) {
	scheme := newLMEvalPolicyTestScheme(t)
	job := newLMEvalPolicyTestJob("job", "uid-current")
	controller := true
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      job.Name,
		Namespace: job.Namespace,
		Labels: map[string]string{
			LMEvalJobUIDLabel: "stale-user-value",
		},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: lmesv1alpha1.Version,
			Kind:       lmesv1alpha1.KindName,
			Name:       job.Name,
			UID:        job.UID,
			Controller: &controller,
		}},
	}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	r := &LMEvalJobReconciler{Client: c}

	if err := r.reconcileExistingLMEvalJobPodLabel(context.Background(), job); err != nil {
		t.Fatalf("repair existing pod label: %v", err)
	}
	actual := &corev1.Pod{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(pod), actual); err != nil {
		t.Fatalf("get patched pod: %v", err)
	}
	if got := actual.Labels[LMEvalJobUIDLabel]; got != string(job.UID) {
		t.Errorf("repaired pod label = %q, want %q", got, job.UID)
	}

	unownedJob := newLMEvalPolicyTestJob("unowned", "uid-other")
	unownedJob.Name = job.Name
	unownedPod := actual.DeepCopy()
	unownedPod.OwnerReferences[0].UID = unownedJob.UID
	unownedPod.Labels[LMEvalJobUIDLabel] = "unrelated"
	if err := c.Update(context.Background(), unownedPod); err != nil {
		t.Fatalf("prepare pod with different owner: %v", err)
	}
	if err := r.reconcileExistingLMEvalJobPodLabel(context.Background(), job); err == nil {
		t.Fatal("reconciler accepted a pod controlled by another LMEvalJob UID")
	}
}

func TestReconcileLMEvalJobNetworkPolicyRepairsDriftAndRecreatesDeletion(t *testing.T) {
	scheme := newLMEvalPolicyTestScheme(t)
	job := newLMEvalPolicyTestJob("job", "uid-policy")
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	if err := reconcileLMEvalJobNetworkPolicy(context.Background(), c, job, scheme); err != nil {
		t.Fatalf("create NetworkPolicy: %v", err)
	}
	desired, err := buildLMEvalJobNetworkPolicy(job, scheme)
	if err != nil {
		t.Fatalf("build desired NetworkPolicy: %v", err)
	}

	actual := &networkingv1.NetworkPolicy{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(desired), actual); err != nil {
		t.Fatalf("get created NetworkPolicy: %v", err)
	}
	actual.Spec.PodSelector.MatchLabels[LMEvalJobUIDLabel] = "drifted"
	if err := c.Update(context.Background(), actual); err != nil {
		t.Fatalf("introduce policy drift: %v", err)
	}
	if err := reconcileLMEvalJobNetworkPolicy(context.Background(), c, job, scheme); err != nil {
		t.Fatalf("repair policy drift: %v", err)
	}
	actual = &networkingv1.NetworkPolicy{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(desired), actual); err != nil {
		t.Fatalf("get repaired policy: %v", err)
	}
	if got := actual.Spec.PodSelector.MatchLabels[LMEvalJobUIDLabel]; got != string(job.UID) {
		t.Errorf("repaired selector UID = %q, want %q", got, job.UID)
	}

	if err := c.Delete(context.Background(), actual); err != nil {
		t.Fatalf("delete NetworkPolicy: %v", err)
	}
	if err := reconcileLMEvalJobNetworkPolicy(context.Background(), c, job, scheme); err != nil {
		t.Fatalf("recreate deleted NetworkPolicy: %v", err)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(desired), &networkingv1.NetworkPolicy{}); err != nil {
		t.Fatalf("NetworkPolicy was not recreated: %v", err)
	}
}

type failingNetworkPolicyClient struct {
	client.Client
	createErr error
	updateErr error
}

func (c *failingNetworkPolicyClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if _, ok := obj.(*networkingv1.NetworkPolicy); ok && c.createErr != nil {
		return c.createErr
	}
	return c.Client.Create(ctx, obj, opts...)
}

func (c *failingNetworkPolicyClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if _, ok := obj.(*networkingv1.NetworkPolicy); ok && c.updateErr != nil {
		return c.updateErr
	}
	return c.Client.Update(ctx, obj, opts...)
}

func TestLMEvalJobPolicyFailurePreventsPodCreation(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			scheme := newLMEvalPolicyTestScheme(t)
			job := newLMEvalPolicyTestJob("job", "uid-fail-closed")
			objects := []client.Object{job}
			if operation == "update" {
				policy, err := buildLMEvalJobNetworkPolicy(job, scheme)
				if err != nil {
					t.Fatalf("build initial NetworkPolicy: %v", err)
				}
				policy.Spec.PodSelector.MatchLabels[LMEvalJobUIDLabel] = "drifted"
				objects = append(objects, policy)
			}
			baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			failureClient := &failingNetworkPolicyClient{Client: baseClient}
			if operation == "create" {
				failureClient.createErr = errors.New("simulated NetworkPolicy create failure")
			} else {
				failureClient.updateErr = errors.New("simulated NetworkPolicy update failure")
			}
			r := &LMEvalJobReconciler{Client: failureClient, Scheme: scheme}

			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(job)}); err == nil {
				t.Fatalf("Reconcile() ignored NetworkPolicy %s failure", operation)
			}
			pods := &corev1.PodList{}
			if err := baseClient.List(context.Background(), pods); err != nil {
				t.Fatalf("list pods after failed reconciliation: %v", err)
			}
			if len(pods.Items) != 0 {
				t.Errorf("created %d pods despite NetworkPolicy %s failure", len(pods.Items), operation)
			}
		})
	}
}
