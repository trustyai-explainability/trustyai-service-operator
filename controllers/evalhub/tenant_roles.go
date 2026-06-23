package evalhub

import (
	"context"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	tenantAdminRoleName    = "evalhub-tenant-admin"
	tenantUserRoleName     = "evalhub-user"
	tenantAdminBindingName = "evalhub-tenant-admin-binding"
)

func tenantAdminRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"evalhubs"},
			Verbs:     []string{"get"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"evalhubs/proxy"},
			Verbs:     []string{"get", "create"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"providers"},
			Verbs:     []string{"get", "list"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"collections"},
			Verbs:     []string{"get", "list", "create", "update", "patch", "delete"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"status-events"},
			Verbs:     []string{"create"},
		},
		{
			APIGroups: []string{"mlflow.kubeflow.org"},
			Resources: []string{"experiments"},
			Verbs:     []string{"get", "list"},
		},
	}
}

func tenantUserRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"evalhubs"},
			Verbs:     []string{"get"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"evalhubs/proxy"},
			Verbs:     []string{"get", "create"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"providers"},
			Verbs:     []string{"get", "list"},
		},
		{
			APIGroups: []string{"trustyai.opendatahub.io"},
			Resources: []string{"collections"},
			Verbs:     []string{"get", "list"},
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

func tenantRoleLabels(instance *evalhubv1.EvalHub, name string) map[string]string {
	return map[string]string{
		"app":                          "eval-hub",
		"app.kubernetes.io/name":       name,
		"app.kubernetes.io/instance":   instance.Name,
		"app.kubernetes.io/part-of":    "eval-hub",
		"app.kubernetes.io/version":    constants.Version,
		"app.kubernetes.io/component":  "tenancy",
		"app.kubernetes.io/managed-by": "trustyai-service-operator",
	}
}

// reconcileSingleTenancyRoles creates or cleans up namespace-scoped Roles and
// RoleBindings for self-service single-tenant EvalHub deployments.
// In single mode it provisions evalhub-tenant-admin and evalhub-user Roles.
// In multi mode it removes any leftover tenant Roles from a previous config.
func (r *EvalHubReconciler) reconcileSingleTenancyRoles(ctx context.Context, instance *evalhubv1.EvalHub) error {
	if !instance.Spec.IsSingleTenancy() {
		return r.cleanupSingleTenancyRoles(ctx, instance)
	}

	if err := r.createTenantRole(ctx, instance, tenantAdminRoleName, tenantAdminRules()); err != nil {
		return err
	}
	if err := r.createTenantRole(ctx, instance, tenantUserRoleName, tenantUserRules()); err != nil {
		return err
	}
	return r.createTenantAdminBinding(ctx, instance)
}

func (r *EvalHubReconciler) createTenantRole(ctx context.Context, instance *evalhubv1.EvalHub, name string, rules []rbacv1.PolicyRule) error {
	log := log.FromContext(ctx)

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
			Labels:    tenantRoleLabels(instance, name),
		},
		Rules: rules,
	}

	if err := ctrl.SetControllerReference(instance, role, r.Scheme); err != nil {
		return err
	}

	found := &rbacv1.Role{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: instance.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating tenant Role", "Name", name)
		return r.Create(ctx, role)
	} else if err != nil {
		return err
	}

	if !equalPolicyRules(found.Rules, role.Rules) {
		found.Rules = role.Rules
		log.Info("Updating tenant Role rules", "Name", name)
		return r.Update(ctx, found)
	}
	return nil
}

func (r *EvalHubReconciler) createTenantAdminBinding(ctx context.Context, instance *evalhubv1.EvalHub) error {
	log := log.FromContext(ctx)

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenantAdminBindingName,
			Namespace: instance.Namespace,
			Labels:    tenantRoleLabels(instance, tenantAdminBindingName),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "Group",
				Name:     "system:serviceaccounts:" + instance.Namespace,
				APIGroup: rbacv1.GroupName,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     tenantAdminRoleName,
			APIGroup: rbacv1.GroupName,
		},
	}

	if err := ctrl.SetControllerReference(instance, rb, r.Scheme); err != nil {
		return err
	}

	found := &rbacv1.RoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating tenant admin RoleBinding", "Name", rb.Name)
		return r.Create(ctx, rb)
	} else if err != nil {
		return err
	}

	if !equalRoleBindingRoleRef(found.RoleRef, rb.RoleRef) {
		log.Info("RoleRef differs, deleting and recreating tenant admin RoleBinding", "Name", rb.Name)
		if err := r.Delete(ctx, found); err != nil {
			return err
		}
		return r.Create(ctx, rb)
	}
	if !equalRoleBindingSubjects(found.Subjects, rb.Subjects) {
		found.Subjects = rb.Subjects
		log.Info("Updating tenant admin RoleBinding subjects", "Name", rb.Name)
		return r.Update(ctx, found)
	}
	return nil
}

func (r *EvalHubReconciler) cleanupSingleTenancyRoles(ctx context.Context, instance *evalhubv1.EvalHub) error {
	log := log.FromContext(ctx)
	ns := instance.Namespace

	rb := &rbacv1.RoleBinding{}
	if err := r.Get(ctx, types.NamespacedName{Name: tenantAdminBindingName, Namespace: ns}, rb); err == nil {
		log.Info("Removing tenant admin RoleBinding", "Name", tenantAdminBindingName)
		if err := r.Delete(ctx, rb); err != nil && !errors.IsNotFound(err) {
			return err
		}
	} else if !errors.IsNotFound(err) {
		return err
	}

	for _, name := range []string{tenantAdminRoleName, tenantUserRoleName} {
		role := &rbacv1.Role{}
		if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, role); err == nil {
			log.Info("Removing tenant Role", "Name", name)
			if err := r.Delete(ctx, role); err != nil && !errors.IsNotFound(err) {
				return err
			}
		} else if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
