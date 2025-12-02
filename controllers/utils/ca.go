package utils

// modified from https://github.com/opendatahub-io/llama-stack-k8s-operator/blob/odh/controllers/resource_helper.go

import (
	"errors"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"regexp"
	"strings"
)

// Constants for validation limits.
const (
	// maxConfigMapKeyLength defines the maximum allowed length for ConfigMap keys
	// based on Kubernetes DNS subdomain name limits.
	maxConfigMapKeyLength = 253
)

// validConfigMapKeyRegex defines allowed characters for ConfigMap keys.
// Kubernetes ConfigMap keys must be valid DNS subdomain names or data keys.
var validConfigMapKeyRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-_.]*[a-zA-Z0-9])?$`)

// validateConfigMapKeys validates that all ConfigMap keys contain only safe characters.
// Note: This function validates key names only. PEM content validation is performed
// separately in the controller's reconcileCABundleConfigMap function.
func validateConfigMapKeys(keys []string) error {
	for _, key := range keys {
		if key == "" {
			return errors.New("ConfigMap key cannot be empty")
		}
		if len(key) > maxConfigMapKeyLength {
			return fmt.Errorf("failed to validate ConfigMap key '%s': too long (max %d characters)", key, maxConfigMapKeyLength)
		}
		if !validConfigMapKeyRegex.MatchString(key) {
			return fmt.Errorf("failed to validate ConfigMap key '%s': contains invalid characters. Only alphanumeric characters, hyphens, underscores, and dots are allowed", key)
		}
		// Additional security check: prevent path traversal attempts
		if strings.Contains(key, "..") || strings.Contains(key, "/") {
			return fmt.Errorf("failed to validate ConfigMap key '%s': contains invalid path characters", key)
		}
	}
	return nil
}

type CABundleSourceVolume struct {
	// the volume that contains the CA files to be concatenated
	CABundleSourceVolumeName string
	// the directory to use on the source volume
	CABundleSourceDir string
	// the CA bundle config - contains the file names that contain CA info
	CABundleFileNames []string
}

type CABundleInitContainerConfig struct {
	// the name of the ca bundle init container
	CABundleInitName string
	// the container image to use for the ca bundle init container
	CABundleInitImage string

	// the volumes that contain the CA bundles
	CABundleSources []CABundleSourceVolume

	// the name of the volume used to transfer the created CA file to the containers
	CABundleTransferVolumeName string
	// the directory to use on the transfer volume
	CABundleTransferDir string
	// the filename to use on the transfer volume in the transfer directory
	CABundleTransferFileName string
}

// CreateCABundleInitContainer creates an InitContainer that concatenates multiple CA bundle keys
// from a number of source ConfigMap into a single file in the ca-bundle "transfer" volume.
func CreateCABundleInitContainer(caBundleInitContainerConfig CABundleInitContainerConfig) (corev1.Container, error) {

	var volumeMounts = []corev1.VolumeMount{
		{
			Name:      caBundleInitContainerConfig.CABundleTransferVolumeName,
			MountPath: caBundleInitContainerConfig.CABundleTransferDir,
		},
	}

	var fileListBuilder strings.Builder
	for i, caBundleSource := range caBundleInitContainerConfig.CABundleSources {
		// Validate ConfigMap keys for security
		if err := validateConfigMapKeys(caBundleSource.CABundleFileNames); err != nil {
			return corev1.Container{}, fmt.Errorf("failed to validate ConfigMap keys: %w", err)
		}

		// Build the file list as a shell array embedded in the script
		// This ensures the arguments are properly passed to the script

		for j, key := range caBundleSource.CABundleFileNames {
			if i > 0 || j > 0 {
				fileListBuilder.WriteString(" ")
			}
			// Quote each key to handle any special characters safely
			fileListBuilder.WriteString(fmt.Sprintf("%q", caBundleSource.CABundleSourceDir+"/"+key))
		}

		// add the CA bundle source volume to the initcontainer volume mounts
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      caBundleSource.CABundleSourceVolumeName,
			MountPath: caBundleSource.CABundleSourceDir,
		})
	}
	fileList := fileListBuilder.String()

	// Use a secure script approach that embeds the file list directly
	// This eliminates the issue with arguments not being passed to sh -c

	script := fmt.Sprintf(`#!/bin/sh
set -e
output_file="%s/%s"

# Clear the output file
> "$output_file"

# Process each validated key file (keys are pre-validated)
for file_path in %s; do
    if [ -f "$file_path" ]; then
        cat "$file_path" >> "$output_file"
        echo >> "$output_file"  # Add newline between certificates
    else
        echo "Warning: Certificate file $file_path not found" >&2
    fi
done`, caBundleInitContainerConfig.CABundleTransferDir, caBundleInitContainerConfig.CABundleTransferFileName, fileList)

	return corev1.Container{
		Name:    caBundleInitContainerConfig.CABundleInitName,
		Image:   caBundleInitContainerConfig.CABundleInitImage,
		Command: []string{"/bin/sh", "-c", script},
		// No Args needed since we embed the file list in the script
		VolumeMounts: volumeMounts,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: &[]bool{false}[0],
			RunAsNonRoot:             &[]bool{true}[0],
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}, nil
}
