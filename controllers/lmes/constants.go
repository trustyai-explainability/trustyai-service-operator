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
	DefaultPodImage            = "quay.io/trustyai/ta-lmes-job:latest"
	DefaultDriverImage         = "quay.io/trustyai/ta-lmes-driver:latest"
	DefaultPodCheckingInterval = time.Second * 10
	DefaultImagePullPolicy     = corev1.PullAlways
	DefaultMaxBatchSize        = 24
	DefaultBatchSize           = "1"
	DefaultDetectDevice        = true
	ServiceName                = "LMES"
)
