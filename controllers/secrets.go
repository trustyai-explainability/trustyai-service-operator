package controllers

import (
	"context"
	"fmt"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getSecret retrieves a secret if it exists, returns an error if not
func (r *TrustyAIServiceReconciler) getSecret(ctx context.Context, name, namespace string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("secret %s not found in namespace %s: %w", name, namespace, err)
		}
		return nil, fmt.Errorf("failed to get secret %s in namespace %s: %w", name, namespace, err)
	}
	return secret, nil
}

// findDatabaseSecret finds the DB configuration secret named (specified or default) in the same namespace as the CR
func (r *TrustyAIServiceReconciler) findDatabaseSecret(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) (*corev1.Secret, error) {

	databaseConfigurationsName := instance.Spec.Storage.DatabaseConfigurations
	defaultDatabaseConfigurationsName := instance.Name + dbCredentialsSuffix

	if databaseConfigurationsName != "" {
		secret, err := r.getSecret(ctx, databaseConfigurationsName, instance.Namespace)
		if err != nil {
			return nil, err
		}
		if secret != nil {
			return secret, nil
		}
	} else {
		// If specified not found, try the default
		secret, err := r.getSecret(ctx, defaultDatabaseConfigurationsName, instance.Namespace)
		if err != nil {
			return nil, err
		}
		if secret != nil {
			return secret, nil
		}
	}

	return nil, fmt.Errorf("neither secret %s nor %s found in namespace %s", databaseConfigurationsName, defaultDatabaseConfigurationsName, instance.Namespace)
}

// validateDatabaseSecret validates the DB configuration secret
func (r *TrustyAIServiceReconciler) validateDatabaseSecret(secret *corev1.Secret) error {

	mandatoryKeys := []string{
		"databaseKind",
		"databaseUsername",
		"databasePassword",
		"databaseService",
		"databasePort",
		"databaseName",
	}

	for _, key := range mandatoryKeys {
		value, exists := secret.Data[key]
		if !exists {
			return fmt.Errorf("mandatory key %s is missing from database configuration", key)
		}
		if len(value) == 0 {
			return fmt.Errorf("mandatory key %s is empty on database configuration", key)
		}
	}
	return nil
}
