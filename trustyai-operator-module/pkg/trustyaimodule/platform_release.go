package trustyaimodule

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// readPlatformVersion reads the platformVersion key from the platform-managed
// PlatformConfigMapName ConfigMap. Returns an empty string, with no error, if
// the ConfigMap or key does not exist yet (e.g. before the platform operator
// has created it).
func (r *TrustyAIModuleReconciler) readPlatformVersion(ctx context.Context) (string, error) {
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      PlatformConfigMapName,
		Namespace: r.Namespace,
	}, cm)
	if err != nil {
		if errors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get platform config ConfigMap: %w", err)
	}
	return cm.Data[PlatformVersionKey], nil
}

// updatePlatformRelease completes the platform version handshake: it reads
// the platform version the platform operator is currently running and
// records it in the module's status.releases[name="platform"] entry, once
// the module has reconciled successfully under that version. TrustyAI has no
// version-gated migration steps today, so there is no upgrade work to
// perform before advancing the recorded version.
func (r *TrustyAIModuleReconciler) updatePlatformRelease(ctx context.Context, module *platformv1alpha1.TrustyAI) error {
	platformVersion, err := r.readPlatformVersion(ctx)
	if err != nil {
		return err
	}
	if platformVersion == "" {
		return nil
	}
	module.Status.SetPlatformRelease(platformVersion)
	return nil
}
