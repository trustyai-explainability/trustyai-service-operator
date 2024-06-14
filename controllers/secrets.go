package controllers

import (
	"context"
	"fmt"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// findDatabaseSecret finds the DB configuration secret named (specified or default) in the same namespace as the CR
func (r *TrustyAIServiceReconciler) findDatabaseSecret(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) (*corev1.Secret, error) {

	databaseConfigurationsName := instance.Spec.Storage.DatabaseConfigurations
	defaultDatabaseConfigurationsName := instance.Name + dbCredentialsSuffix

	secret := &corev1.Secret{}

	if databaseConfigurationsName != "" {
		secret := &corev1.Secret{}
		err := r.Get(ctx, client.ObjectKey{Name: databaseConfigurationsName, Namespace: instance.Namespace}, secret)
		if err == nil {
			return secret, nil
		}
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get secret %s in namespace %s: %w", databaseConfigurationsName, instance.Namespace, err)
		}
	} else {
		// If specified not found, try the default

		err := r.Get(ctx, client.ObjectKey{Name: defaultDatabaseConfigurationsName, Namespace: instance.Namespace}, secret)
		if err == nil {
			return secret, nil
		}
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get secret %s in namespace %s: %w", defaultDatabaseConfigurationsName, instance.Namespace, err)
		}

	}

	return nil, fmt.Errorf("neither secret %s nor %s found in namespace %s", databaseConfigurationsName, defaultDatabaseConfigurationsName, instance.Namespace)
}

// validateDatabaseSecret validates the DB configuration secret
func (r *TrustyAIServiceReconciler) validateDatabaseSecret(secret *corev1.Secret) error {

	mandatoryKeys := []string{"databaseKind", "databaseUsername", "databasePassword", "databaseService", "databasePort"}

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
