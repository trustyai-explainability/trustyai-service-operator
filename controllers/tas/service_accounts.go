package tas

import (
	"context"

	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func generateServiceAccountName(instance *trustyaiopendatahubiov1alpha1.TrustyAIService) string {
	return instance.Name + "-proxy"
}

// createServiceAccount creates a service account for this instance's OAuth proxy
func (r *TrustyAIServiceReconciler) createServiceAccount(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	routeName := instance.Name
	serviceAccountName := generateServiceAccountName(instance)

	// Define the OAuth redirect reference
	oauthRedirectRef := struct {
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
		Reference  struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"reference"`
	}{
		Kind:       "OAuthRedirectReference",
		APIVersion: "v1",
		Reference: struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		}{
			Kind: "Route",
			Name: routeName,
		},
	}

	// Marshal the struct into JSON format for the annotation
	oauthRedirectRefJSON, err := json.Marshal(oauthRedirectRef)

	if err != nil {
		// Handle error
		return err
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				"serviceaccounts.openshift.io/oauth-redirectreference.primary": string(oauthRedirectRefJSON),
			},
			Labels: map[string]string{
				"app":                        componentName,
				"app.kubernetes.io/name":     serviceAccountName,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  componentName,
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
	err = r.Get(ctx, types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}, found)
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

// createClusterRoleBinding creates a binding between the service account and token review cluster role
func (r *TrustyAIServiceReconciler) createClusterRoleBinding(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, serviceAccountName string) error {
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-" + instance.Namespace + "-proxy-rolebinding",
			Namespace: instance.Namespace,
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
			Name:     "trustyai-service-operator-proxy-role",
			APIGroup: rbacv1.GroupName,
		},
	}

	// Set instance as the owner of the ClusterRoleBinding
	if err := controllerutil.SetControllerReference(instance, clusterRoleBinding, r.Scheme); err != nil {
		return err
	}

	// Check if this ClusterRoleBinding already exists
	found := &rbacv1.ClusterRoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: clusterRoleBinding.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating a new ClusterRoleBinding", "Name", clusterRoleBinding.Name)
		err = r.Create(ctx, clusterRoleBinding)
		if err != nil {
			log.FromContext(ctx).Error(err, "Error creating a new ClusterRoleBinding")
			return err
		}
	}

	return nil
}
