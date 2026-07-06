package utils

import (
	"context"
	"fmt"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getSecret retrieves a secret if it exists, returns an error if not
func GetSecret(ctx context.Context, c client.Client, name, namespace string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("secret %s not found in namespace %s: %w", name, namespace, err)
		}
		return nil, fmt.Errorf("failed to get secret %s in namespace %s: %w", name, namespace, err)
	}
	return secret, nil
}

// MountSecret adds a given secret name as a mounted volume to a deployment
// use containerIndices to control which containers in the deployment receive the secret
func MountSecret(deployment *v1.Deployment, secretToMount string, volumeName string, mountPath string, containerIndices []int) {
	// Add to volumes
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  secretToMount,
				DefaultMode: func() *int32 { i := int32(420); return &i }(),
			},
		},
	})
	// For each container in containerIndices, mount the secrets
	for _, idx := range containerIndices {
		if idx < len(deployment.Spec.Template.Spec.Containers) {
			deployment.Spec.Template.Spec.Containers[idx].VolumeMounts = append(
				deployment.Spec.Template.Spec.Containers[idx].VolumeMounts,
				corev1.VolumeMount{
					Name:      volumeName,
					MountPath: mountPath,
					ReadOnly:  true,
				},
			)
		}
	}
}
