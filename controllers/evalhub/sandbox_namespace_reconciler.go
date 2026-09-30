/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// sandboxNamespaceControllerName matches ctrl.NewControllerManagedBy(mgr).Named(...) for logs and registration.
const sandboxNamespaceControllerName = "evalhub-sandbox-namespace"

const (
	// metadataEndpointIP is the link-local node metadata endpoint that must never
	// be reachable from a sandbox namespace (cloud instance metadata service). It is
	// never emitted as an allow peer, so egress to it stays blocked by default-deny.
	metadataEndpointIP = "169.254.169.254"

	// sandboxBrokerServiceAccountName is the name of the ServiceAccount the sandbox
	// broker uses inside the sandbox namespace.
	sandboxBrokerServiceAccountName = "sandbox-broker"
	// sandboxBrokerRoleName / sandboxBrokerRoleBindingName scope the broker to the
	// sandbox namespace only (Role, not ClusterRole) to prevent lateral movement.
	sandboxBrokerRoleName        = "sandbox-broker"
	sandboxBrokerRoleBindingName = "sandbox-broker"
	// sandboxResourceQuotaName is the ResourceQuota enforcing the sandbox envelope.
	sandboxResourceQuotaName = "sandbox-quota"

	// NetworkPolicy names within the sandbox namespace.
	sandboxNetPolDefaultDeny  = "sandbox-default-deny"
	sandboxNetPolAllowIntraNS = "sandbox-allow-intra-namespace"
	sandboxNetPolAllowDNS     = "sandbox-allow-dns"
	sandboxNetPolAllowEgress  = "sandbox-allow-egress-endpoints"

	// sandboxCRNameLabel links a provisioned sandbox namespace (and its resources)
	// back to the owning SandboxNamespace CR.
	sandboxCRNameLabel      = "trustyai.opendatahub.io/sandboxnamespace"
	sandboxCRNamespaceLabel = "trustyai.opendatahub.io/sandboxnamespace-namespace"
	sandboxComponentLabel   = "app.kubernetes.io/component"
	sandboxComponentValue   = "evalhub-sandbox"
	sandboxManagedByLabel   = "app.kubernetes.io/managed-by"
	sandboxManagedByValue   = "trustyai-service-operator"
)

// Sandbox lifecycle phases surfaced on SandboxNamespace status.
const (
	sandboxPhasePending     = "Pending"
	sandboxPhaseReady       = "Ready"
	sandboxPhaseTerminating = "Terminating"
	sandboxPhaseError       = "Error"
)

//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;delete
// The manager must itself hold every permission it grants to the sandbox broker
// Role, or RBAC escalation-prevention rejects the Role's creation. The broker gets
// full CRUD on pods (plus services/configmaps, already covered by other markers)
// and read-only access to the pods/log and pods/status subresources.
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods/log,verbs=get
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get
//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=sandboxnamespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=sandboxnamespaces/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=sandboxnamespaces/finalizers,verbs=update

// SandboxNamespaceReconciler provisions and tears down the isolated namespace that
// backs a single sandboxed evaluation job. It reacts to SandboxNamespace CRs
// (created by the eval-hub application) and creates a dedicated Namespace with a
// ResourceQuota, a default-deny NetworkPolicy set, and a broker ServiceAccount +
// namespaced Role/RoleBinding. A finalizer on the CR guarantees the namespace is
// deleted (cascading to everything inside it) even across operator restarts.
type SandboxNamespaceReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	EventRecorder record.EventRecorder
}

// Reconcile drives a SandboxNamespace CR toward its desired state.
func (r *SandboxNamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	instance := &evalhubv1.SandboxNamespace{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	nsName := r.sandboxNamespaceName(instance)

	// Deletion / terminal teardown.
	if !instance.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(instance, evalhubv1.SandboxFinalizerName) {
			if err := r.teardownSandbox(ctx, instance); err != nil {
				logger.Error(err, "Failed to tear down sandbox namespace", "namespace", nsName)
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(instance, evalhubv1.SandboxFinalizerName)
			if err := r.Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Ensure the finalizer is present before provisioning anything, so a crash
	// mid-provisioning still leaves cleanup guaranteed.
	if !controllerutil.ContainsFinalizer(instance, evalhubv1.SandboxFinalizerName) {
		controllerutil.AddFinalizer(instance, evalhubv1.SandboxFinalizerName)
		if err := r.Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if err := r.provisionSandbox(ctx, instance, nsName); err != nil {
		logger.Error(err, "Failed to provision sandbox namespace", "namespace", nsName)
		// Preserve whatever namespace name is already recorded (set only once the
		// namespace actually exists). Never stamp a not-yet-created candidate name
		// here, or a later spec.namespaceName correction would be ignored.
		r.setStatus(ctx, instance, sandboxPhaseError, instance.Status.NamespaceName)
		return ctrl.Result{}, err
	}

	r.setStatus(ctx, instance, sandboxPhaseReady, nsName)
	return ctrl.Result{}, nil
}

// sandboxNamespaceName returns the name of the namespace to provision for this CR.
// A caller-specified spec.namespaceName wins; otherwise a deterministic,
// DNS-1123-safe name is derived from the CR name.
func (r *SandboxNamespaceReconciler) sandboxNamespaceName(instance *evalhubv1.SandboxNamespace) string {
	if instance.Status.NamespaceName != "" {
		return instance.Status.NamespaceName
	}
	if instance.Spec.NamespaceName != "" {
		return instance.Spec.NamespaceName
	}
	return normalizeDNS1123LabelValue("evalhub-sandbox-" + instance.Name)
}

// provisionSandbox creates (idempotently) the sandbox namespace and all resources
// scoped within it.
func (r *SandboxNamespaceReconciler) provisionSandbox(ctx context.Context, instance *evalhubv1.SandboxNamespace, nsName string) error {
	if err := r.ensureNamespace(ctx, instance, nsName); err != nil {
		return fmt.Errorf("ensure namespace: %w", err)
	}
	// The namespace now exists: record its name in status before provisioning
	// anything inside it. Persisting NamespaceName only after ensureNamespace
	// succeeds makes it authoritative for name resolution (sandboxNamespaceName)
	// and teardown — a not-yet-created candidate name is never stamped, so a
	// later spec.namespaceName correction is still honoured, and deletion always
	// targets the namespace that was actually created.
	if instance.Status.NamespaceName != nsName {
		instance.Status.NamespaceName = nsName
		if err := r.Status().Update(ctx, instance); err != nil {
			return fmt.Errorf("record sandbox namespace name: %w", err)
		}
	}
	if err := r.ensureResourceQuota(ctx, instance, nsName); err != nil {
		return fmt.Errorf("ensure resourcequota: %w", err)
	}
	if err := r.ensureNetworkPolicies(ctx, instance, nsName); err != nil {
		return fmt.Errorf("ensure networkpolicies: %w", err)
	}
	if err := r.ensureBrokerRBAC(ctx, instance, nsName); err != nil {
		return fmt.Errorf("ensure broker rbac: %w", err)
	}
	return nil
}

// teardownSandbox deletes every namespace this CR owns; the namespace delete
// cascades to all resources inside it. It is idempotent (no owned namespace is
// success).
//
// Teardown discovers namespaces by the CR's ownership labels rather than by the
// currently-resolved name. This guarantees a namespace provisioned earlier is
// still cleaned up even if its name later diverges from sandboxNamespaceName —
// for example when the namespace was created but the status write recording its
// name failed and spec.namespaceName was subsequently changed. A namespace the
// operator did not provision for this CR (missing ownership labels) is never
// deleted, so a spec.namespaceName pointing at a pre-existing or foreign
// namespace remains untouched.
func (r *SandboxNamespaceReconciler) teardownSandbox(ctx context.Context, instance *evalhubv1.SandboxNamespace) error {
	namespaces := &corev1.NamespaceList{}
	if err := r.List(ctx, namespaces, client.MatchingLabels{
		sandboxManagedByLabel:   sandboxManagedByValue,
		sandboxCRNameLabel:      instance.Name,
		sandboxCRNamespaceLabel: instance.Namespace,
	}); err != nil {
		return err
	}
	for i := range namespaces.Items {
		existing := &namespaces.Items[i]
		if !sandboxNamespaceOwnedBy(existing, instance) {
			continue
		}
		if err := r.Delete(ctx, existing); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// sandboxNamespaceOwnedBy reports whether ns was provisioned by the operator for
// this specific SandboxNamespace CR, identified by the managed-by label plus the
// CR name/namespace back-links stamped in sandboxLabels.
func sandboxNamespaceOwnedBy(ns *corev1.Namespace, instance *evalhubv1.SandboxNamespace) bool {
	labels := ns.GetLabels()
	return labels[sandboxManagedByLabel] == sandboxManagedByValue &&
		labels[sandboxCRNameLabel] == instance.Name &&
		labels[sandboxCRNamespaceLabel] == instance.Namespace
}

// sandboxLabels returns the common labels stamped on the sandbox namespace and its
// resources, linking them to the parent job and owning CR for tracking and cleanup.
func sandboxLabels(instance *evalhubv1.SandboxNamespace) map[string]string {
	labels := map[string]string{
		sandboxManagedByLabel:   sandboxManagedByValue,
		sandboxComponentLabel:   sandboxComponentValue,
		sandboxCRNameLabel:      instance.Name,
		sandboxCRNamespaceLabel: instance.Namespace,
	}
	if instance.Spec.JobID != "" {
		labels[evalHubJobIDLabel] = normalizeDNS1123LabelValue(instance.Spec.JobID)
	}
	if instance.Spec.EvalHubInstanceName != "" {
		labels[evalHubInstanceNameLabel] = instance.Spec.EvalHubInstanceName
	}
	if instance.Spec.EvalHubInstanceNamespace != "" {
		labels[evalHubInstanceNamespaceLabel] = instance.Spec.EvalHubInstanceNamespace
	}
	labels["app.kubernetes.io/version"] = constants.Version
	return labels
}

// ensureNamespace creates the sandbox namespace if absent, or reconciles its labels.
func (r *SandboxNamespaceReconciler) ensureNamespace(ctx context.Context, instance *evalhubv1.SandboxNamespace, nsName string) error {
	logger := log.FromContext(ctx)
	desired := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nsName,
			Labels: sandboxLabels(instance),
		},
	}
	existing := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: nsName}, existing)
	if errors.IsNotFound(err) {
		logger.Info("Creating sandbox namespace", "namespace", nsName)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	// Refuse to adopt a namespace the operator did not provision for this CR. Without
	// this guard a spec.namespaceName pointing at an existing namespace (e.g. a
	// system namespace) would be relabelled, locked down with default-deny network
	// policies, and ultimately deleted on teardown.
	if !sandboxNamespaceOwnedBy(existing, instance) {
		return fmt.Errorf("refusing to adopt namespace %q: not owned by SandboxNamespace %s/%s", nsName, instance.Namespace, instance.Name)
	}
	// If the namespace is being deleted, wait for it to disappear before recreating.
	if !existing.DeletionTimestamp.IsZero() {
		return fmt.Errorf("sandbox namespace %q is terminating; requeue", nsName)
	}
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	changed := false
	for k, v := range sandboxLabels(instance) {
		if existing.Labels[k] != v {
			existing.Labels[k] = v
			changed = true
		}
	}
	if changed {
		return r.Update(ctx, existing)
	}
	return nil
}

// ensureResourceQuota creates or updates the ResourceQuota bounding the sandbox.
func (r *SandboxNamespaceReconciler) ensureResourceQuota(ctx context.Context, instance *evalhubv1.SandboxNamespace, nsName string) error {
	hard, err := buildResourceQuotaHard(instance.Spec.ResourceEnvelope)
	if err != nil {
		return err
	}
	existing := &corev1.ResourceQuota{}
	getErr := r.Get(ctx, types.NamespacedName{Name: sandboxResourceQuotaName, Namespace: nsName}, existing)
	if len(hard) == 0 {
		if errors.IsNotFound(getErr) {
			return nil
		}
		if getErr != nil {
			return getErr
		}
		if err := r.Delete(ctx, existing); err != nil && !errors.IsNotFound(err) {
			return err
		}
		return nil
	}
	desired := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sandboxResourceQuotaName,
			Namespace: nsName,
			Labels:    sandboxLabels(instance),
		},
		Spec: corev1.ResourceQuotaSpec{Hard: hard},
	}
	if errors.IsNotFound(getErr) {
		return r.Create(ctx, desired)
	}
	if getErr != nil {
		return getErr
	}
	existing.Spec.Hard = hard
	return r.Update(ctx, existing)
}

// buildResourceQuotaHard translates a resource envelope into ResourceQuota hard
// limits. CPU/memory constrain both requests and limits; MaxPods caps pods.
func buildResourceQuotaHard(env *evalhubv1.SandboxResourceEnvelope) (corev1.ResourceList, error) {
	hard := corev1.ResourceList{}
	if env == nil {
		return hard, nil
	}
	if env.CPU != "" {
		q, err := resource.ParseQuantity(env.CPU)
		if err != nil {
			return nil, fmt.Errorf("parse sandbox cpu %q: %w", env.CPU, err)
		}
		hard[corev1.ResourceRequestsCPU] = q
		hard[corev1.ResourceLimitsCPU] = q
	}
	if env.Memory != "" {
		q, err := resource.ParseQuantity(env.Memory)
		if err != nil {
			return nil, fmt.Errorf("parse sandbox memory %q: %w", env.Memory, err)
		}
		hard[corev1.ResourceRequestsMemory] = q
		hard[corev1.ResourceLimitsMemory] = q
	}
	if env.MaxPods > 0 {
		hard[corev1.ResourcePods] = *resource.NewQuantity(int64(env.MaxPods), resource.DecimalSI)
	}
	return hard, nil
}

// ensureNetworkPolicies creates the default-deny + narrow-allow NetworkPolicy set.
func (r *SandboxNamespaceReconciler) ensureNetworkPolicies(ctx context.Context, instance *evalhubv1.SandboxNamespace, nsName string) error {
	for _, np := range buildNetworkPolicies(instance, nsName) {
		if err := r.ensureNetworkPolicy(ctx, np); err != nil {
			return err
		}
	}
	// When no endpoint resolves to a literal IP the allow-egress policy is not
	// built. Remove any copy left over from an earlier reconcile so a stale allow
	// rule cannot outlive the endpoints that justified it.
	if len(endpointEgressPeers(instance.Spec.EvalHubAPIEndpoint, instance.Spec.ModelEndpointURL)) == 0 {
		if err := r.deleteNetworkPolicy(ctx, nsName, sandboxNetPolAllowEgress); err != nil {
			return err
		}
	}
	return nil
}

// deleteNetworkPolicy removes a NetworkPolicy by name, treating an already-absent
// policy as success and surfacing any other Get/Delete error.
func (r *SandboxNamespaceReconciler) deleteNetworkPolicy(ctx context.Context, nsName, name string) error {
	existing := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: nsName}, existing)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := r.Delete(ctx, existing); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *SandboxNamespaceReconciler) ensureNetworkPolicy(ctx context.Context, desired *networkingv1.NetworkPolicy) error {
	existing := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if errors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	return r.Update(ctx, existing)
}

// buildNetworkPolicies returns the NetworkPolicy set enforcing sandbox isolation.
//
// The kube API server and the node metadata endpoint (169.254.169.254) are blocked
// implicitly by the default-deny egress policy (they appear in no allow rule). Each
// allow peer is a host-sized ipBlock for a single resolved endpoint IP, and the
// metadata endpoint is never emitted as a peer, so it can never be reached.
func buildNetworkPolicies(instance *evalhubv1.SandboxNamespace, nsName string) []*networkingv1.NetworkPolicy {
	labels := sandboxLabels(instance)
	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	dnsPort := intstr.FromInt(53)

	policies := []*networkingv1.NetworkPolicy{
		// 1. default-deny-all ingress and egress.
		{
			ObjectMeta: metav1.ObjectMeta{Name: sandboxNetPolDefaultDeny, Namespace: nsName, Labels: labels},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			},
		},
		// 2. allow ingress only from pods within the sandbox namespace.
		{
			ObjectMeta: metav1.ObjectMeta{Name: sandboxNetPolAllowIntraNS, Namespace: nsName, Labels: labels},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}}},
				},
			},
		},
		// 3. allow DNS egress (kube-dns / openshift-dns) on UDP+TCP 53.
		{
			ObjectMeta: metav1.ObjectMeta{Name: sandboxNetPolAllowDNS, Namespace: nsName, Labels: labels},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				Egress: []networkingv1.NetworkPolicyEgressRule{
					{
						To: []networkingv1.NetworkPolicyPeer{
							{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "openshift-dns"}}},
							{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}}},
						},
						Ports: []networkingv1.NetworkPolicyPort{
							{Protocol: &udp, Port: &dnsPort},
							{Protocol: &tcp, Port: &dnsPort},
						},
					},
				},
			},
		},
	}

	// 4. allow egress to the eval-hub API and model inference endpoints, when they
	// resolve to concrete IPs. Hostname-only endpoints are left to the L7 egress
	// proxy (separate epic); the default-deny still blocks everything else.
	egressPeers := endpointEgressPeers(instance.Spec.EvalHubAPIEndpoint, instance.Spec.ModelEndpointURL)
	if len(egressPeers) > 0 {
		policies = append(policies, &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: sandboxNetPolAllowEgress, Namespace: nsName, Labels: labels},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				Egress:      []networkingv1.NetworkPolicyEgressRule{{To: egressPeers}},
			},
		})
	}

	return policies
}

// endpointEgressPeers turns endpoint URLs/hosts into NetworkPolicy peers. Only
// endpoints that resolve to a literal IP become ipBlock peers, each a host-sized
// CIDR (/32 for IPv4, /128 for IPv6). The node metadata endpoint is skipped
// entirely rather than allowed, so egress to it stays blocked by default-deny.
// Hostname-only endpoints yield no peer.
func endpointEgressPeers(endpoints ...string) []networkingv1.NetworkPolicyPeer {
	var peers []networkingv1.NetworkPolicyPeer
	seen := map[string]struct{}{}
	for _, ep := range endpoints {
		ip := hostIPFromEndpoint(ep)
		if ip == "" {
			continue
		}
		parsed := net.ParseIP(ip)
		if parsed == nil {
			continue
		}
		// Never allow egress to the node metadata endpoint.
		if parsed.Equal(net.ParseIP(metadataEndpointIP)) {
			continue
		}
		prefix := "/128"
		if parsed.To4() != nil {
			prefix = "/32"
		}
		cidr := ip + prefix
		if _, dup := seen[cidr]; dup {
			continue
		}
		seen[cidr] = struct{}{}
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: cidr},
		})
	}
	return peers
}

// hostIPFromEndpoint extracts a literal IPv4/IPv6 address from an endpoint that may
// be a bare host, host:port, or full URL. It returns "" when the host is a DNS name.
func hostIPFromEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	host := endpoint
	if strings.Contains(endpoint, "://") {
		if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
			host = u.Host
		}
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if ip := net.ParseIP(host); ip != nil {
		return host
	}
	return ""
}

// ensureBrokerRBAC creates the sandbox broker ServiceAccount and a namespaced Role
// (pods, services, configmaps) + RoleBinding, all scoped to the sandbox namespace.
func (r *SandboxNamespaceReconciler) ensureBrokerRBAC(ctx context.Context, instance *evalhubv1.SandboxNamespace, nsName string) error {
	labels := sandboxLabels(instance)

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: sandboxBrokerServiceAccountName, Namespace: nsName, Labels: labels},
	}
	if err := r.ensureObject(ctx, sa, &corev1.ServiceAccount{}, func(existing client.Object) bool { return false }); err != nil {
		return err
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: sandboxBrokerRoleName, Namespace: nsName, Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "services", "configmaps"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				// Log and status are read-only subresources used to observe sandbox pods.
				APIGroups: []string{""},
				Resources: []string{"pods/log", "pods/status"},
				Verbs:     []string{"get"},
			},
		},
	}
	if err := r.ensureObject(ctx, role, &rbacv1.Role{}, func(existing client.Object) bool {
		e := existing.(*rbacv1.Role)
		e.Rules = role.Rules
		return true
	}); err != nil {
		return err
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: sandboxBrokerRoleBindingName, Namespace: nsName, Labels: labels},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     sandboxBrokerRoleName,
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: sandboxBrokerServiceAccountName, Namespace: nsName},
		},
	}
	return r.ensureObject(ctx, rb, &rbacv1.RoleBinding{}, func(existing client.Object) bool {
		e := existing.(*rbacv1.RoleBinding)
		e.RoleRef = rb.RoleRef
		e.Subjects = rb.Subjects
		return true
	})
}

// ensureObject creates desired when absent; otherwise applies mutate to the fetched
// object and updates it when mutate reports a change. into must be a fresh empty
// object of the same kind as desired.
func (r *SandboxNamespaceReconciler) ensureObject(ctx context.Context, desired, into client.Object, mutate func(existing client.Object) bool) error {
	key := client.ObjectKeyFromObject(desired)
	err := r.Get(ctx, key, into)
	if errors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if mutate != nil && mutate(into) {
		return r.Update(ctx, into)
	}
	return nil
}

// setStatus updates the SandboxNamespace status phase and provisioned namespace name.
func (r *SandboxNamespaceReconciler) setStatus(ctx context.Context, instance *evalhubv1.SandboxNamespace, phase, nsName string) {
	logger := log.FromContext(ctx)
	if instance.Status.Phase == phase && instance.Status.NamespaceName == nsName {
		return
	}
	instance.Status.Phase = phase
	instance.Status.NamespaceName = nsName
	if err := r.Status().Update(ctx, instance); err != nil {
		logger.Error(err, "Failed to update SandboxNamespace status", "phase", phase)
	}
}

// SetupWithManager is unused directly; registration goes through
// registerEvalHubSandboxNamespaceController to match the other EvalHub auxiliary
// controllers.
func registerEvalHubSandboxNamespaceController(mgr manager.Manager) error {
	r := &SandboxNamespaceReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		EventRecorder: mgr.GetEventRecorderFor("trustyai-service-operator"),
	}
	// Child resources live in the sandbox namespace, which differs from the CR's
	// namespace. Cross-namespace owner references are not permitted, so drift on the
	// isolation resources (default-deny NetworkPolicy, ResourceQuota, broker Role/
	// RoleBinding) or on the namespace's own labels would otherwise go uncorrected
	// until the CR changed. Label-based watches map each child back to its owning CR
	// request so tampering or deletion triggers a reconcile that restores desired
	// state. ServiceAccount is deliberately not watched: it is high-cardinality
	// (a cluster-wide informer would cache every namespace's built-in SAs) and its
	// drift does not weaken the isolation guarantee.
	enqueueOwner := handler.EnqueueRequestsFromMapFunc(mapSandboxLabelsToCR)
	return ctrl.NewControllerManagedBy(mgr).
		Named(sandboxNamespaceControllerName).
		For(&evalhubv1.SandboxNamespace{}).
		Watches(&corev1.Namespace{}, enqueueOwner).
		Watches(&corev1.ResourceQuota{}, enqueueOwner).
		Watches(&networkingv1.NetworkPolicy{}, enqueueOwner).
		Watches(&rbacv1.Role{}, enqueueOwner).
		Watches(&rbacv1.RoleBinding{}, enqueueOwner).
		Complete(r)
}

// mapSandboxLabelsToCR maps a labelled child object back to its owning
// SandboxNamespace request using the CR name/namespace back-links stamped by
// sandboxLabels. Objects missing either ownership label enqueue nothing, so
// unrelated resources caught by the watch are ignored.
func mapSandboxLabelsToCR(_ context.Context, obj client.Object) []ctrl.Request {
	labels := obj.GetLabels()
	name := labels[sandboxCRNameLabel]
	namespace := labels[sandboxCRNamespaceLabel]
	if name == "" || namespace == "" {
		return nil
	}
	return []ctrl.Request{{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	}}
}
