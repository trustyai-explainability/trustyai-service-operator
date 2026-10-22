package tas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const databaseTuningHashAnnotation = "trustyai.opendatahub.io/database-tuning-hash"

var databaseConnectionEnvironmentVariables = map[string]struct{}{
	"DATABASE_SERVICE":                     {},
	"DATABASE_PORT":                        {},
	"DATABASE_NAME":                        {},
	"DATABASE_HOST":                        {},
	"DATABASE_USER":                        {},
	"DATABASE_USERNAME":                    {},
	"DATABASE_PASSWORD":                    {},
	"DATABASE_DATABASE":                    {},
	"DATABASE_KIND":                        {},
	"DATABASE_URL":                         {},
	"QUARKUS_DATASOURCE_DB_KIND":           {},
	"QUARKUS_DATASOURCE_JDBC_URL":          {},
	"QUARKUS_DATASOURCE_JDBC_MAX_SIZE":     {},
	"QUARKUS_DATASOURCE_USERNAME":          {},
	"QUARKUS_DATASOURCE_PASSWORD":          {},
	"QUARKUS_DATASOURCE_GENERATION":        {},
	"QUARKUS_DATASOURCE_JDBC_INITIAL_SIZE": {},
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
	if len(configMap.BinaryData) > 0 {
		return nil, fmt.Errorf("ConfigMap %q referenced by storage.databaseTuning must use text data entries; binaryData cannot be imported as environment variables", name)
	}
	if len(configMap.Data) == 0 {
		return nil, fmt.Errorf("ConfigMap %q referenced by storage.databaseTuning has no data entries to import as environment variables", name)
	}
	for key := range configMap.Data {
		if problems := validation.IsEnvVarName(key); len(problems) > 0 {
			return nil, fmt.Errorf("ConfigMap %q key %q cannot be imported as an environment variable: %s", name, key, problems[0])
		}
		if _, forbidden := databaseConnectionEnvironmentVariables[key]; forbidden {
			return nil, fmt.Errorf("ConfigMap %q key %q configures database connection or credentials; keep connection settings and secrets in storage.databaseConfigurations", name, key)
		}
	}
	return configMap, nil
}

func databaseTuningConfigMapHash(configMap *corev1.ConfigMap) string {
	if configMap == nil {
		return ""
	}
	data, err := json.Marshal(configMap.Data)
	if err != nil {
		// A map[string]string is always JSON serializable. Keep the helper total in
		// case its input type changes, while avoiding a rollout from an empty hash.
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func deploymentHasDatabaseTuning(deployment *appsv1.Deployment) bool {
	if deployment.Spec.Template.Annotations[databaseTuningHashAnnotation] != "" {
		return true
	}
	serviceContainer := serviceContainer(deployment)
	if serviceContainer == nil {
		return false
	}
	for _, source := range serviceContainer.EnvFrom {
		if source.ConfigMapRef != nil {
			return true
		}
	}
	return false
}

func databaseTuningDeploymentChanged(current, desired *appsv1.Deployment) bool {
	if current.Spec.Template.Annotations[databaseTuningHashAnnotation] != desired.Spec.Template.Annotations[databaseTuningHashAnnotation] {
		return true
	}
	currentContainer := serviceContainer(current)
	desiredContainer := serviceContainer(desired)
	if currentContainer == nil || desiredContainer == nil {
		return currentContainer != desiredContainer
	}
	return !reflect.DeepEqual(currentContainer.EnvFrom, desiredContainer.EnvFrom)
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
		statusErr := r.setDatabaseTuningCondition(ctx, instance, metav1.ConditionFalse, "InvalidReference", validationErr.Error(), true)
		if statusErr != nil {
			return fmt.Errorf("database tuning configuration is invalid: %v; updating status failed: %w", validationErr, statusErr)
		}
		return validationErr
	}

	if instance.Spec.Storage.IsDatabaseTuningSet() {
		return r.setDatabaseTuningCondition(ctx, instance, metav1.ConditionTrue, "ConfigMapAvailable", fmt.Sprintf("ConfigMap %q is available and can be imported as environment variables", instance.Spec.Storage.DatabaseTuning), false)
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
