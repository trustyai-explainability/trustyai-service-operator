/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lmes

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

const (
	DriverPath                 = "/bin/driver"
	DestDriverPath             = "/opt/app-root/src/bin/driver"
	OutputPath                 = "/opt/app-root/src/output"
	HuggingFaceHomePath        = "/opt/app-root/src/hf_home"
	PodImageKey                = "lmes-pod-image"
	DriverImageKey             = "lmes-driver-image"
	PodCheckingIntervalKey     = "lmes-pod-checking-interval"
	ImagePullPolicyKey         = "lmes-image-pull-policy"
	MaxBatchSizeKey            = "lmes-max-batch-size"
	DefaultBatchSizeKey        = "lmes-default-batch-size"
	DetectDeviceKey            = "lmes-detect-device"
	AllowOnline                = "lmes-allow-online"
	AllowCodeExecution         = "lmes-allow-code-execution"
	DriverPort                 = "lmes-driver-port"
	DefaultPodCheckingInterval = time.Second * 10
	DefaultImagePullPolicy     = corev1.PullAlways
	DefaultMaxBatchSize        = 24
	DefaultBatchSize           = "1"
	DefaultDetectDevice        = true
	ServiceName                = "LMES"

	// DefaultCABundleConfigMapName is the standard RHOAI ConfigMap that holds the cluster CA bundle.
	// It is injected into managed namespaces by the RHOAI operator.
	DefaultCABundleConfigMapName = "odh-trusted-ca-bundle"
	// ServiceCAConfigMapName is the well-known ConfigMap that contains the OpenShift
	// service-serving CA, used by cluster-internal HTTPS services (*.svc.cluster.local).
	ServiceCAConfigMapName = "openshift-service-ca.crt"
	// ServiceCAKey is the standard key within the service-serving CA ConfigMap.
	ServiceCAKey = "service-ca.crt"
	// MergedCABundleKey is the key used in the per-job merged CA ConfigMap.
	MergedCABundleKey = "merged-ca-bundle.crt"
	// MergedCAConfigMapSuffix is appended to the job name to form the merged CA ConfigMap name.
	MergedCAConfigMapSuffix = "-ca-bundle"
	// CABundleVolumeName is the volume name used when auto-mounting the merged CA bundle.
	CABundleVolumeName = "odh-ca-bundle"
	// CABundleMountPath is the file path at which the merged CA bundle is mounted inside the lm-eval pod.
	// REQUESTS_CA_BUNDLE is set to this path so Python's requests library picks it up automatically.
	CABundleMountPath = "/etc/ssl/certs/odh-ca-bundle.crt"

	// LastScheduledGenerationAnnotation records the spec generation that was active when the pod
	// was last created. When a completed job's current generation exceeds this value the operator
	// knows the spec changed and resets the job so it can be re-run with the updated configuration.
	LastScheduledGenerationAnnotation = "trustyai.opendatahub.io/last-scheduled-generation"
)
