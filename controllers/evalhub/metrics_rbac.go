package evalhub

import (
	"context"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const metricsRoleName = "evalhub-metrics-manager"

func metricsRoleBindingName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-metrics-rb"
}

func (r *EvalHubReconciler) buildMetricsRole(instance *evalhubv1alpha1.EvalHub) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metricsRoleName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     metricsRoleName,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  "eval-hub",
				"app.kubernetes.io/version":  constants.Version,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"monitoring.coreos.com"},
				Resources: []string{"servicemonitors"},
				Verbs:     []string{"get", "list", "watch", "create", "update"},
			},
			{
				APIGroups: []string{"networking.k8s.io"},
				Resources: []string{"networkpolicies"},
				Verbs:     []string{"get", "list", "watch", "create", "update"},
			},
		},
	}
}

func (r *EvalHubReconciler) buildMetricsRoleBinding(instance *evalhubv1alpha1.EvalHub) *rbacv1.RoleBinding {
	saName := generateServiceAccountName(instance)
	rbName := metricsRoleBindingName(instance)

	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rbName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     rbName,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  "eval-hub",
				"app.kubernetes.io/version":  constants.Version,
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: instance.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     metricsRoleName,
		},
	}
}

func (r *EvalHubReconciler) reconcileMetricsRBAC(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)

	// Reconcile Role
	desiredRole := r.buildMetricsRole(instance)
	if err := ctrl.SetControllerReference(instance, desiredRole, r.Scheme); err != nil {
		return err
	}

	foundRole := &rbacv1.Role{}
	err := r.Get(ctx, types.NamespacedName{Name: desiredRole.Name, Namespace: desiredRole.Namespace}, foundRole)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating metrics Role", "Namespace", desiredRole.Namespace, "Name", desiredRole.Name)
			if err := r.Create(ctx, desiredRole); err != nil {
				return err
			}
		} else {
			return err
		}
	} else if !equalPolicyRules(foundRole.Rules, desiredRole.Rules) {
		foundRole.Rules = desiredRole.Rules
		log.Info("Updating metrics Role rules", "Name", desiredRole.Name)
		if err := r.Update(ctx, foundRole); err != nil {
			return err
		}
	}

	// Reconcile RoleBinding
	desiredRB := r.buildMetricsRoleBinding(instance)
	if err := ctrl.SetControllerReference(instance, desiredRB, r.Scheme); err != nil {
		return err
	}

	foundRB := &rbacv1.RoleBinding{}
	err = r.Get(ctx, types.NamespacedName{Name: desiredRB.Name, Namespace: desiredRB.Namespace}, foundRB)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating metrics RoleBinding", "Namespace", desiredRB.Namespace, "Name", desiredRB.Name)
			return r.Create(ctx, desiredRB)
		}
		return err
	}

	if !equalRoleBindingSubjects(foundRB.Subjects, desiredRB.Subjects) {
		foundRB.Subjects = desiredRB.Subjects
		log.Info("Updating metrics RoleBinding subjects", "Name", desiredRB.Name)
		return r.Update(ctx, foundRB)
	}

	if !equalRoleBindingRoleRef(foundRB.RoleRef, desiredRB.RoleRef) {
		log.Info("RoleRef differs, deleting and recreating metrics RoleBinding", "Name", desiredRB.Name)
		if err := r.Delete(ctx, foundRB); err != nil {
			return err
		}
		return r.Create(ctx, desiredRB)
	}

	return nil
}
