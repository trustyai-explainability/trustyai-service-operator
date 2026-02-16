package evalhub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"

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

var dns1123LabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// normalizeDNS1123LabelValue converts s into a DNS-1123 compatible string that is <= 63 chars.
// It preserves a human-readable prefix and appends a short stable hash to avoid collisions.
//
// This is intentionally stricter than the Kubernetes label value regex to ensure cross-tool
// compatibility (e.g. components that validate against DNS-1123).
func normalizeDNS1123LabelValue(s string) string {
	const maxLen = 63
	const hashLen = 10 // 40 bits of hash in hex; low collision risk for our use.

	raw := strings.ToLower(strings.TrimSpace(s))
	if raw == "" {
		return "x"
	}

	// Replace any invalid character with '-' and collapse runs of '-'.
	var b strings.Builder
	b.Grow(len(raw))
	lastDash := false
	for _, r := range raw {
		isAllowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !isAllowed {
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
			continue
		}
		if r == '-' {
			if lastDash {
				continue
			}
			lastDash = true
			b.WriteByte('-')
			continue
		}
		lastDash = false
		b.WriteRune(r)
	}
	clean := strings.Trim(b.String(), "-")
	if clean == "" {
		clean = "x"
	}

	// If already valid and within limit, return as-is.
	if len(clean) <= maxLen && dns1123LabelRe.MatchString(clean) {
		return clean
	}

	sum := sha256.Sum256([]byte(s))
	h := hex.EncodeToString(sum[:])[:hashLen]

	// Keep as much prefix as possible while reserving "-<hash>".
	prefixMax := maxLen - 1 - hashLen
	prefix := clean
	if len(prefix) > prefixMax {
		prefix = prefix[:prefixMax]
		prefix = strings.Trim(prefix, "-")
		if prefix == "" {
			prefix = "x"
		}
	}

	out := prefix + "-" + h
	// Defensive: ensure output is valid.
	out = strings.Trim(out, "-")
	if len(out) > maxLen {
		out = out[:maxLen]
		out = strings.Trim(out, "-")
	}
	if out == "" || !dns1123LabelRe.MatchString(out) {
		// Last resort: just the hash with a leading alpha prefix.
		out = "x-" + h
	}
	return out
}

func generateServiceAccountName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-api"
}

// generateAPIAccessRoleName returns the name for the per-instance API access Role.
func generateAPIAccessRoleName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-api-access-role"
}

// generateJobsAPIAccessRoleName returns the name for the per-instance jobs API access Role.
func generateJobsAPIAccessRoleName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-jobs-api-access-role"
}

func generateAuthReviewerClusterRoleBindingName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-" + instance.Namespace + "-auth-reviewer-crb"
}

// generateAuthReviewerClusterRoleBindingAppNameLabelValue returns a deterministic, DNS-1123 compatible
// label value (<=63 chars) derived from the full auth reviewer ClusterRoleBinding name.
func generateAuthReviewerClusterRoleBindingAppNameLabelValue(instance *evalhubv1alpha1.EvalHub) string {
	return normalizeDNS1123LabelValue(generateAuthReviewerClusterRoleBindingName(instance))
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
				"app.kubernetes.io/name":     normalizeDNS1123LabelValue(serviceAccountName),
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

	// Create per-instance Roles before RoleBindings
	err = r.createAPIAccessRole(ctx, instance)
	if err != nil {
		return err
	}

	err = r.createJobsAPIAccessRole(ctx, instance)
	if err != nil {
		return err
	}

	// Create namespace-scoped RoleBinding for EvalHub API access (evalhubs/proxy)
	err = r.createAPIAccessRoleBinding(ctx, instance, serviceAccountName)
	if err != nil {
		return err
	}

	// Create RoleBindings for split resource-manager roles (API SA only)
	err = r.createJobsWriterRoleBinding(ctx, instance, serviceAccountName)
	if err != nil {
		return err
	}

	err = r.createJobConfigRoleBinding(ctx, instance, serviceAccountName)
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

// authReviewerClusterRoleName is the ClusterRole for kube-rbac-proxy auth checks only.
// It contains only tokenreviews/create and subjectaccessreviews/create permissions.
const authReviewerClusterRoleName = "trustyai-service-operator-evalhub-auth-reviewer-role"

// Split resource-manager ClusterRole names.
// These replace the monolithic evalhub-resource-manager with function-specific roles.
const (
	jobsWriterClusterRoleName = "trustyai-service-operator-evalhub-jobs-writer"
	jobConfigClusterRoleName  = "trustyai-service-operator-evalhub-job-config"
)

// MLFlow access uses custom ClusterRoles scoped to the "mlflow.kubeflow.org" API group.
// MLFlow's kubernetes-workspace-provider checks permissions via SelfSubjectAccessReview
// against this group (not core Kubernetes resources). The ClusterRoles are pre-created
// at operator installation time (config/rbac/evalhub_mlflow_access_role.yaml and
// config/rbac/evalhub_mlflow_jobs_role.yaml).
const mlflowAccessClusterRoleName = "trustyai-service-operator-evalhub-mlflow-access"

// mlflowJobsAccessClusterRoleName is a restricted MLflow ClusterRole for job pods.
// Jobs only need create, get, list -- not update or delete.
const mlflowJobsAccessClusterRoleName = "trustyai-service-operator-evalhub-mlflow-jobs-access"

// createAPIAccessRole creates a per-instance namespaced Role with resourceNames
// scoped to this specific EvalHub instance. This ensures the SA can only access
// its own instance's evalhubs/proxy subresource.
func (r *EvalHubReconciler) createAPIAccessRole(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)

	roleName := generateAPIAccessRoleName(instance)
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     roleName,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  "eval-hub",
				"app.kubernetes.io/version":  constants.Version,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"trustyai.opendatahub.io"},
				Resources:     []string{"evalhubs"},
				ResourceNames: []string{instance.Name},
				Verbs:         []string{"get"},
			},
			{
				APIGroups:     []string{"trustyai.opendatahub.io"},
				Resources:     []string{"evalhubs/proxy"},
				ResourceNames: []string{instance.Name},
				Verbs:         []string{"get", "create"},
			},
		},
	}

	if err := ctrl.SetControllerReference(instance, role, r.Scheme); err != nil {
		return err
	}

	found := &rbacv1.Role{}
	err := r.Get(ctx, types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating API access Role", "Namespace", role.Namespace, "Name", role.Name)
		return r.Create(ctx, role)
	} else if err != nil {
		return err
	}

	// Role exists, check if rules need updating
	if !equalPolicyRules(found.Rules, role.Rules) {
		found.Rules = role.Rules
		log.Info("Updating API access Role rules", "Name", role.Name)
		return r.Update(ctx, found)
	}

	return nil
}

// createJobsAPIAccessRole creates a per-instance namespaced Role for the jobs SA
// with resourceNames scoped to this specific EvalHub instance.
func (r *EvalHubReconciler) createJobsAPIAccessRole(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)

	roleName := generateJobsAPIAccessRoleName(instance)
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     roleName,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  "eval-hub",
				"app.kubernetes.io/version":  constants.Version,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"trustyai.opendatahub.io"},
				Resources:     []string{"evalhubs/proxy"},
				ResourceNames: []string{instance.Name},
				Verbs:         []string{"get", "create"},
			},
		},
	}

	if err := ctrl.SetControllerReference(instance, role, r.Scheme); err != nil {
		return err
	}

	found := &rbacv1.Role{}
	err := r.Get(ctx, types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating jobs API access Role", "Namespace", role.Namespace, "Name", role.Name)
		return r.Create(ctx, role)
	} else if err != nil {
		return err
	}

	if !equalPolicyRules(found.Rules, role.Rules) {
		found.Rules = role.Rules
		log.Info("Updating jobs API access Role rules", "Name", role.Name)
		return r.Update(ctx, found)
	}

	return nil
}

// createMLFlowAccessRoleBinding creates a RoleBinding for a ServiceAccount to
// the specified MLflow ClusterRole in the instance namespace. This allows the
// ServiceAccount to pass MLFlow's kubernetes-auth SubjectAccessReview checks
// against the mlflow.kubeflow.org API group in the workspace namespace.
func (r *EvalHubReconciler) createMLFlowAccessRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string, suffix string, clusterRoleName string) error {
	log := log.FromContext(ctx)

	roleBindingName := instance.Name + "-mlflow-" + suffix + "-rb"
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

	clusterRoleBindingName := generateAuthReviewerClusterRoleBindingName(instance)
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleBindingName,
			Labels: map[string]string{
				"app":                        "eval-hub",
				"app.kubernetes.io/name":     generateAuthReviewerClusterRoleBindingAppNameLabelValue(instance),
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
// ServiceAccount to the per-instance Role for evalhubs/proxy access.
func (r *EvalHubReconciler) createAPIAccessRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	roleBindingName := instance.Name + "-api-access-rb"
	roleName := generateAPIAccessRoleName(instance)

	return r.createGenericRoleBinding(ctx, instance, roleBindingName, serviceAccountName, rbacv1.RoleRef{
		Kind:     "Role",
		Name:     roleName,
		APIGroup: rbacv1.GroupName,
	})
}

// createJobsAPIAccessRoleBinding creates a namespace-scoped RoleBinding for the jobs
// ServiceAccount to the per-instance Role for evalhubs/proxy access.
func (r *EvalHubReconciler) createJobsAPIAccessRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	roleBindingName := instance.Name + "-jobs-api-access-rb"
	roleName := generateJobsAPIAccessRoleName(instance)

	return r.createGenericRoleBinding(ctx, instance, roleBindingName, serviceAccountName, rbacv1.RoleRef{
		Kind:     "Role",
		Name:     roleName,
		APIGroup: rbacv1.GroupName,
	})
}

// createJobsWriterRoleBinding creates a RoleBinding for the API SA to the
// jobs-writer ClusterRole (batch/jobs create,delete).
func (r *EvalHubReconciler) createJobsWriterRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	return r.createGenericRoleBinding(ctx, instance, instance.Name+"-jobs-writer-rb", serviceAccountName, rbacv1.RoleRef{
		Kind:     "ClusterRole",
		Name:     jobsWriterClusterRoleName,
		APIGroup: rbacv1.GroupName,
	})
}

// createJobConfigRoleBinding creates a RoleBinding for the API SA to the
// job-config ClusterRole (configmaps create,get,list).
func (r *EvalHubReconciler) createJobConfigRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, serviceAccountName string) error {
	return r.createGenericRoleBinding(ctx, instance, instance.Name+"-job-config-rb", serviceAccountName, rbacv1.RoleRef{
		Kind:     "ClusterRole",
		Name:     jobConfigClusterRoleName,
		APIGroup: rbacv1.GroupName,
	})
}

// createGenericRoleBinding creates a namespace-scoped RoleBinding with the given
// name, subject SA, and role reference. Handles create-or-update logic including
// immutable RoleRef recreation.
func (r *EvalHubReconciler) createGenericRoleBinding(ctx context.Context, instance *evalhubv1alpha1.EvalHub, roleBindingName string, serviceAccountName string, roleRef rbacv1.RoleRef) error {
	log := log.FromContext(ctx)

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
		RoleRef: roleRef,
	}

	if err := ctrl.SetControllerReference(instance, roleBinding, r.Scheme); err != nil {
		return err
	}

	found := &rbacv1.RoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: roleBinding.Name, Namespace: roleBinding.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating RoleBinding", "Namespace", roleBinding.Namespace, "Name", roleBinding.Name)
		return r.Create(ctx, roleBinding)
	} else if err != nil {
		return err
	}

	subjectsEqual := equalRoleBindingSubjects(found.Subjects, roleBinding.Subjects)
	roleRefEqual := equalRoleBindingRoleRef(found.RoleRef, roleBinding.RoleRef)

	if !subjectsEqual || !roleRefEqual {
		if roleRefEqual && !subjectsEqual {
			found.Subjects = roleBinding.Subjects
			log.Info("Updating RoleBinding subjects", "Name", roleBinding.Name)
			return r.Update(ctx, found)
		} else if !roleRefEqual {
			log.Info("RoleRef differs, deleting and recreating RoleBinding", "Name", roleBinding.Name)
			if err := r.Delete(ctx, found); err != nil {
				return err
			}
			log.Info("Creating new RoleBinding", "Name", roleBinding.Name)
			return r.Create(ctx, roleBinding)
		}
	}

	return nil
}

// equalPolicyRules compares two slices of PolicyRules for equality.
func equalPolicyRules(a, b []rbacv1.PolicyRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalStringSlices(a[i].APIGroups, b[i].APIGroups) ||
			!equalStringSlices(a[i].Resources, b[i].Resources) ||
			!equalStringSlices(a[i].ResourceNames, b[i].ResourceNames) ||
			!equalStringSlices(a[i].Verbs, b[i].Verbs) {
			return false
		}
	}
	return true
}

// equalStringSlices compares two string slices for equality (order-sensitive).
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
				"app.kubernetes.io/name":      normalizeDNS1123LabelValue(serviceAccountName),
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
