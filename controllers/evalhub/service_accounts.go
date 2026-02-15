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
	return instance.Name + "-api"
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

	// Create ClusterRoleBinding for kube-rbac-proxy auth (tokenreviews/subjectaccessreviews only)
	err = r.createAuthReviewerClusterRoleBinding(ctx, instance, serviceAccountName)
	if err != nil {
		return err
	}

	// Create namespace-scoped RoleBinding for EvalHub API access (evalhubs/proxy)
	err = r.createAPIAccessRoleBinding(ctx, instance, serviceAccountName)
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
	err = r.createJobsAPIAccessRoleBinding(ctx, instance, jobsServiceAccountName)
	if err != nil {
		return err
	}

	// Create MLFlow access RoleBindings for both ServiceAccounts.
	// MLFlow's kubernetes-auth plugin validates tokens via SubjectAccessReview against
	// the workspace namespace. The custom "evalhub-mlflow-access" ClusterRole provides
	// the required mlflow.kubeflow.org permissions for both the api and jobs SAs.
	err = r.createMLFlowAccessRoleBinding(ctx, instance, serviceAccountName, "api", mlflowAccessClusterRoleName)
	if err != nil {
		return err
	}
	err = r.createMLFlowAccessRoleBinding(ctx, instance, jobsServiceAccountName, "jobs", mlflowJobsAccessClusterRoleName)
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

// authReviewerClusterRoleName is the ClusterRole for kube-rbac-proxy auth checks only.
// It contains only tokenreviews/create and subjectaccessreviews/create permissions.
const authReviewerClusterRoleName = "trustyai-service-operator-evalhub-auth-reviewer-role"

// apiAccessClusterRoleName is the ClusterRole for EvalHub API access (evalhubs/proxy).
// Bound via namespace-scoped RoleBinding to restrict access to the instance's namespace.
const apiAccessClusterRoleName = "trustyai-service-operator-evalhub-api-role"

// jobsAPIAccessClusterRoleName is the ClusterRole for jobs ServiceAccount API access.
// Bound via namespace-scoped RoleBinding to restrict access to the instance's namespace.
const jobsAPIAccessClusterRoleName = "trustyai-service-operator-evalhub-jobs-api-role"

// createResourceManagementRoleBinding creates a RoleBinding for the API ServiceAccount
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

// MLFlow access uses custom ClusterRoles scoped to the "mlflow.kubeflow.org" API group.
// MLFlow's kubernetes-workspace-provider checks permissions via SelfSubjectAccessReview
// against this group (not core Kubernetes resources). The ClusterRoles are pre-created
// at operator installation time (config/rbac/evalhub_mlflow_access_role.yaml and
// config/rbac/evalhub_mlflow_jobs_role.yaml).
const mlflowAccessClusterRoleName = "trustyai-service-operator-evalhub-mlflow-access"

// mlflowJobsAccessClusterRoleName is a restricted MLflow ClusterRole for job pods.
// Jobs only need create, get, list — not update or delete.
const mlflowJobsAccessClusterRoleName = "trustyai-service-operator-evalhub-mlflow-jobs-access"

// createMLFlowAccessRoleBinding creates a RoleBinding for a ServiceAccount to
// the specified MLflow ClusterRole in the instance namespace. This allows the
// ServiceAccount to pass MLFlow's kubernetes-auth SubjectAccessReview checks
// against the mlflow.kubeflow.org API group in the workspace namespace.
func (r *EvalHubReconciler) createMLFlowAccessRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string, suffix string, clusterRoleName string) error {
	log := log.FromContext(ctx)

	roleBindingName := instance.Name + "-mlflow-" + suffix
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
			Name:     clusterRoleName,
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
		log.Info("Creating MLFlow access RoleBinding", "Namespace", roleBinding.Namespace, "Name", roleBinding.Name)
		return r.Create(ctx, roleBinding)
	} else if err != nil {
		return err
	}

	// RoleBinding exists, check if it needs updating
	subjectsEqual := equalRoleBindingSubjects(found.Subjects, roleBinding.Subjects)
	roleRefEqual := equalRoleBindingRoleRef(found.RoleRef, roleBinding.RoleRef)

	if !subjectsEqual || !roleRefEqual {
		if roleRefEqual && !subjectsEqual {
			found.Subjects = roleBinding.Subjects
			log.Info("Updating MLFlow access RoleBinding subjects", "Name", roleBinding.Name)
			return r.Update(ctx, found)
		} else if !roleRefEqual {
			log.Info("RoleRef differs, deleting and recreating MLFlow access RoleBinding", "Name", roleBinding.Name)
			if err := r.Delete(ctx, found); err != nil {
				return err
			}
			log.Info("Creating new MLFlow access RoleBinding", "Name", roleBinding.Name)
			return r.Create(ctx, roleBinding)
		}
	}

	return nil
}

// createAuthReviewerClusterRoleBinding creates a ClusterRoleBinding for kube-rbac-proxy
// auth checks (tokenreviews and subjectaccessreviews only). This is the only
// cluster-scoped binding needed for the EvalHub API ServiceAccount.
func (r *EvalHubReconciler) createAuthReviewerClusterRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	log := log.FromContext(ctx)

	clusterRoleBindingName := instance.Name + "-" + instance.Namespace + "-auth-reviewer"
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleBindingName,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     clusterRoleBindingName,
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
			Name:     authReviewerClusterRoleName,
			APIGroup: rbacv1.GroupName,
		},
	}

	// Check if ClusterRoleBinding already exists
	found := &rbacv1.ClusterRoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: clusterRoleBindingName}, found)
	if err != nil && errors.IsNotFound(err) {
		// Note: We don't set owner references because ClusterRoleBindings cannot have namespace-scoped owners
		log.Info("Creating auth reviewer ClusterRoleBinding", "name", clusterRoleBindingName)
		return r.Create(ctx, clusterRoleBinding)
	} else if err != nil {
		return err
	}

	// ClusterRoleBinding already exists, check if it needs updating
	subjectsEqual := equalSubjects(found, clusterRoleBinding)
	roleRefEqual := equalRoleRef(found, clusterRoleBinding)

	if !subjectsEqual || !roleRefEqual {
		if roleRefEqual && !subjectsEqual {
			found.Subjects = clusterRoleBinding.Subjects
			log.Info("Updating auth reviewer ClusterRoleBinding subjects", "name", clusterRoleBindingName)
			return r.Update(ctx, found)
		} else if !roleRefEqual {
			log.Info("RoleRef differs, deleting and recreating auth reviewer ClusterRoleBinding", "name", clusterRoleBindingName)
			if err := r.Delete(ctx, found); err != nil {
				return err
			}
			log.Info("Creating new auth reviewer ClusterRoleBinding", "name", clusterRoleBindingName)
			return r.Create(ctx, clusterRoleBinding)
		}
	}

	return nil
}

// createAPIAccessRoleBinding creates a namespace-scoped RoleBinding for the API
// ServiceAccount to access evalhubs/proxy within the instance's namespace only.
// This replaces the previous ClusterRoleBinding approach to enforce namespace boundaries.
func (r *EvalHubReconciler) createAPIAccessRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	log := log.FromContext(ctx)

	roleBindingName := instance.Name + "-api-rolebinding"
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
			Name:     apiAccessClusterRoleName,
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
		log.Info("Creating API access RoleBinding", "Namespace", roleBinding.Namespace, "Name", roleBinding.Name)
		return r.Create(ctx, roleBinding)
	} else if err != nil {
		return err
	}

	// RoleBinding exists, check if it needs updating
	subjectsEqual := equalRoleBindingSubjects(found.Subjects, roleBinding.Subjects)
	roleRefEqual := equalRoleBindingRoleRef(found.RoleRef, roleBinding.RoleRef)

	if !subjectsEqual || !roleRefEqual {
		if roleRefEqual && !subjectsEqual {
			found.Subjects = roleBinding.Subjects
			log.Info("Updating API access RoleBinding subjects", "Name", roleBinding.Name)
			return r.Update(ctx, found)
		} else if !roleRefEqual {
			log.Info("RoleRef differs, deleting and recreating API access RoleBinding", "Name", roleBinding.Name)
			if err := r.Delete(ctx, found); err != nil {
				return err
			}
			log.Info("Creating new API access RoleBinding", "Name", roleBinding.Name)
			return r.Create(ctx, roleBinding)
		}
	}

	return nil
}

// createJobsAPIAccessRoleBinding creates a namespace-scoped RoleBinding for the jobs
// ServiceAccount to access evalhubs/proxy within the instance's namespace only.
func (r *EvalHubReconciler) createJobsAPIAccessRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	log := log.FromContext(ctx)

	roleBindingName := instance.Name + "-jobs-api-rolebinding"
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     instance.Name + "-jobs-api",
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
			Name:     jobsAPIAccessClusterRoleName,
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
		log.Info("Creating jobs API access RoleBinding", "Name", roleBinding.Name)
		return r.Create(ctx, roleBinding)
	} else if err != nil {
		return err
	}

	// RoleBinding exists, check if it needs updating
	subjectsEqual := equalRoleBindingSubjects(found.Subjects, roleBinding.Subjects)
	roleRefEqual := equalRoleBindingRoleRef(found.RoleRef, roleBinding.RoleRef)

	if !subjectsEqual || !roleRefEqual {
		if roleRefEqual && !subjectsEqual {
			found.Subjects = roleBinding.Subjects
			log.Info("Updating jobs API access RoleBinding subjects", "Name", roleBinding.Name)
			return r.Update(ctx, found)
		} else if !roleRefEqual {
			log.Info("RoleRef differs, deleting and recreating jobs API access RoleBinding", "Name", roleBinding.Name)
			if err := r.Delete(ctx, found); err != nil {
				return err
			}
			log.Info("Creating new jobs API access RoleBinding", "Name", roleBinding.Name)
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
