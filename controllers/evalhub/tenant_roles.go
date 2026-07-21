package evalhub

import (
	"context"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// reconcileSingleTenancyRoles creates convenience RBAC Roles in single mode and cleans them
// up when the instance switches back to multi mode.
func (r *EvalHubReconciler) reconcileSingleTenancyRoles(ctx context.Context, instance *evalhubv1.EvalHub) error {
	if instance.Spec.IsSingleTenancy() {
		return r.ensureSingleTenancyRoles(ctx, instance)
	}
	return r.cleanupSingleTenancyRoles(ctx, instance)
}

// ensureSingleTenancyRoles creates or updates the evalhub-tenant-admin and evalhub-user Roles
// plus the admin RoleBinding in the instance namespace.
func (r *EvalHubReconciler) ensureSingleTenancyRoles(ctx context.Context, instance *evalhubv1.EvalHub) error {
	if err := r.ensureRole(ctx, instance, tenantAdminRoleName, tenantAdminRules(instance)); err != nil {
		return err
	}
	if err := r.ensureRole(ctx, instance, tenantUserRoleName, tenantUserRules(instance)); err != nil {
		return err
	}
	return r.ensureTenantAdminBinding(ctx, instance)
}

// ensureRole creates or updates a namespaced Role in the instance namespace, setting the
// EvalHub CR as owner so it is garbage collected when the CR is deleted.
func (r *EvalHubReconciler) ensureRole(ctx context.Context, instance *evalhubv1.EvalHub, name string, rules []rbacv1.PolicyRule) error {
	log := log.FromContext(ctx)
	desired := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
		},
		Rules: rules,
	}
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	existing := &rbacv1.Role{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: instance.Namespace}, existing)
	if errors.IsNotFound(err) {
		log.Info("Creating single-tenancy Role", "name", name)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.Rules = rules
	return r.Update(ctx, existing)
}

// ensureTenantAdminBinding creates or updates a RoleBinding that binds the evalhub-tenant-admin
// Role to the OpenShift built-in "admin" ClusterRole subjects in the instance namespace.
// This lets existing namespace admins manage EvalHub resources without extra RBAC grants.
func (r *EvalHubReconciler) ensureTenantAdminBinding(ctx context.Context, instance *evalhubv1.EvalHub) error {
	log := log.FromContext(ctx)
	desired := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenantAdminBindingName,
			Namespace: instance.Namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     tenantAdminRoleName,
		},
		Subjects: []rbacv1.Subject{
			{
				APIGroup: rbacv1.GroupName,
				Kind:     "Group",
				Name:     "system:serviceaccounts:" + instance.Namespace,
			},
		},
	}
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	existing := &rbacv1.RoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: tenantAdminBindingName, Namespace: instance.Namespace}, existing)
	if errors.IsNotFound(err) {
		log.Info("Creating single-tenancy admin RoleBinding")
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.RoleRef = desired.RoleRef
	existing.Subjects = desired.Subjects
	return r.Update(ctx, existing)
}

// cleanupSingleTenancyRoles deletes the single-tenancy Roles and RoleBinding if they exist.
// Called when the instance is in multi mode (handles single→multi mode switch).
func (r *EvalHubReconciler) cleanupSingleTenancyRoles(ctx context.Context, instance *evalhubv1.EvalHub) error {
	log := log.FromContext(ctx)
	ns := instance.Namespace

	rb := &rbacv1.RoleBinding{}
	if err := r.Get(ctx, types.NamespacedName{Name: tenantAdminBindingName, Namespace: ns}, rb); err == nil {
		log.Info("Removing stale single-tenancy RoleBinding")
		if err := r.Delete(ctx, rb); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	for _, roleName := range []string{tenantAdminRoleName, tenantUserRoleName} {
		role := &rbacv1.Role{}
		if err := r.Get(ctx, types.NamespacedName{Name: roleName, Namespace: ns}, role); err == nil {
			log.Info("Removing stale single-tenancy Role", "name", roleName)
			if err := r.Delete(ctx, role); err != nil && !errors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

// tenantAdminRules returns the PolicyRules for the evalhub-tenant-admin Role.
// Full control over EvalHub resources; read on MLflow experiments.
func tenantAdminRules(instance *evalhubv1.EvalHub) []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"evalhubs"},
			Verbs:     []string{"get", "list"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"health"},
			Verbs:     []string{"get"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"evaluations", "collections", "providers", "status-events"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
		},
		{
			APIGroups:     []string{"trustyai.opendatahub.io"},
			Resources:     []string{"evalhubs/proxy"},
			ResourceNames: []string{instance.Name},
			Verbs:         []string{"get", "create"},
		},
		{
			APIGroups: []string{"mlflow.kubeflow.org"},
			Resources: []string{"experiments"},
			Verbs:     []string{"get", "list"},
		},
	}
}

// tenantUserRules returns the PolicyRules for the evalhub-user Role.
// Read on collections/providers, submit evaluations, read MLflow experiments.
func tenantUserRules(instance *evalhubv1.EvalHub) []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"evalhubs"},
			Verbs:     []string{"get", "list"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"health"},
			Verbs:     []string{"get"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"collections", "providers"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups:     []string{"trustyai.opendatahub.io"},
			Resources:     []string{"evalhubs/proxy"},
			ResourceNames: []string{instance.Name},
			Verbs:         []string{"get", "create"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"status-events"},
			Verbs:     []string{"create"},
		},
		{
			APIGroups: []string{"mlflow.kubeflow.org"},
			Resources: []string{"experiments"},
			Verbs:     []string{"get", "list", "create"},
		},
	}
}
