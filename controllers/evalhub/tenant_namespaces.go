package evalhub

import (
	"context"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	}

	return nil
}
