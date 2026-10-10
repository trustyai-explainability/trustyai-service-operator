package nemo_guardrails

import (
	"context"
	"fmt"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

// workloadNamespace is the namespace where this reconciler's Deployment, Service, and Route live.
func (r *NemoGuardrailsReconciler) workloadNamespace(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails) string {
	return deriveInfraNamespace(r.Namespace, nemoGuardrails.Namespace)
}

// deployedNamespace is where resources were last created. Deletion uses it so a
// change in the controller's own namespace still removes objects from the previous namespace.
func (r *NemoGuardrailsReconciler) deployedNamespace(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails) string {
	if nemoGuardrails.Status.WorkloadNamespace != "" {
		return nemoGuardrails.Status.WorkloadNamespace
	}
	return r.workloadNamespace(nemoGuardrails)
}

// rejectUnownedWorkload reports a name collision in namespace before any object there is created or any previous workload is deleted.
func (r *NemoGuardrailsReconciler) rejectUnownedWorkload(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, namespace string) error {
	name := nemoGuardrails.Name
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
			}{kind: "ConfigMap", obj: &corev1.ConfigMap{ObjectMeta: meta(GetRBACConfigName(*nemoGuardrails))}},
			struct {
				kind string
				obj  client.Object
			}{kind: "ServiceAccount", obj: &corev1.ServiceAccount{ObjectMeta: meta(utils.GetServiceAccountName(nemoGuardrails))}},
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
	return nil
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
				if err == nil {
					names = append(names, fmt.Sprintf("%s-%s", nemoGuardrails.Name, source.Name))
					continue
				}
				if !errors.IsNotFound(err) {
					return nil, err
				}
			}
			if nemoGuardrails.Namespace != namespace {
				names = append(names, configCM)
			}
		}
	}
	if ca := nemoGuardrails.Spec.CABundleConfig; ca != nil && ca.ConfigMapName != "" {
		sourceNamespace := ca.ConfigMapNamespace
		if sourceNamespace == "" {
			sourceNamespace = nemoGuardrails.Namespace
		}
		if sourceNamespace != namespace {
			source := &corev1.ConfigMap{}
			err := r.Get(ctx, types.NamespacedName{Name: ca.ConfigMapName, Namespace: sourceNamespace}, source)
			if err == nil && source.Namespace != namespace {
				names = append(names, source.Name)
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

// releasePreviousWorkload deletes objects in the previous namespace only after the target namespace has no unowned name collisions.
func (r *NemoGuardrailsReconciler) releasePreviousWorkload(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails, targetNamespace string) error {
	if err := r.rejectUnownedWorkload(ctx, nemoGuardrails, targetNamespace); err != nil {
		return err
	}
	if previous := nemoGuardrails.Status.WorkloadNamespace; previous != "" && previous != targetNamespace {
		return r.deleteOwnedInNamespace(ctx, nemoGuardrails, previous)
	}
	return nil
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
	// Objects created before the owner label existed are still selected by name.
	for _, obj := range []client.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: owner.GetName(), Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: owner.GetName(), Namespace: namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: owner.GetName() + "-ca-bundle", Namespace: namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: owner.GetName() + "-rbac-proxy-config", Namespace: namespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: owner.GetName() + "-serviceaccount", Namespace: namespace}},
		&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: owner.GetName(), Namespace: namespace}},
	} {
		if err := r.deleteNamedIfOwned(ctx, owner, obj); err != nil {
			return err
		}
	}
	return nil
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

func (r *NemoGuardrailsReconciler) validateWorkloadNamespace(ctx context.Context, namespace string) error {
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("namespace %s does not exist", namespace)
		}
		return err
	}
	if ns.Labels[WorkloadNamespaceLabel] != WorkloadNamespaceLabelValue {
		return fmt.Errorf("namespace %s must be labeled %s=%s before it can run NeMo Guardrails for a custom resource in another namespace", namespace, WorkloadNamespaceLabel, WorkloadNamespaceLabelValue)
	}
	return nil
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

// ensureConfigMapInNamespace copies source into namespace so a pod in that namespace can mount it.
func (r *NemoGuardrailsReconciler) ensureConfigMapInNamespace(ctx context.Context, owner client.Object, source *corev1.ConfigMap, namespace string) (*corev1.ConfigMap, error) {
	if source.Namespace == namespace {
		return source, nil
	}
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        source.Name,
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

func ownedBy(obj metav1.Object, owner client.Object) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			return true
		}
	}
	return obj.GetLabels()[utils.OwnerUIDLabel] == string(owner.GetUID()) && owner.GetUID() != ""
}
