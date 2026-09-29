/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// sandboxScheme builds the minimal scheme for SandboxNamespace reconciler tests.
func sandboxScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	sc := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(sc))
	require.NoError(t, networkingv1.AddToScheme(sc))
	require.NoError(t, rbacv1.AddToScheme(sc))
	require.NoError(t, evalhubv1.AddToScheme(sc))
	return sc
}

// buildSandboxReconciler returns a reconciler backed by a fake client seeded with objs.
// The SandboxNamespace status subresource is registered so Status().Update works.
func buildSandboxReconciler(t *testing.T, sc *runtime.Scheme, objs ...client.Object) (*SandboxNamespaceReconciler, client.Client) {
	t.Helper()
	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithStatusSubresource(&evalhubv1.SandboxNamespace{}).
		WithObjects(objs...).
		Build()
	return &SandboxNamespaceReconciler{
		Client:        fc,
		Scheme:        sc,
		EventRecorder: record.NewFakeRecorder(10),
	}, fc
}

func sandboxRequest(cr *evalhubv1.SandboxNamespace) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}}
}

// reconcileSandboxToReady runs the two reconcile passes (finalizer add, then provision).
func reconcileSandboxToReady(t *testing.T, r *SandboxNamespaceReconciler, cr *evalhubv1.SandboxNamespace) {
	t.Helper()
	req := sandboxRequest(cr)

	res, err := r.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, res.Requeue, "first reconcile should add the finalizer and requeue")

	_, err = r.Reconcile(context.Background(), req)
	require.NoError(t, err)
}

func newSandboxCR(name, ns string) *evalhubv1.SandboxNamespace {
	return &evalhubv1.SandboxNamespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: evalhubv1.SandboxNamespaceSpec{
			JobID:                    name,
			EvalHubInstanceName:      "evalhub-1",
			EvalHubInstanceNamespace: ns,
			ResourceEnvelope:         &evalhubv1.SandboxResourceEnvelope{CPU: "2", Memory: "4Gi", MaxPods: 5},
			ModelEndpointURL:         "http://10.0.0.5:8080",
			EvalHubAPIEndpoint:       "http://10.0.0.6:9000",
		},
	}
}

// TestSandboxReconciler_ProvisionsNamespaceAndResources verifies the full provisioning
// path: a labelled namespace, a ResourceQuota matching the envelope, the NetworkPolicy
// set, and the broker ServiceAccount/Role/RoleBinding.
func TestSandboxReconciler_ProvisionsNamespaceAndResources(t *testing.T) {
	sc := sandboxScheme(t)
	cr := newSandboxCR("job-abc", "control-ns")
	r, fc := buildSandboxReconciler(t, sc, cr)

	reconcileSandboxToReady(t, r, cr)

	nsName := "evalhub-sandbox-job-abc"
	ctx := context.Background()

	// Namespace with parent-job labels.
	ns := &corev1.Namespace{}
	require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: nsName}, ns))
	assert.Equal(t, "job-abc", ns.Labels[evalHubJobIDLabel])
	assert.Equal(t, "evalhub-1", ns.Labels[evalHubInstanceNameLabel])
	assert.Equal(t, "control-ns", ns.Labels[evalHubInstanceNamespaceLabel])
	assert.Equal(t, "job-abc", ns.Labels[sandboxCRNameLabel])
	assert.Equal(t, sandboxComponentValue, ns.Labels[sandboxComponentLabel])

	// ResourceQuota matches the envelope (cpu/memory bound requests+limits, pods=MaxPods).
	rq := &corev1.ResourceQuota{}
	require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: sandboxResourceQuotaName, Namespace: nsName}, rq))
	assert.Equal(t, "2", rq.Spec.Hard.Name(corev1.ResourceRequestsCPU, "").String())
	assert.Equal(t, "2", rq.Spec.Hard.Name(corev1.ResourceLimitsCPU, "").String())
	assert.Equal(t, "4Gi", rq.Spec.Hard.Name(corev1.ResourceRequestsMemory, "").String())
	assert.Equal(t, "4Gi", rq.Spec.Hard.Name(corev1.ResourceLimitsMemory, "").String())
	assert.Equal(t, "5", rq.Spec.Hard.Name(corev1.ResourcePods, "").String())

	// All four NetworkPolicies present.
	for _, name := range []string{sandboxNetPolDefaultDeny, sandboxNetPolAllowIntraNS, sandboxNetPolAllowDNS, sandboxNetPolAllowEgress} {
		np := &networkingv1.NetworkPolicy{}
		assert.NoError(t, fc.Get(ctx, types.NamespacedName{Name: name, Namespace: nsName}, np), "expected NetworkPolicy %s", name)
	}

	// Broker ServiceAccount, Role, RoleBinding.
	assert.NoError(t, fc.Get(ctx, types.NamespacedName{Name: sandboxBrokerServiceAccountName, Namespace: nsName}, &corev1.ServiceAccount{}))
	role := &rbacv1.Role{}
	require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: sandboxBrokerRoleName, Namespace: nsName}, role))
	require.Len(t, role.Rules, 1)
	assert.ElementsMatch(t, []string{"pods", "pods/log", "pods/status", "services", "configmaps"}, role.Rules[0].Resources)
	rb := &rbacv1.RoleBinding{}
	require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: sandboxBrokerRoleBindingName, Namespace: nsName}, rb))
	assert.Equal(t, sandboxBrokerRoleName, rb.RoleRef.Name)
	require.Len(t, rb.Subjects, 1)
	assert.Equal(t, sandboxBrokerServiceAccountName, rb.Subjects[0].Name)

	// Status reflects Ready with the provisioned namespace name.
	updated := &evalhubv1.SandboxNamespace{}
	require.NoError(t, fc.Get(ctx, sandboxRequest(cr).NamespacedName, updated))
	assert.Equal(t, sandboxPhaseReady, updated.Status.Phase)
	assert.Equal(t, nsName, updated.Status.NamespaceName)
}

// TestSandboxReconciler_NetworkPolicyContent verifies the isolation semantics of the
// generated NetworkPolicy set, including that the node-metadata endpoint is excluded
// from every allow rule and never appears as an allowed CIDR.
func TestSandboxReconciler_NetworkPolicyContent(t *testing.T) {
	sc := sandboxScheme(t)
	cr := newSandboxCR("job-net", "control-ns")
	r, fc := buildSandboxReconciler(t, sc, cr)
	reconcileSandboxToReady(t, r, cr)

	nsName := "evalhub-sandbox-job-net"
	ctx := context.Background()
	get := func(name string) *networkingv1.NetworkPolicy {
		np := &networkingv1.NetworkPolicy{}
		require.NoError(t, fc.Get(ctx, types.NamespacedName{Name: name, Namespace: nsName}, np))
		return np
	}

	// default-deny: selects all pods, both directions, no allow rules.
	deny := get(sandboxNetPolDefaultDeny)
	assert.Empty(t, deny.Spec.PodSelector.MatchLabels)
	assert.ElementsMatch(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, deny.Spec.PolicyTypes)
	assert.Empty(t, deny.Spec.Ingress)
	assert.Empty(t, deny.Spec.Egress)

	// intra-namespace ingress: from pods in the same namespace (podSelector, no namespaceSelector).
	intra := get(sandboxNetPolAllowIntraNS)
	require.Len(t, intra.Spec.Ingress, 1)
	require.Len(t, intra.Spec.Ingress[0].From, 1)
	assert.NotNil(t, intra.Spec.Ingress[0].From[0].PodSelector)
	assert.Nil(t, intra.Spec.Ingress[0].From[0].NamespaceSelector)

	// DNS egress: UDP and TCP on port 53.
	dns := get(sandboxNetPolAllowDNS)
	require.Len(t, dns.Spec.Egress, 1)
	var protos []corev1.Protocol
	for _, p := range dns.Spec.Egress[0].Ports {
		require.NotNil(t, p.Port)
		assert.Equal(t, int32(53), p.Port.IntVal)
		require.NotNil(t, p.Protocol)
		protos = append(protos, *p.Protocol)
	}
	assert.ElementsMatch(t, []corev1.Protocol{corev1.ProtocolUDP, corev1.ProtocolTCP}, protos)

	// endpoint egress: an ipBlock per resolvable endpoint, each excluding the metadata IP.
	egress := get(sandboxNetPolAllowEgress)
	require.Len(t, egress.Spec.Egress, 1)
	var allowedCIDRs []string
	for _, peer := range egress.Spec.Egress[0].To {
		require.NotNil(t, peer.IPBlock, "endpoint egress peers must be ipBlocks")
		allowedCIDRs = append(allowedCIDRs, peer.IPBlock.CIDR)
		assert.Contains(t, peer.IPBlock.Except, metadataEndpointCIDR, "metadata endpoint must be excepted from every allow ipBlock")
	}
	assert.ElementsMatch(t, []string{"10.0.0.5/32", "10.0.0.6/32"}, allowedCIDRs)
	assert.NotContains(t, allowedCIDRs, metadataEndpointCIDR, "metadata endpoint must never be an allowed CIDR")
}

// TestSandboxReconciler_Idempotent verifies repeated reconciliation is safe: the
// finalizer is added exactly once and provisioning does not error on re-entry.
func TestSandboxReconciler_Idempotent(t *testing.T) {
	sc := sandboxScheme(t)
	cr := newSandboxCR("job-idem", "control-ns")
	r, fc := buildSandboxReconciler(t, sc, cr)
	req := sandboxRequest(cr)

	for i := 0; i < 4; i++ {
		_, err := r.Reconcile(context.Background(), req)
		require.NoErrorf(t, err, "reconcile pass %d", i)
	}

	updated := &evalhubv1.SandboxNamespace{}
	require.NoError(t, fc.Get(context.Background(), req.NamespacedName, updated))
	count := 0
	for _, f := range updated.Finalizers {
		if f == evalhubv1.SandboxFinalizerName {
			count++
		}
	}
	assert.Equal(t, 1, count, "finalizer must be added exactly once")
	assert.Equal(t, sandboxPhaseReady, updated.Status.Phase)
}

// TestSandboxReconciler_TeardownOnDelete verifies the deletion branch: the finalizer
// triggers a namespace delete (cascading cleanup) and is then removed so the CR can go.
func TestSandboxReconciler_TeardownOnDelete(t *testing.T) {
	sc := sandboxScheme(t)
	nsName := "evalhub-sandbox-job-del"
	cr := &evalhubv1.SandboxNamespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "job-del",
			Namespace:  "control-ns",
			Finalizers: []string{evalhubv1.SandboxFinalizerName},
		},
		Spec:   evalhubv1.SandboxNamespaceSpec{JobID: "job-del"},
		Status: evalhubv1.SandboxNamespaceStatus{NamespaceName: nsName, Phase: sandboxPhaseReady},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	r, fc := buildSandboxReconciler(t, sc, cr, ns)
	ctx := context.Background()

	// Deleting the CR only marks it (finalizer present) — the reconciler must finish it.
	require.NoError(t, fc.Delete(ctx, cr))

	_, err := r.Reconcile(ctx, sandboxRequest(cr))
	require.NoError(t, err)

	// The sandbox namespace was deleted (cascade removes everything inside it).
	err = fc.Get(ctx, types.NamespacedName{Name: nsName}, &corev1.Namespace{})
	assert.True(t, apierrors.IsNotFound(err), "sandbox namespace should be deleted")

	// Finalizer removed → the CR is fully gone.
	err = fc.Get(ctx, sandboxRequest(cr).NamespacedName, &evalhubv1.SandboxNamespace{})
	assert.True(t, apierrors.IsNotFound(err), "CR should be removed once the finalizer is cleared")
}

// TestSandboxReconciler_TeardownMissingNamespaceIsSuccess verifies teardown is
// idempotent when the namespace is already gone (restart-safety).
func TestSandboxReconciler_TeardownMissingNamespaceIsSuccess(t *testing.T) {
	sc := sandboxScheme(t)
	cr := &evalhubv1.SandboxNamespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "job-gone",
			Namespace:  "control-ns",
			Finalizers: []string{evalhubv1.SandboxFinalizerName},
		},
		Spec:   evalhubv1.SandboxNamespaceSpec{JobID: "job-gone"},
		Status: evalhubv1.SandboxNamespaceStatus{NamespaceName: "evalhub-sandbox-job-gone"},
	}
	r, fc := buildSandboxReconciler(t, sc, cr)
	ctx := context.Background()

	require.NoError(t, fc.Delete(ctx, cr))
	_, err := r.Reconcile(ctx, sandboxRequest(cr))
	require.NoError(t, err, "teardown with an already-absent namespace must succeed")

	err = fc.Get(ctx, sandboxRequest(cr).NamespacedName, &evalhubv1.SandboxNamespace{})
	assert.True(t, apierrors.IsNotFound(err))
}

// TestSandboxReconciler_NamespaceNameNotStampedBeforeCreation verifies that a
// provisioning failure at namespace creation does not persist the candidate name to
// status, so a later spec.namespaceName correction is still honoured (and teardown
// never targets a namespace that was never created).
func TestSandboxReconciler_NamespaceNameNotStampedBeforeCreation(t *testing.T) {
	sc := sandboxScheme(t)
	cr := newSandboxCR("job-fail", "control-ns")
	// Finalizer already present so Reconcile proceeds straight to provisioning.
	cr.Finalizers = []string{evalhubv1.SandboxFinalizerName}

	fc := fake.NewClientBuilder().
		WithScheme(sc).
		WithStatusSubresource(&evalhubv1.SandboxNamespace{}).
		WithObjects(cr).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*corev1.Namespace); ok {
					return apierrors.NewInternalError(fmt.Errorf("namespace create boom"))
				}
				return c.Create(ctx, obj, opts...)
			},
		}).
		Build()
	r := &SandboxNamespaceReconciler{Client: fc, Scheme: sc, EventRecorder: record.NewFakeRecorder(10)}

	_, err := r.Reconcile(context.Background(), sandboxRequest(cr))
	require.Error(t, err, "namespace creation failure must surface")

	updated := &evalhubv1.SandboxNamespace{}
	require.NoError(t, fc.Get(context.Background(), sandboxRequest(cr).NamespacedName, updated))
	assert.Equal(t, sandboxPhaseError, updated.Status.Phase)
	assert.Empty(t, updated.Status.NamespaceName, "namespace name must not be stamped before the namespace exists")
}

// TestHostIPFromEndpoint covers literal-IP extraction from the endpoint forms the
// eval-hub app may stamp onto the CR.
func TestHostIPFromEndpoint(t *testing.T) {
	cases := map[string]string{
		"http://10.0.0.5:8080":      "10.0.0.5",
		"https://10.0.0.6":          "10.0.0.6",
		"10.0.0.7:9000":             "10.0.0.7",
		"10.0.0.8":                  "10.0.0.8",
		"http://model.svc:8080":     "", // DNS name → no ipBlock peer
		"model.svc.cluster.local":   "",
		"":                          "",
		"http://[2001:db8::1]:8080": "2001:db8::1",
	}
	for in, want := range cases {
		assert.Equalf(t, want, hostIPFromEndpoint(in), "hostIPFromEndpoint(%q)", in)
	}
}

// TestEndpointEgressPeers verifies only literal IPs become peers, duplicates collapse,
// and every ipBlock excepts the metadata endpoint.
func TestEndpointEgressPeers(t *testing.T) {
	// DNS-only endpoints produce no peers.
	assert.Empty(t, endpointEgressPeers("http://a.svc", "b.svc.cluster.local"))

	peers := endpointEgressPeers("http://10.0.0.5:8080", "http://10.0.0.5:9090", "https://10.0.0.6")
	require.Len(t, peers, 2, "duplicate IPs must collapse")
	for _, p := range peers {
		require.NotNil(t, p.IPBlock)
		assert.Contains(t, p.IPBlock.Except, metadataEndpointCIDR)
	}
}

// TestBuildResourceQuotaHard verifies envelope translation, including the empty case.
func TestBuildResourceQuotaHard(t *testing.T) {
	empty, err := buildResourceQuotaHard(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	hard, err := buildResourceQuotaHard(&evalhubv1.SandboxResourceEnvelope{CPU: "1", Memory: "512Mi", MaxPods: 3})
	require.NoError(t, err)
	assert.Equal(t, "1", hard.Name(corev1.ResourceRequestsCPU, "").String())
	assert.Equal(t, "1", hard.Name(corev1.ResourceLimitsCPU, "").String())
	assert.Equal(t, "512Mi", hard.Name(corev1.ResourceRequestsMemory, "").String())
	assert.Equal(t, "512Mi", hard.Name(corev1.ResourceLimitsMemory, "").String())
	assert.Equal(t, "3", hard.Name(corev1.ResourcePods, "").String())

	_, err = buildResourceQuotaHard(&evalhubv1.SandboxResourceEnvelope{CPU: "not-a-quantity"})
	assert.Error(t, err, "invalid quantity must surface a parse error")
}

func TestEnsureResourceQuotaDeletesQuotaWhenEnvelopeIsEmpty(t *testing.T) {
	sc := sandboxScheme(t)
	nsName := "sandbox-ns"
	existing := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: sandboxResourceQuotaName, Namespace: nsName},
		Spec:       corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{corev1.ResourceRequestsCPU: resource.MustParse("2")}},
	}
	r, fc := buildSandboxReconciler(t, sc, existing)
	instance := &evalhubv1.SandboxNamespace{}
	ctx := context.Background()

	require.NoError(t, r.ensureResourceQuota(ctx, instance, nsName))
	err := fc.Get(ctx, types.NamespacedName{Name: sandboxResourceQuotaName, Namespace: nsName}, &corev1.ResourceQuota{})
	assert.True(t, apierrors.IsNotFound(err), "stale quota should be deleted")

	// An absent quota is an idempotent no-op.
	assert.NoError(t, r.ensureResourceQuota(ctx, instance, nsName))
}

// TestSandboxNamespaceName covers the name-resolution precedence.
func TestSandboxNamespaceName(t *testing.T) {
	r := &SandboxNamespaceReconciler{}

	// Status wins.
	statusSet := &evalhubv1.SandboxNamespace{
		ObjectMeta: metav1.ObjectMeta{Name: "cr"},
		Spec:       evalhubv1.SandboxNamespaceSpec{NamespaceName: "spec-ns"},
		Status:     evalhubv1.SandboxNamespaceStatus{NamespaceName: "status-ns"},
	}
	assert.Equal(t, "status-ns", r.sandboxNamespaceName(statusSet))

	// Spec next.
	specSet := &evalhubv1.SandboxNamespace{
		ObjectMeta: metav1.ObjectMeta{Name: "cr"},
		Spec:       evalhubv1.SandboxNamespaceSpec{NamespaceName: "spec-ns"},
	}
	assert.Equal(t, "spec-ns", r.sandboxNamespaceName(specSet))

	// Derived from CR name last.
	derived := &evalhubv1.SandboxNamespace{ObjectMeta: metav1.ObjectMeta{Name: "abc"}}
	assert.Equal(t, "evalhub-sandbox-abc", r.sandboxNamespaceName(derived))
}
