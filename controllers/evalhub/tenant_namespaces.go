package evalhub

import (
	"context"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// tenantLabel is the label that marks a namespace as an EvalHub tenant namespace.
	// When the operator detects a namespace with this label, it automatically provisions
	// the job ServiceAccount and RBAC bindings required for evaluation jobs to run there.
	tenantLabel = "evalhub.trustyai.opendatahub.io/tenant"
)

// reconcileTenantNamespaces lists all namespaces with the tenant label and ensures
// the job ServiceAccount and API SA RoleBindings exist in each one.
func (r *EvalHubReconciler) reconcileTenantNamespaces(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)

	// List namespaces with the tenant label
	nsList := &corev1.NamespaceList{}
	if err := r.List(ctx, nsList, client.HasLabels{tenantLabel}); err != nil {
		return err
	}

	for i := range nsList.Items {
		ns := nsList.Items[i].Name

		// Skip the instance namespace (already handled by main reconcile)
		if ns == instance.Namespace {
			continue
		}

		log.Info("Reconciling tenant namespace", "namespace", ns)

		// Create job SA, job access Role/RoleBinding, and MLFlow RoleBinding
		if err := r.createJobsServiceAccount(ctx, instance, ns); err != nil {
			log.Error(err, "Failed to create job ServiceAccount in tenant namespace", "namespace", ns)
			return err
		}

		// Create jobs-writer RoleBinding for the API SA in the tenant namespace
		// so the EvalHub service can create Jobs there
		serviceAccountName := generateServiceAccountName(instance)
		jobWriterRBName := normalizeDNS1123LabelValue(instance.Name + "-" + ns + "-job-writer-rb")
		if err := r.createJobRoleBinding(ctx, instance, jobWriterRBName, serviceAccountName, ns, rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     jobsWriterClusterRoleName,
			APIGroup: rbacv1.GroupName,
		}); err != nil {
			log.Error(err, "Failed to create jobs-writer RoleBinding in tenant namespace", "namespace", ns)
			return err
		}

		// Create job-config RoleBinding for the API SA in the tenant namespace
		// so the EvalHub service can create ConfigMaps there
		jobConfigRBName := normalizeDNS1123LabelValue(instance.Name + "-" + ns + "-job-config-rb")
		if err := r.createJobRoleBinding(ctx, instance, jobConfigRBName, serviceAccountName, ns, rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     jobConfigClusterRoleName,
			APIGroup: rbacv1.GroupName,
		}); err != nil {
			log.Error(err, "Failed to create job-config RoleBinding in tenant namespace", "namespace", ns)
			return err
		}

		// Create service CA ConfigMap in the tenant namespace so job pods can
		// mount the cluster service CA for TLS callbacks to EvalHub.
		if err := r.createTenantServiceCAConfigMap(ctx, instance, ns); err != nil {
			log.Error(err, "Failed to create service CA ConfigMap in tenant namespace", "namespace", ns)
			return err
		}
	}

	return nil
}

// createTenantServiceCAConfigMap ensures a service CA ConfigMap exists in the
// tenant namespace. The ConfigMap is annotated so that OpenShift's service CA
// operator automatically injects the cluster CA bundle.
func (r *EvalHubReconciler) createTenantServiceCAConfigMap(ctx context.Context, instance *evalhubv1alpha1.EvalHub, namespace string) error {
	log := log.FromContext(ctx)
	cmName := instance.Name + "-service-ca"

	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: cmName, Namespace: namespace}, cm)
	if err == nil {
		// Already exists — ensure annotation is present
		if cm.Annotations == nil {
			cm.Annotations = make(map[string]string)
		}
		if cm.Annotations["service.beta.openshift.io/inject-cabundle"] != "true" {
			cm.Annotations["service.beta.openshift.io/inject-cabundle"] = "true"
			return r.Update(ctx, cm)
		}
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	// Create new ConfigMap with CA injection annotation and job resource labels
	cm = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: namespace,
			Labels:    jobResourceLabels(instance, cmName),
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle": "true",
			},
		},
		Data: map[string]string{},
	}
	log.Info("Creating service CA ConfigMap in tenant namespace", "namespace", namespace, "name", cmName)
	return r.Create(ctx, cm)
}
