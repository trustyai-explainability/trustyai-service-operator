package nemo_guardrails

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WorkloadNamespaceLabel must be set to WorkloadNamespaceLabelValue on a namespace
// before a NemoGuardrails custom resource in another namespace can place its workload there.
const (
	WorkloadNamespaceLabel      = "trustyai.opendatahub.io/nemo-guardrails-workload"
	WorkloadNamespaceLabelValue = "true"
)

// deriveInfraNamespace returns the namespace where NemoGuardrails workloads should be deployed.
// When the controller runs in a known ODH/RHOAI install namespace, workloads are centralized
// into that platform's shared AI Gateway infra namespace. Otherwise, workloads default to the custom resource's own namespace.
func deriveInfraNamespace(controllerNs, crNamespace string) string {
	switch controllerNs {
	case "redhat-ods-applications":
		return "redhat-ai-gateway-infra"
	case "opendatahub":
		return "odh-ai-gateway-infra"
	default:
		// Unknown controller namespace, fall back to the custom resource's own namespace.
		return crNamespace
	}
}

// workloadIdentity names the objects created for one NemoGuardrails in targetNamespace.
type workloadIdentity struct {
	Name           string
	Selector       string
	PortName       string
	AdminPortName  string
	ServiceAccount string
}

// maxServicePortNameLen is the Kubernetes Service port name limit.
const maxServicePortNameLen = 15

func newWorkloadIdentity(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) workloadIdentity {
	name := nemoGuardrails.Name
	// A shared infra namespace uses {namespace}-{cr-name} so two projects can reuse this custom resource name.
	if targetNamespace != "" && targetNamespace != nemoGuardrails.Namespace {
		name = prefixedWorkloadObjectName(nemoGuardrails.Namespace, nemoGuardrails.Name)
	}
	// Same-namespace installs keep the custom resource name as the selector.
	// In a shared namespace the object name already includes the owning namespace.
	selector := name
	portName := name
	if len(portName) > maxServicePortNameLen {
		portName = "http"
		if utils.RequiresAuth(nemoGuardrails) {
			portName = "https"
		}
	}
	adminPortName := name + "-admin"
	if len(adminPortName) > maxServicePortNameLen {
		adminPortName = "admin"
	}
	return workloadIdentity{
		Name:           name,
		Selector:       selector,
		PortName:       portName,
		AdminPortName:  adminPortName,
		ServiceAccount: name + "-serviceaccount",
	}
}

// prefixedWorkloadObjectName is the Deployment, Service, and Route name when the workload
// is placed in a namespace other than the custom resource's namespace.
func prefixedWorkloadObjectName(ownerNamespace, ownerName string) string {
	if ownerNamespace == "" || ownerName == "" {
		return ownerName
	}
	return dns1123Name(ownerNamespace + "-" + ownerName)
}

// copiedObjectName is the name of a ConfigMap or Secret copied into a different namespace.
func copiedObjectName(ownerNamespace, ownerName, sourceName, targetNamespace string) string {
	if targetNamespace == "" || targetNamespace == ownerNamespace || sourceName == "" {
		return sourceName
	}
	return dns1123Name(ownerNamespace + "-" + ownerName + "-" + sourceName)
}

// defaultConfigCopyName is the copy of an operator-namespace default ConfigMap.
// Same-namespace copies keep the historical untruncated name.
// Infra copies keep the default suffix, as in {namespace}-{cr-name}-default-pii.
func defaultConfigCopyName(ownerNamespace, ownerName, sourceName, targetNamespace string) string {
	if targetNamespace == "" || targetNamespace == ownerNamespace {
		return ownerName + "-" + sourceName
	}
	return dns1123Name(ownerNamespace + "-" + ownerName + "-" + defaultConfigShortName(sourceName))
}

// defaultConfigShortName turns trustyai-service-operator-nemo-guardrails-default-pii into default-pii.
func defaultConfigShortName(sourceName string) string {
	head := strings.TrimSuffix(nemoGuardrailsDefaultConfigPrefix, "default")
	if rest, ok := strings.CutPrefix(sourceName, head); ok && (rest == "default" || strings.HasPrefix(rest, "default-")) {
		return rest
	}
	return sourceName
}

func dns1123Name(name string) string {
	name = strings.ToLower(name)
	if len(name) <= 63 {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(sum[:4])
	return strings.TrimRight(name[:63-len(suffix)-1], "-") + "-" + suffix
}

// workloadNamespace is the namespace where this reconciler's Deployment, Service, and Route live.
func (r *NemoGuardrailsReconciler) workloadNamespace(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails) string {
	return deriveInfraNamespace(r.Namespace, nemoGuardrails.Namespace)
}

// rejectUnownedWorkload reports a name collision in namespace before any object there is created or any previous workload is deleted.
func (r *NemoGuardrailsReconciler) rejectUnownedWorkload(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, namespace string) error {
	identity := newWorkloadIdentity(nemoGuardrails, namespace)
	name := identity.Name
	meta := func(objectName string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: objectName, Namespace: namespace}
	}
	checks := []struct {
		kind string
		obj  client.Object
	}{
		{"Deployment", &appsv1.Deployment{ObjectMeta: meta(name)}},
		{"Service", &corev1.Service{ObjectMeta: meta(name)}},
		{"ConfigMap", &corev1.ConfigMap{ObjectMeta: meta(name + "-ca-bundle")}},
	}
	if nemoGuardrails.Spec.ExposeRoute != nil && *nemoGuardrails.Spec.ExposeRoute {
		checks = append(checks, struct {
			kind string
			obj  client.Object
		}{kind: "Route", obj: &routev1.Route{ObjectMeta: meta(name)}})
	}
	if utils.RequiresAuth(nemoGuardrails) {
		checks = append(checks,
			struct {
				kind string
				obj  client.Object
			}{kind: "ConfigMap", obj: &corev1.ConfigMap{ObjectMeta: meta(GetRBACConfigName(name, false))}},
			struct {
				kind string
				obj  client.Object
			}{kind: "ConfigMap", obj: &corev1.ConfigMap{ObjectMeta: meta(GetRBACConfigName(name, true))}},
			struct {
				kind string
				obj  client.Object
			}{kind: "ServiceAccount", obj: &corev1.ServiceAccount{ObjectMeta: meta(identity.ServiceAccount)}},
		)
	}
	for _, check := range checks {
		if err := r.requireOwnedOrAbsent(ctx, nemoGuardrails, check.kind, check.obj); err != nil {
			return err
		}
	}
	copiedNames, err := r.copiedConfigMapNames(ctx, nemoGuardrails, namespace)
	if err != nil {
		return err
	}
	for _, configName := range copiedNames {
		if err := r.requireOwnedOrAbsent(ctx, nemoGuardrails, "ConfigMap", &corev1.ConfigMap{ObjectMeta: meta(configName)}); err != nil {
			return err
		}
	}
	for _, secretName := range copiedSecretNames(nemoGuardrails, namespace) {
		if err := r.requireOwnedOrAbsent(ctx, nemoGuardrails, "Secret", &corev1.Secret{ObjectMeta: meta(secretName)}); err != nil {
			return err
		}
	}
	return nil
}

// copiedSecretNames lists Secret names that reconciliation will copy into namespace.
func copiedSecretNames(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, namespace string) []string {
	if nemoGuardrails.Namespace == namespace {
		return nil
	}
	var names []string
	seen := map[string]struct{}{}
	for _, envVar := range nemoGuardrails.Spec.Env {
		if envVar.ValueFrom == nil || envVar.ValueFrom.SecretKeyRef == nil {
			continue
		}
		secretName := envVar.ValueFrom.SecretKeyRef.Name
		if secretName == "" {
			continue
		}
		copyName := copiedObjectName(nemoGuardrails.Namespace, nemoGuardrails.Name, secretName, namespace)
		if _, ok := seen[copyName]; ok {
			continue
		}
		seen[copyName] = struct{}{}
		names = append(names, copyName)
	}
	return names
}

// copiedConfigMapNames lists ConfigMap names that reconciliation will copy into namespace.
// A user ConfigMap already in namespace is mounted in place and is not included.
func (r *NemoGuardrailsReconciler) copiedConfigMapNames(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, namespace string) ([]string, error) {
	var names []string
	for _, nemoConfig := range nemoGuardrails.Spec.NemoConfigs {
		for _, configCM := range nemoConfig.ConfigMaps {
			if strings.HasPrefix(configCM, nemoGuardrailsDefaultConfigPrefix) {
				source := &corev1.ConfigMap{}
				err := r.Get(ctx, types.NamespacedName{Name: configCM, Namespace: r.Namespace}, source)
				if err != nil && !errors.IsNotFound(err) {
					return nil, err
				}
				if err == nil || nemoGuardrails.Namespace != namespace {
					names = append(names, defaultConfigCopyName(nemoGuardrails.Namespace, nemoGuardrails.Name, configCM, namespace))
					continue
				}
			}
			if nemoGuardrails.Namespace != namespace {
				names = append(names, copiedObjectName(nemoGuardrails.Namespace, nemoGuardrails.Name, configCM, namespace))
			}
		}
	}
	if ca := nemoGuardrails.Spec.CABundleConfig; ca != nil && ca.ConfigMapName != "" {
		sourceNamespace, nsErr := userCABundleNamespace(nemoGuardrails)
		if nsErr == nil && sourceNamespace != namespace {
			source := &corev1.ConfigMap{}
			err := r.Get(ctx, types.NamespacedName{Name: ca.ConfigMapName, Namespace: sourceNamespace}, source)
			if err == nil && source.Namespace != namespace {
				names = append(names, copiedObjectName(nemoGuardrails.Namespace, nemoGuardrails.Name, source.Name, namespace))
			} else if err != nil && !errors.IsNotFound(err) {
				return nil, err
			}
		}
	}
	return names, nil
}

// previousWorkloadNamespace is the namespace of the Deployment, Service, and Route that was last created.
func previousWorkloadNamespace(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails) string {
	if nemoGuardrails.Status.WorkloadNamespace != "" {
		return nemoGuardrails.Status.WorkloadNamespace
	}
	return nemoGuardrails.Namespace
}

// releasePreviousWorkload rejects a target-namespace name collision before any object is created there.
// The previous namespace is removed by releaseReadyWorkload once the new Deployment is Ready.
func (r *NemoGuardrailsReconciler) releasePreviousWorkload(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) error {
	return r.rejectUnownedWorkload(ctx, nemoGuardrails, targetNamespace)
}

// releaseReadyWorkload deletes the previous namespace after the Deployment in targetNamespace is Ready.
// An exposed Route must also be admitted. A missing or stuck rollout leaves the previous stack serving.
func (r *NemoGuardrailsReconciler) releaseReadyWorkload(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) error {
	previous := previousWorkloadNamespace(nemoGuardrails)
	if previous == "" || previous == targetNamespace {
		return nil
	}
	identity := newWorkloadIdentity(nemoGuardrails, targetNamespace)
	ready, err := utils.CheckDeploymentReady(ctx, r.Client, identity.Name, targetNamespace)
	if err != nil {
		if stderrors.Is(err, utils.ErrDeploymentProgressDeadlineExceeded) {
			return nil
		}
		return err
	}
	if !ready {
		return nil
	}
	if nemoGuardrails.Spec.ExposeRoute != nil && *nemoGuardrails.Spec.ExposeRoute {
		admitted, err := utils.RouteAdmitted(ctx, r.Client, identity.Name, targetNamespace)
		if err != nil {
			return err
		}
		if !admitted {
			return nil
		}
	}
	current := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: identity.Name, Namespace: targetNamespace}, current); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !ownedBy(current, nemoGuardrails) {
		return nil
	}
	return r.deleteOwnedInNamespace(ctx, nemoGuardrails, previous)
}

func (r *NemoGuardrailsReconciler) deleteOwnedInNamespace(ctx context.Context, owner client.Object, namespace string) error {
	if namespace == "" {
		return nil
	}
	if err := deleteOwned(ctx, r.Client, namespace, owner, &appsv1.DeploymentList{}, func(list client.ObjectList) []client.Object {
		items := list.(*appsv1.DeploymentList).Items
		objects := make([]client.Object, len(items))
		for i := range items {
			objects[i] = &items[i]
		}
		return objects
	}); err != nil {
		return err
	}
	if err := deleteOwned(ctx, r.Client, namespace, owner, &corev1.ServiceList{}, func(list client.ObjectList) []client.Object {
		items := list.(*corev1.ServiceList).Items
		objects := make([]client.Object, len(items))
		for i := range items {
			objects[i] = &items[i]
		}
		return objects
	}); err != nil {
		return err
	}
	if err := deleteOwned(ctx, r.Client, namespace, owner, &corev1.ConfigMapList{}, func(list client.ObjectList) []client.Object {
		items := list.(*corev1.ConfigMapList).Items
		objects := make([]client.Object, len(items))
		for i := range items {
			objects[i] = &items[i]
		}
		return objects
	}); err != nil {
		return err
	}
	if err := deleteOwned(ctx, r.Client, namespace, owner, &corev1.ServiceAccountList{}, func(list client.ObjectList) []client.Object {
		items := list.(*corev1.ServiceAccountList).Items
		objects := make([]client.Object, len(items))
		for i := range items {
			objects[i] = &items[i]
		}
		return objects
	}); err != nil {
		return err
	}
	if err := deleteOwned(ctx, r.Client, namespace, owner, &routev1.RouteList{}, func(list client.ObjectList) []client.Object {
		items := list.(*routev1.RouteList).Items
		objects := make([]client.Object, len(items))
		for i := range items {
			objects[i] = &items[i]
		}
		return objects
	}); err != nil {
		return err
	}
	if err := deleteOwned(ctx, r.Client, namespace, owner, &corev1.SecretList{}, func(list client.ObjectList) []client.Object {
		items := list.(*corev1.SecretList).Items
		objects := make([]client.Object, len(items))
		for i := range items {
			objects[i] = &items[i]
		}
		return objects
	}); err != nil {
		return err
	}
	// Objects created before the owner label existed are still selected by name.
	// Cross-namespace workloads use {namespace}-{cr-name}; earlier installs used only the custom resource name.
	names := []string{owner.GetName()}
	if prefixed := prefixedWorkloadObjectName(owner.GetNamespace(), owner.GetName()); prefixed != owner.GetName() {
		names = append(names, prefixed)
	}
	var named []client.Object
	for _, name := range names {
		named = append(named,
			&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name + "-ca-bundle", Namespace: namespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name + "-rbac-proxy-config", Namespace: namespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name + "-rbac-proxy-admin-config", Namespace: namespace}},
			&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name + "-serviceaccount", Namespace: namespace}},
			&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
		)
	}
	for _, obj := range named {
		if err := r.deleteNamedIfOwned(ctx, owner, obj); err != nil {
			return err
		}
	}
	return nil
}

// retireUnscopedNames removes workload objects that used only the custom resource name
// after the namespace-prefixed Deployment is Ready. A missing or stuck rollout leaves that previous stack serving.
// Copied ConfigMaps and Secrets keep the {namespace}-{cr-name}-{source} name.
func (r *NemoGuardrailsReconciler) retireUnscopedNames(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) error {
	if targetNamespace == "" || targetNamespace == nemoGuardrails.Namespace {
		return nil
	}
	identity := newWorkloadIdentity(nemoGuardrails, targetNamespace)
	unscoped := nemoGuardrails.Name
	if unscoped == "" || unscoped == identity.Name {
		return nil
	}
	ready, err := utils.CheckDeploymentReady(ctx, r.Client, identity.Name, targetNamespace)
	if err != nil {
		// A stuck rollout must not remove the stack that is still serving.
		if stderrors.Is(err, utils.ErrDeploymentProgressDeadlineExceeded) {
			return nil
		}
		return err
	}
	if !ready {
		return nil
	}
	current := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: identity.Name, Namespace: targetNamespace}, current); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !ownedBy(current, nemoGuardrails) {
		return nil
	}

	legacy := []client.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: unscoped, Namespace: targetNamespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: unscoped, Namespace: targetNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: unscoped + "-ca-bundle", Namespace: targetNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: unscoped + "-rbac-proxy-config", Namespace: targetNamespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: unscoped + "-rbac-proxy-admin-config", Namespace: targetNamespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: unscoped + "-serviceaccount", Namespace: targetNamespace}},
		&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: unscoped, Namespace: targetNamespace}},
		// The admin proxy ConfigMap used to be created in the custom resource namespace.
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: unscoped + "-rbac-proxy-admin-config", Namespace: nemoGuardrails.Namespace}},
	}
	for _, configName := range legacyCopiedConfigMapNames(nemoGuardrails, targetNamespace) {
		legacy = append(legacy, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configName, Namespace: targetNamespace}})
	}
	for _, secretName := range legacySecretNames(nemoGuardrails, targetNamespace) {
		legacy = append(legacy, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: targetNamespace}})
	}
	for _, obj := range legacy {
		if err := r.deleteNamedIfOwned(ctx, nemoGuardrails, obj); err != nil {
			return err
		}
	}
	return nil
}

// legacyCopiedConfigMapNames lists ConfigMap names used before copies were prefixed.
func legacyCopiedConfigMapNames(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) []string {
	var names []string
	seen := map[string]struct{}{}
	add := func(legacyName, currentName string) {
		if legacyName == "" || legacyName == currentName {
			return
		}
		if _, ok := seen[legacyName]; ok {
			return
		}
		seen[legacyName] = struct{}{}
		names = append(names, legacyName)
	}
	for _, nemoConfig := range nemoGuardrails.Spec.NemoConfigs {
		for _, configCM := range nemoConfig.ConfigMaps {
			if strings.HasPrefix(configCM, nemoGuardrailsDefaultConfigPrefix) {
				current := defaultConfigCopyName(nemoGuardrails.Namespace, nemoGuardrails.Name, configCM, targetNamespace)
				add(nemoGuardrails.Name+"-"+configCM, current)
				add(dns1123Name(nemoGuardrails.Namespace+"-"+nemoGuardrails.Name+"-"+configCM), current)
			}
			add(configCM, copiedObjectName(nemoGuardrails.Namespace, nemoGuardrails.Name, configCM, targetNamespace))
		}
	}
	if ca := nemoGuardrails.Spec.CABundleConfig; ca != nil {
		add(ca.ConfigMapName, copiedObjectName(nemoGuardrails.Namespace, nemoGuardrails.Name, ca.ConfigMapName, targetNamespace))
	}
	return names
}

// legacySecretNames lists Secret names used before copies were prefixed.
func legacySecretNames(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) []string {
	var names []string
	seen := map[string]struct{}{}
	for _, envVar := range nemoGuardrails.Spec.Env {
		if envVar.ValueFrom == nil || envVar.ValueFrom.SecretKeyRef == nil {
			continue
		}
		secretName := envVar.ValueFrom.SecretKeyRef.Name
		if secretName == "" || secretName == copiedObjectName(nemoGuardrails.Namespace, nemoGuardrails.Name, secretName, targetNamespace) {
			continue
		}
		if _, ok := seen[secretName]; ok {
			continue
		}
		seen[secretName] = struct{}{}
		names = append(names, secretName)
	}
	return names
}

func (r *NemoGuardrailsReconciler) deleteNamedIfOwned(ctx context.Context, owner client.Object, obj client.Object) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !ownedBy(obj, owner) {
		return nil
	}
	if err := r.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

// isWorkloadNamespaceReady returns true when namespace exists and carries the
// opt-in label. An API error is surfaced so the caller can requeue,
// but a missing namespace or missing label simply returns false.
func (r *NemoGuardrailsReconciler) isWorkloadNamespaceReady(ctx context.Context, namespace string) (bool, error) {
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return ns.Labels[WorkloadNamespaceLabel] == WorkloadNamespaceLabelValue, nil
}

// requireOwnedOrAbsent returns an error when obj already exists and this custom resource does not own it.
// The existing object is left unchanged.
func (r *NemoGuardrailsReconciler) requireOwnedOrAbsent(ctx context.Context, owner client.Object, kind string, obj client.Object) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if ownedBy(obj, owner) {
		return nil
	}
	return fmt.Errorf("%s %s/%s already exists and is not owned by NemoGuardrails %s/%s", kind, obj.GetNamespace(), obj.GetName(), owner.GetNamespace(), owner.GetName())
}

func deleteOwned(ctx context.Context, c client.Client, namespace string, owner client.Object, list client.ObjectList, items func(client.ObjectList) []client.Object) error {
	if owner.GetUID() == "" {
		return nil
	}
	if err := c.List(ctx, list, client.InNamespace(namespace), client.MatchingLabels{utils.OwnerUIDLabel: string(owner.GetUID())}); err != nil {
		return err
	}
	for _, obj := range items(list) {
		if !ownedBy(obj, owner) {
			continue
		}
		if err := c.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// ensureReferencedSecretsInNamespace copies any Secrets referenced by secretKeyRef
// in spec.env to the workload namespace so that pods there can resolve them.
func (r *NemoGuardrailsReconciler) ensureReferencedSecretsInNamespace(ctx context.Context, owner *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) error {
	if targetNamespace == owner.Namespace {
		return nil
	}
	seen := map[string]bool{}
	for _, envVar := range owner.Spec.Env {
		if envVar.ValueFrom == nil || envVar.ValueFrom.SecretKeyRef == nil {
			continue
		}
		ref := envVar.ValueFrom.SecretKeyRef
		secretName := ref.Name
		if secretName == "" {
			continue
		}
		if seen[secretName] {
			continue
		}

		source := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: owner.Namespace}, source); err != nil {
			if errors.IsNotFound(err) && ref.Optional != nil && *ref.Optional {
				continue
			}
			return fmt.Errorf("secret %s/%s referenced by spec.env: %w", owner.Namespace, secretName, err)
		}
		seen[secretName] = true
		if _, err := r.ensureSecretInNamespace(ctx, owner, source, targetNamespace); err != nil {
			return err
		}
	}
	return nil
}

// ensureSecretInNamespace copies source into namespace so a pod there can reference it.
func (r *NemoGuardrailsReconciler) ensureSecretInNamespace(ctx context.Context, owner client.Object, source *corev1.Secret, namespace string) (*corev1.Secret, error) {
	if source.Namespace == namespace {
		return source, nil
	}
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        copiedObjectName(owner.GetNamespace(), owner.GetName(), source.Name, namespace),
			Namespace:   namespace,
			Labels:      copyStringMap(source.Labels),
			Annotations: copyStringMap(source.Annotations),
		},
		Data: source.Data,
		Type: source.Type,
	}
	if err := utils.SetOwnerReference(owner, desired, r.Scheme); err != nil {
		return nil, err
	}
	existing := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: namespace}, existing); err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		if err := r.Create(ctx, desired); err != nil {
			return nil, err
		}
		return desired, nil
	}
	if !ownedBy(existing, owner) {
		return nil, fmt.Errorf("secret %s/%s already exists and is not owned by NemoGuardrails %s/%s", namespace, desired.Name, owner.GetNamespace(), owner.GetName())
	}
	if secretMatches(existing, desired) {
		return existing, nil
	}
	existing.Data = desired.Data
	existing.Type = desired.Type
	existing.Labels = mergeStringMap(existing.Labels, desired.Labels)
	existing.Annotations = mergeStringMap(existing.Annotations, desired.Annotations)
	if err := r.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func secretMatches(existing, desired *corev1.Secret) bool {
	if existing.Type != desired.Type || !equality.Semantic.DeepEqual(existing.Data, desired.Data) {
		return false
	}
	return stringMapIncludes(existing.Labels, desired.Labels) && stringMapIncludes(existing.Annotations, desired.Annotations)
}

func stringMapIncludes(current, required map[string]string) bool {
	for key, value := range required {
		if current[key] != value {
			return false
		}
	}
	return true
}

func mergeStringMap(current, required map[string]string) map[string]string {
	if len(required) == 0 {
		return current
	}
	if current == nil {
		current = map[string]string{}
	}
	for key, value := range required {
		current[key] = value
	}
	return current
}

// ensureConfigMapInNamespace copies source into namespace so a pod in that namespace can mount it.
func (r *NemoGuardrailsReconciler) ensureConfigMapInNamespace(ctx context.Context, owner client.Object, source *corev1.ConfigMap, namespace string) (*corev1.ConfigMap, error) {
	if source.Namespace == namespace {
		return source, nil
	}
	copyName := copiedObjectName(owner.GetNamespace(), owner.GetName(), source.Name, namespace)
	if strings.HasPrefix(source.Name, nemoGuardrailsDefaultConfigPrefix) {
		copyName = defaultConfigCopyName(owner.GetNamespace(), owner.GetName(), source.Name, namespace)
	}
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        copyName,
			Namespace:   namespace,
			Labels:      copyStringMap(source.Labels),
			Annotations: copyStringMap(source.Annotations),
		},
		Data: source.Data,
	}
	return r.createOrUpdateOwnedConfigMap(ctx, owner, desired)
}

// createOrUpdateOwnedConfigMap creates desired or updates it when this custom resource already owns it.
// An existing ConfigMap with the same name that is not owned is left unchanged.
func (r *NemoGuardrailsReconciler) createOrUpdateOwnedConfigMap(ctx context.Context, owner client.Object, desired *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	if err := utils.SetOwnerReference(owner, desired, r.Scheme); err != nil {
		return nil, err
	}
	copied, created, err := utils.ReconcileManuallyDefinedConfigMap(ctx, r.Client, owner, desired)
	if err != nil {
		return nil, err
	}
	if !created && !ownedBy(copied, owner) {
		return nil, fmt.Errorf("configmap %s/%s already exists and is not owned by NemoGuardrails %s/%s", desired.Namespace, desired.Name, owner.GetNamespace(), owner.GetName())
	}
	if !created {
		desired.Labels = mergeStringMap(copyStringMap(copied.Labels), desired.Labels)
		desired.Annotations = mergeStringMap(copyStringMap(copied.Annotations), desired.Annotations)
		if err := utils.CompareAndUpdateConfigmap(ctx, r.Client, copied, desired, true); err != nil {
			return nil, err
		}
	}
	return copied, nil
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func rewriteSecretRefs(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) []corev1.EnvVar {
	env := make([]corev1.EnvVar, len(nemoGuardrails.Spec.Env))
	for i, variable := range nemoGuardrails.Spec.Env {
		env[i] = *variable.DeepCopy()
		if env[i].ValueFrom == nil || env[i].ValueFrom.SecretKeyRef == nil {
			continue
		}
		env[i].ValueFrom.SecretKeyRef.Name = copiedObjectName(nemoGuardrails.Namespace, nemoGuardrails.Name, env[i].ValueFrom.SecretKeyRef.Name, targetNamespace)
	}
	return env
}

func ownedBy(obj metav1.Object, owner client.Object) bool {
	return utils.IsOwnedBy(obj, owner)
}

// deleteExposedRoute removes an owned Route from each namespace where this custom resource may have published one.
func (r *NemoGuardrailsReconciler) deleteExposedRoute(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, namespaces ...string) error {
	seen := map[string]struct{}{}
	for _, namespace := range namespaces {
		if namespace == "" {
			continue
		}
		if _, ok := seen[namespace]; ok {
			continue
		}
		seen[namespace] = struct{}{}
		names := []string{nemoGuardrails.Name}
		if prefixed := prefixedWorkloadObjectName(nemoGuardrails.Namespace, nemoGuardrails.Name); prefixed != nemoGuardrails.Name {
			names = append(names, prefixed)
		}
		for _, name := range names {
			if err := utils.DeleteRoute(ctx, r.Client, nemoGuardrails, name, namespace); err != nil {
				return err
			}
		}
	}
	return nil
}
