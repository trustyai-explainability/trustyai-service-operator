package tas

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var supportedDatabaseTuningKeys = map[string]struct{}{
	"DATABASE_POOL_SIZE":              {},
	"DATABASE_MAX_OVERFLOW":           {},
	"DATABASE_POOL_TIMEOUT":           {},
	"DATABASE_POOL_RECYCLE":           {},
	"DATABASE_MAX_CONNECTIONS_BUDGET": {},
	"DATABASE_APPLICATION_NAME":       {},
	"DATABASE_STATEMENT_TIMEOUT_MS":   {},
	"DATABASE_SYNCHRONOUS_COMMIT":     {},
	"DATABASE_SQLITE_JOURNAL_MODE":    {},
	"DATABASE_SQLITE_SYNCHRONOUS":     {},
	"DATABASE_SQLITE_BUSY_TIMEOUT_MS": {},
	"DATABASE_SQLITE_CACHE_SIZE_KB":   {},
}

func (r *TrustyAIServiceReconciler) getDatabaseTuningConfigMap(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService) (*corev1.ConfigMap, error) {
	if !instance.Spec.Storage.IsDatabaseTuningSet() {
		return nil, nil
	}
	if !instance.Spec.Storage.IsStorageDatabase() {
		return nil, fmt.Errorf("storage.databaseTuning is supported only when storage.format is DATABASE (got %q)", instance.Spec.Storage.Format)
	}

	name := instance.Spec.Storage.DatabaseTuning
	configMap := &corev1.ConfigMap{}
	if err := r.crdReader().Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: name}, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("storage.databaseTuning references ConfigMap %q in namespace %q, but it does not exist", name, instance.Namespace)
		}
		return nil, fmt.Errorf("reading ConfigMap %q referenced by storage.databaseTuning: %w", name, err)
	}
	if err := validateDatabaseTuningConfigMap(configMap); err != nil {
		return nil, err
	}
	return configMap, nil
}

func validateDatabaseTuningConfigMap(configMap *corev1.ConfigMap) error {
	if len(configMap.BinaryData) > 0 {
		return fmt.Errorf("ConfigMap %q uses binaryData, which is not supported for database tuning; use data with supported tuning keys", configMap.Name)
	}
	for key := range configMap.Data {
		if _, supported := supportedDatabaseTuningKeys[key]; !supported {
			return fmt.Errorf("ConfigMap %q contains unsupported database tuning key %q", configMap.Name, key)
		}
	}
	return nil
}

func databaseTuningConfigMapEnvVars(configMapName string) []corev1.EnvVar {
	keys := make([]string, 0, len(supportedDatabaseTuningKeys))
	for key := range supportedDatabaseTuningKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	optional := true
	envVars := make([]corev1.EnvVar, 0, len(keys))
	for _, key := range keys {
		envVars = append(envVars, corev1.EnvVar{
			Name: key,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
					Key:                  key,
					Optional:             &optional,
				},
			},
		})
	}
	return envVars
}

func deploymentHasDatabaseTuning(deployment *appsv1.Deployment) bool {
	container := serviceContainer(deployment)
	if container == nil {
		return false
	}
	for _, env := range container.Env {
		if _, supported := supportedDatabaseTuningKeys[env.Name]; supported && env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
			return true
		}
	}
	return false
}

func databaseTuningDeploymentChanged(current, desired *appsv1.Deployment) bool {
	currentContainer := serviceContainer(current)
	desiredContainer := serviceContainer(desired)
	if currentContainer == nil || desiredContainer == nil {
		return currentContainer != desiredContainer
	}
	return !reflect.DeepEqual(databaseTuningEnvVars(currentContainer.Env), databaseTuningEnvVars(desiredContainer.Env))
}

func databaseTuningEnvVars(envVars []corev1.EnvVar) []corev1.EnvVar {
	tuningEnvVars := make([]corev1.EnvVar, 0, len(supportedDatabaseTuningKeys))
	for _, env := range envVars {
		if _, supported := supportedDatabaseTuningKeys[env.Name]; supported && env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
			tuningEnvVars = append(tuningEnvVars, env)
		}
	}
	return tuningEnvVars
}

func serviceContainer(deployment *appsv1.Deployment) *corev1.Container {
	for i := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[i].Name == "trustyai-service" {
			return &deployment.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

func (r *TrustyAIServiceReconciler) validateAndSetDatabaseTuningStatus(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService) error {
	_, validationErr := r.getDatabaseTuningConfigMap(ctx, instance)
	if validationErr != nil {
		statusErr := r.setDatabaseTuningCondition(ctx, instance, metav1.ConditionFalse, "InvalidConfiguration", validationErr.Error(), true)
		if statusErr != nil {
			return fmt.Errorf("database tuning configuration is invalid: %v; updating status failed: %w", validationErr, statusErr)
		}
		return validationErr
	}

	if instance.Spec.Storage.IsDatabaseTuningSet() {
		return r.setDatabaseTuningCondition(ctx, instance, metav1.ConditionTrue, "ConfigMapAvailable", fmt.Sprintf("ConfigMap %q is available for database tuning", instance.Spec.Storage.DatabaseTuning), false)
	}
	// Clear a previous failure if the optional reference was removed.
	return r.setDatabaseTuningCondition(ctx, instance, metav1.ConditionTrue, "NotConfigured", "No database tuning ConfigMap is configured", false)
}

func (r *TrustyAIServiceReconciler) setDatabaseTuningCondition(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, status metav1.ConditionStatus, reason, message string, markUnavailable bool) error {
	conditionExists := false
	conditionMatches := false
	for _, condition := range instance.Status.Conditions {
		if condition.Type == ConditionTypeDatabaseTuningAvailable {
			conditionExists = true
			conditionMatches = condition.Status == status && condition.Reason == reason && condition.Message == message && condition.ObservedGeneration == instance.Generation
			break
		}
	}
	if !conditionExists && !instance.Spec.Storage.IsDatabaseTuningSet() && !markUnavailable {
		return nil
	}
	if conditionMatches {
		return nil
	}
	_, err := r.updateStatus(ctx, instance, func(saved *trustyaiopendatahubiov1.TrustyAIService) {
		saved.SetStatus(ConditionTypeDatabaseTuningAvailable, reason, message, coreConditionStatus(status))
		if markUnavailable {
			UpdateTrustyAIServiceNotAvailable(saved)
			saved.Status.Phase = PhaseNotReady
			saved.Status.Ready = corev1.ConditionFalse
			saved.SetStatus(ConditionTypeReady, "DatabaseTuningInvalid", "Database tuning ConfigMap is missing or invalid", corev1.ConditionFalse)
			saved.SetStatus(ConditionTypeProvisioningSucceeded, "ProvisioningFailed", "Service provisioning cannot continue because database tuning configuration is invalid", corev1.ConditionFalse)
			saved.SetStatus(ConditionTypeDegraded, "InvalidDatabaseTuning", "Database tuning ConfigMap is missing or invalid", corev1.ConditionTrue)
		}
	})
	return err
}

func coreConditionStatus(status metav1.ConditionStatus) corev1.ConditionStatus {
	switch status {
	case metav1.ConditionTrue:
		return corev1.ConditionTrue
	case metav1.ConditionFalse:
		return corev1.ConditionFalse
	default:
		return corev1.ConditionUnknown
	}
}
