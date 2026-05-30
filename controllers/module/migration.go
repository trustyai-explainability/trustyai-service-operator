package module

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
)

const (
	migrationAnnotation = "trustyai.opendatahub.io/migration-completed"
	fieldManager        = "trustyai-module-operator"
)

// isMigrationCompleted checks whether migration has already run.
func isMigrationCompleted(instance *modulev1alpha1.TrustyAI) bool {
	if instance.Annotations == nil {
		return false
	}
	return instance.Annotations[migrationAnnotation] == "true"
}

// runMigration performs one-time adoption of pre-existing resources when the
// operator transitions from in-tree component deployment to modular
// deployment. It:
//  1. Adopts the operator ConfigMap via Server-Side Apply (transfers field
//     ownership from the old ODH in-tree field manager)
//  2. Marks migration as completed via an annotation on the module CR
//
// If the operator ConfigMap does not exist (fresh install), this is a no-op
// that still marks migration as completed.
func (r *Reconciler) runMigration(ctx context.Context, instance *modulev1alpha1.TrustyAI) error {
	if isMigrationCompleted(instance) {
		return nil
	}

	log := log.FromContext(ctx)
	log.Info("Running one-time migration check")

	ns := operatorNamespace()

	if ns != "" {
		adopted, err := r.adoptConfigMap(ctx, ns)
		if err != nil {
			log.Error(err, "Failed to adopt operator ConfigMap during migration")
			meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
				Type:               ConditionProvisioningSucceeded,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: instance.Generation,
				Reason:             "MigrationFailed",
				Message:            fmt.Sprintf("Failed to adopt ConfigMap: %v", err),
			})
			return err
		}
		if adopted {
			log.Info("Adopted pre-existing operator ConfigMap via SSA",
				"configmap", constants.ConfigMap, "namespace", ns)
		} else {
			log.Info("No pre-existing operator ConfigMap found, fresh install")
		}
	}

	if instance.Annotations == nil {
		instance.Annotations = make(map[string]string)
	}
	instance.Annotations[migrationAnnotation] = "true"
	if err := r.Update(ctx, instance); err != nil {
		return fmt.Errorf("marking migration completed: %w", err)
	}

	log.Info("Migration check completed")
	return nil
}

// adoptConfigMap uses Server-Side Apply with ForceOwnership to take ownership
// of the operator ConfigMap. This handles the case where the ConfigMap was
// previously managed by the ODH operator's in-tree Kustomize deployment with
// a different field manager. Returns (true, nil) if the ConfigMap existed and
// was adopted, (false, nil) if it did not exist.
func (r *Reconciler) adoptConfigMap(ctx context.Context, namespace string) (bool, error) {
	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      constants.ConfigMap,
		Namespace: namespace,
	}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking for operator ConfigMap: %w", err)
	}

	applyData, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      existing.Name,
			"namespace": existing.Namespace,
		},
		"data": existing.Data,
	})
	if err != nil {
		return false, fmt.Errorf("marshalling ConfigMap for SSA: %w", err)
	}

	force := true
	err = r.Patch(ctx, existing, client.RawPatch(types.ApplyPatchType, applyData), &client.PatchOptions{
		FieldManager: fieldManager,
		Force:        &force,
	})
	if err != nil {
		return false, fmt.Errorf("SSA patch on ConfigMap: %w", err)
	}

	return true, nil
}

// operatorNamespace returns the operator's namespace, checking
// APPLICATIONS_NAMESPACE env var first, then the in-cluster file.
func operatorNamespace() string {
	if ns := os.Getenv("APPLICATIONS_NAMESPACE"); ns != "" {
		return ns
	}
	ns, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return ""
	}
	return string(ns)
}
