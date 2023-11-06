package controllers

import (
	"context"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ...

func (r *TrustyAIServiceReconciler) createServiceAccount(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	routeName := instance.Name
	saName := instance.Name + "-proxy"

	// Define the OAuth redirect reference using a struct for easy JSON encoding
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
			Name:      saName,
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				"serviceaccounts.openshift.io/oauth-redirectreference.primary": string(oauthRedirectRefJSON),
			},
			Labels: map[string]string{
				"app":                        componentName,
				"app.kubernetes.io/name":     saName,
				"app.kubernetes.io/instance": instance.Name,
				"app.kubernetes.io/part-of":  componentName,
				"app.kubernetes.io/version":  "0.1.0",
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

	// ServiceAccount created successfully
	return nil
}
