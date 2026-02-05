package evalhub

import (
	"context"
	"sort"

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

	// Create RoleBinding for proxy ServiceAccount to the pre-created ClusterRole
	// This allows the evalhub service to create ConfigMaps, Jobs, and proxy to services
	err = r.createResourceManagementRoleBinding(ctx, instance, serviceAccountName)
	if err != nil {
		return err
	}

	// Create RoleBinding for jobs ServiceAccount to access evalhubs/proxy in this namespace
	jobsServiceAccountName := generateJobsServiceAccountName(instance)
	err = r.createJobsProxyRoleBinding(ctx, instance, jobsServiceAccountName)
	if err != nil {
		return err
	}

	// Create RoleBinding for jobs ServiceAccount to the pre-created ClusterRole
	// This allows jobs to create ConfigMaps and Jobs in this namespace
	err = r.createJobsResourceManagementRoleBinding(ctx, instance, jobsServiceAccountName)
	if err != nil {
		return err
	}

	return nil
}

// Pre-created ClusterRole name for EvalHub resource management
// This ClusterRole is installed with the operator and contains permissions for:
// - ConfigMaps (for job specs)
// - Jobs (for evaluation jobs)
// - services/proxy (for job callbacks)
const resourceManagerClusterRoleName = "trustyai-service-operator-evalhub-resource-manager"

// createResourceManagementRoleBinding creates a RoleBinding for the proxy ServiceAccount
// to the pre-created ClusterRole for resource management.
// Using a pre-created ClusterRole avoids the need for the operator to have broad
// permissions that would be required to dynamically create Roles with these permissions.
func (r *EvalHubReconciler) createResourceManagementRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	log := log.FromContext(ctx)

	roleBindingName := instance.Name + "-resource-manager"
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     roleBindingName,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  "eval-hub",
				"app.kubernetes.io/version":  constants.Version,
			},
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
			Name:     resourceManagerClusterRoleName,
			APIGroup: rbacv1.GroupName,
		},
	}

	// Set instance as the owner and controller
	if err := ctrl.SetControllerReference(instance, roleBinding, r.Scheme); err != nil {
		return err
	}

	// Check if this RoleBinding already exists
	found := &rbacv1.RoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: roleBinding.Name, Namespace: roleBinding.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating resource management RoleBinding", "Namespace", roleBinding.Namespace, "Name", roleBinding.Name)
		return r.Create(ctx, roleBinding)
	} else if err != nil {
		return err
	}

	// RoleBinding exists, check if it needs updating
	subjectsEqual := equalRoleBindingSubjects(found.Subjects, roleBinding.Subjects)
	roleRefEqual := equalRoleBindingRoleRef(found.RoleRef, roleBinding.RoleRef)

	if !subjectsEqual || !roleRefEqual {
		if roleRefEqual && !subjectsEqual {
			// Only subjects differ, we can update them
			found.Subjects = roleBinding.Subjects
			log.Info("Updating resource management RoleBinding subjects", "Name", roleBinding.Name)
			return r.Update(ctx, found)
		} else if !roleRefEqual {
			// RoleRef differs, we need to delete and recreate as RoleRef is immutable
			log.Info("RoleRef differs, deleting and recreating resource management RoleBinding", "Name", roleBinding.Name)

			// Delete existing RoleBinding
			if err := r.Delete(ctx, found); err != nil {
				return err
			}

			// Create new RoleBinding with desired spec
			log.Info("Creating new resource management RoleBinding", "Name", roleBinding.Name)
			return r.Create(ctx, roleBinding)
		}
	}

	return nil
}

// createJobsResourceManagementRoleBinding creates a RoleBinding for the jobs ServiceAccount
// to the pre-created ClusterRole for resource management.
func (r *EvalHubReconciler) createJobsResourceManagementRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	log := log.FromContext(ctx)

	roleBindingName := instance.Name + "-resource-manager-jobs"
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     roleBindingName,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  "eval-hub",
				"app.kubernetes.io/version":  constants.Version,
			},
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
			Name:     resourceManagerClusterRoleName,
			APIGroup: rbacv1.GroupName,
		},
	}

	// Set instance as the owner and controller
	if err := ctrl.SetControllerReference(instance, roleBinding, r.Scheme); err != nil {
		return err
	}

	// Check if this RoleBinding already exists
	found := &rbacv1.RoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: roleBinding.Name, Namespace: roleBinding.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating jobs resource management RoleBinding", "Namespace", roleBinding.Namespace, "Name", roleBinding.Name)
		return r.Create(ctx, roleBinding)
	} else if err != nil {
		return err
	}

	// RoleBinding exists, check if it needs updating
	subjectsEqual := equalRoleBindingSubjects(found.Subjects, roleBinding.Subjects)
	roleRefEqual := equalRoleBindingRoleRef(found.RoleRef, roleBinding.RoleRef)

	if !subjectsEqual || !roleRefEqual {
		if roleRefEqual && !subjectsEqual {
			// Only subjects differ, we can update them
			found.Subjects = roleBinding.Subjects
			log.Info("Updating jobs resource management RoleBinding subjects", "Name", roleBinding.Name)
			return r.Update(ctx, found)
		} else if !roleRefEqual {
			// RoleRef differs, we need to delete and recreate as RoleRef is immutable
			log.Info("RoleRef differs, deleting and recreating jobs resource management RoleBinding", "Name", roleBinding.Name)

			// Delete existing RoleBinding
			if err := r.Delete(ctx, found); err != nil {
				return err
			}

			// Create new RoleBinding with desired spec
			log.Info("Creating new jobs resource management RoleBinding", "Name", roleBinding.Name)
			return r.Create(ctx, roleBinding)
		}
	}

	return nil
}

// createJobsProxyRoleBinding creates a RoleBinding for jobs ServiceAccount to access evalhubs/proxy
// in the EvalHub namespace, preventing cross-namespace access.
func (r *EvalHubReconciler) createJobsProxyRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	log := log.FromContext(ctx)

	roleBindingName := instance.Name + "-" + instance.Namespace + "-jobs-proxy-rolebinding"
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     instance.Name + "-jobs-proxy",
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  "eval-hub",
				"app.kubernetes.io/version":  constants.Version,
			},
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
			Name:     "trustyai-service-operator-evalhub-jobs-proxy-role",
			APIGroup: rbacv1.GroupName,
		},
	}

	// Set instance as the owner and controller
	if err := ctrl.SetControllerReference(instance, roleBinding, r.Scheme); err != nil {
		return err
	}

	// Check if this RoleBinding already exists
	found := &rbacv1.RoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: roleBinding.Name, Namespace: roleBinding.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating jobs proxy RoleBinding", "Name", roleBinding.Name)
		return r.Create(ctx, roleBinding)
	} else if err != nil {
		return err
	}

	// RoleBinding exists, check if it needs updating
	subjectsEqual := equalRoleBindingSubjects(found.Subjects, roleBinding.Subjects)
	roleRefEqual := equalRoleBindingRoleRef(found.RoleRef, roleBinding.RoleRef)

	if !subjectsEqual || !roleRefEqual {
		if roleRefEqual && !subjectsEqual {
			// Only subjects differ, we can update them
			found.Subjects = roleBinding.Subjects
			log.Info("Updating jobs proxy RoleBinding subjects", "Name", roleBinding.Name)
			return r.Update(ctx, found)
		} else if !roleRefEqual {
			// RoleRef differs, we need to delete and recreate as RoleRef is immutable
			log.Info("RoleRef differs, deleting and recreating jobs proxy RoleBinding", "Name", roleBinding.Name)

			// Delete existing RoleBinding
			if err := r.Delete(ctx, found); err != nil {
				return err
			}

			// Create new RoleBinding with desired spec
			log.Info("Creating new jobs proxy RoleBinding", "Name", roleBinding.Name)
			return r.Create(ctx, roleBinding)
		}
	}

	return nil
}

// equalRoleBindingSubjects compares subjects between two RoleBindings in an order-insensitive way.
func equalRoleBindingSubjects(existing, desired []rbacv1.Subject) bool {
	if len(existing) != len(desired) {
		return false
	}

	// Make copies so we don't mutate the original slices
	existingCopy := append([]rbacv1.Subject(nil), existing...)
	desiredCopy := append([]rbacv1.Subject(nil), desired...)

	less := func(a, b rbacv1.Subject) bool {
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	}

	sort.Slice(existingCopy, func(i, j int) bool {
		return less(existingCopy[i], existingCopy[j])
	})
	sort.Slice(desiredCopy, func(i, j int) bool {
		return less(desiredCopy[i], desiredCopy[j])
	})

	for i := range existingCopy {
		if existingCopy[i].Kind != desiredCopy[i].Kind ||
			existingCopy[i].Name != desiredCopy[i].Name ||
			existingCopy[i].Namespace != desiredCopy[i].Namespace {
			return false
		}
	}

	return true
}

// equalRoleBindingRoleRef compares role reference between two RoleBindings
func equalRoleBindingRoleRef(existing, desired rbacv1.RoleRef) bool {
	return existing.Kind == desired.Kind &&
		existing.Name == desired.Name &&
		existing.APIGroup == desired.APIGroup
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

// equalSubjects compares subjects between two ClusterRoleBindings in an order-insensitive way.
func equalSubjects(existing, desired *rbacv1.ClusterRoleBinding) bool {
	if len(existing.Subjects) != len(desired.Subjects) {
		return false
	}

	// Make copies so we don't mutate the original slices
	existingCopy := append([]rbacv1.Subject(nil), existing.Subjects...)
	desiredCopy := append([]rbacv1.Subject(nil), desired.Subjects...)

	less := func(a, b rbacv1.Subject) bool {
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	}

	sort.Slice(existingCopy, func(i, j int) bool {
		return less(existingCopy[i], existingCopy[j])
	})
	sort.Slice(desiredCopy, func(i, j int) bool {
		return less(desiredCopy[i], desiredCopy[j])
	})

	for i := range existingCopy {
		if existingCopy[i].Kind != desiredCopy[i].Kind ||
			existingCopy[i].Name != desiredCopy[i].Name ||
			existingCopy[i].Namespace != desiredCopy[i].Namespace {
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

// generateJobsServiceAccountName generates the name for the jobs service account
func generateJobsServiceAccountName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-jobs"
}

// createJobsServiceAccount creates a service account for jobs created by this EvalHub instance
func (r *EvalHubReconciler) createJobsServiceAccount(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	serviceAccountName := generateJobsServiceAccountName(instance)

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                         "eval-hub",
				"app.kubernetes.io/name":      serviceAccountName,
				"app.kubernetes.io/instance":  instance.Name,
				"app.kubernetes.io/part-of":   "eval-hub",
				"app.kubernetes.io/component": "jobs",
				"app.kubernetes.io/version":   constants.Version,
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
		log.FromContext(ctx).Info("Creating a new Jobs ServiceAccount", "Namespace", sa.Namespace, "Name", sa.Name)
		err = r.Create(ctx, sa)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}
