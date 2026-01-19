package evalhub

import (
	"context"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func generateServiceAccountName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-proxy"
}

// createServiceAccount creates a service account for this instance's kube-rbac-proxy
func (r *EvalHubReconciler) createServiceAccount(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	serviceAccountName := generateServiceAccountName(instance)

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     serviceAccountName,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  "eval-hub",
				"app.kubernetes.io/version":  constants.Version,
			},
		},
	}

	// Set instance as the owner and controller
	if err := ctrl.SetControllerReference(instance, sa, r.Scheme); err != nil {
		return err
	}

	// Check if this ServiceAccount already exists
	found := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating a new ServiceAccount", "Namespace", sa.Namespace, "Name", sa.Name)
		err = r.Create(ctx, sa)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	err = r.createClusterRoleBinding(ctx, instance, serviceAccountName)
	if err != nil {
		return err
	}

	return nil
}

// createClusterRoleBinding creates a binding between the service account and evalhub proxy cluster role
func (r *EvalHubReconciler) createClusterRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	log := log.FromContext(ctx)

	clusterRoleBindingName := instance.Name + "-" + instance.Namespace + "-proxy-rolebinding"
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleBindingName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: instance.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "trustyai-service-operator-evalhub-proxy-role",
			APIGroup: rbacv1.GroupName,
		},
	}

	// Check if ClusterRoleBinding already exists
	found := &rbacv1.ClusterRoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: clusterRoleBindingName}, found)
	if err != nil && errors.IsNotFound(err) {
		// ClusterRoleBinding doesn't exist, create it
		// Note: We don't set owner references because ClusterRoleBindings cannot have namespace-scoped owners
		log.Info("Creating ClusterRoleBinding", "name", clusterRoleBindingName)
		return r.Create(ctx, clusterRoleBinding)
	} else if err != nil {
		// Error getting ClusterRoleBinding
		return err
	}

	// ClusterRoleBinding already exists, check if it needs updating
	subjectsEqual := equalSubjects(found, clusterRoleBinding)
	roleRefEqual := equalRoleRef(found, clusterRoleBinding)

	if !subjectsEqual || !roleRefEqual {
		if roleRefEqual && !subjectsEqual {
			// Only subjects differ, we can update them
			found.Subjects = clusterRoleBinding.Subjects
			log.Info("Updating ClusterRoleBinding subjects", "name", clusterRoleBindingName)
			return r.Update(ctx, found)
		} else if !roleRefEqual {
			// RoleRef differs, we need to delete and recreate as RoleRef is immutable
			log.Info("RoleRef differs, deleting and recreating ClusterRoleBinding", "name", clusterRoleBindingName)

			// Delete existing ClusterRoleBinding
			if err := r.Delete(ctx, found); err != nil {
				return err
			}

			// Create new ClusterRoleBinding with desired spec
			log.Info("Creating new ClusterRoleBinding", "name", clusterRoleBindingName)
			return r.Create(ctx, clusterRoleBinding)
		}
	}

	return nil
}

// equalClusterRoleBindingSpec compares two ClusterRoleBinding specs
func equalClusterRoleBindingSpec(existing, desired *rbacv1.ClusterRoleBinding) bool {
	// Compare subjects
	if len(existing.Subjects) != len(desired.Subjects) {
		return false
	}
	for i, subject := range existing.Subjects {
		if i >= len(desired.Subjects) ||
			subject.Kind != desired.Subjects[i].Kind ||
			subject.Name != desired.Subjects[i].Name ||
			subject.Namespace != desired.Subjects[i].Namespace {
			return false
		}
	}

	// Compare role reference
	return existing.RoleRef.Kind == desired.RoleRef.Kind &&
		existing.RoleRef.Name == desired.RoleRef.Name &&
		existing.RoleRef.APIGroup == desired.RoleRef.APIGroup
}

// equalSubjects compares subjects between two ClusterRoleBindings
func equalSubjects(existing, desired *rbacv1.ClusterRoleBinding) bool {
	if len(existing.Subjects) != len(desired.Subjects) {
		return false
	}
	for i, subject := range existing.Subjects {
		if i >= len(desired.Subjects) ||
			subject.Kind != desired.Subjects[i].Kind ||
			subject.Name != desired.Subjects[i].Name ||
			subject.Namespace != desired.Subjects[i].Namespace {
			return false
		}
	}
	return true
}

// equalRoleRef compares role reference between two ClusterRoleBindings
func equalRoleRef(existing, desired *rbacv1.ClusterRoleBinding) bool {
	return existing.RoleRef.Kind == desired.RoleRef.Kind &&
		existing.RoleRef.Name == desired.RoleRef.Name &&
		existing.RoleRef.APIGroup == desired.RoleRef.APIGroup
}
