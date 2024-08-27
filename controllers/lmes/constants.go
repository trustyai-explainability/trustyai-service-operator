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
	DriverPath              = "/bin/driver"
	DestDriverPath          = "/opt/app-root/src/bin/driver"
	PodImageKey             = "lmes-pod-image"
	DriverImageKey          = "lmes-driver-image"
	PodCheckingIntervalKey  = "lmes-pod-checking-interval"
	ImagePullPolicyKey      = "lmes-image-pull-policy"
	GrpcPortKey             = "lmes-grpc-port"
	GrpcServiceKey          = "lmes-grpc-service"
	GrpcServerSecretKey     = "lmes-grpc-server-secret"
	GrpcClientSecretKey     = "lmes-grpc-client-secret"
	DriverReportIntervalKey = "driver-report-interval"
	GrpcServerCertEnv       = "GRPC_SERVER_CERT"
	GrpcServerKeyEnv        = "GRPC_SERVER_KEY"
	GrpcClientCaEnv         = "GRPC_CLIENT_CA"
	// FIXME: move these images to quay.io/trustyai
	DefaultPodImage             = "quay.io/yhwang/ta-lmes-job:latest"
	DefaultDriverImage          = "quay.io/yhwang/ta-lmes-driver:latest"
	DefaultPodCheckingInterval  = time.Second * 10
	DefaultDriverReportInterval = time.Second * 10
	DefaultImagePullPolicy      = corev1.PullAlways
	DefaultGrpcPort             = 8082
	DefaultGrpcService          = "lm-eval-grpc"
	DefaultGrpcServerSecret     = "grpc-server-cert"
	DefaultGrpcClientSecret     = "grpc-client-cert"
	ServiceName                 = "LMES"
)
