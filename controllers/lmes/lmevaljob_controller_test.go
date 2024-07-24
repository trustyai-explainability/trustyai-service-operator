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
	"testing"

	"github.com/stretchr/testify/assert"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	isController                   = true
	allowPrivilegeEscalation       = false
	runAsNonRootUser               = true
	runAsUser                int64 = 1001030000
	secretMode               int32 = 420
)

func Test_SimplePod(t *testing.T) {
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:        "podimage:latest",
			DriverImage:     "driver:latest",
			ImagePullPolicy: corev1.PullAlways,
			GrpcPort:        8088,
			GrpcService:     "grpc-service",
		},
	}
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			Tasks: []string{"task1", "task2"},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "lm-eval-service",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: lmesv1alpha1.Version,
					Kind:       lmesv1alpha1.KindName,
					Name:       "test",
					Controller: &isController,
					UID:        "for-testing",
				},
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:            "driver",
					Image:           lmevalRec.options.DriverImage,
					ImagePullPolicy: lmevalRec.options.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "main",
					Image:           lmevalRec.options.PodImage,
					ImagePullPolicy: lmevalRec.options.ImagePullPolicy,
					Env:             []corev1.EnvVar{},
					Command:         lmevalRec.generateCmd(job),
					Args:            generateArgs(job),
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
					},
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRootUser,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "shared", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := lmevalRec.createPod(job)

	assert.Equal(t, expect, newPod)
}

func Test_GrpcMTlsPod(t *testing.T) {
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:         "podimage:latest",
			DriverImage:      "driver:latest",
			ImagePullPolicy:  corev1.PullAlways,
			GrpcPort:         8088,
			GrpcService:      "grpc-service",
			GrpcServerSecret: "server-secret",
			GrpcClientSecret: "client-secret",
			grpcTLSMode:      TLSMode_mTLS,
		},
	}
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			Tasks: []string{"task1", "task2"},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "lm-eval-service",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: lmesv1alpha1.Version,
					Kind:       lmesv1alpha1.KindName,
					Name:       "test",
					Controller: &isController,
					UID:        "for-testing",
				},
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:            "driver",
					Image:           lmevalRec.options.DriverImage,
					ImagePullPolicy: lmevalRec.options.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "main",
					Image:           lmevalRec.options.PodImage,
					ImagePullPolicy: lmevalRec.options.ImagePullPolicy,
					Env: []corev1.EnvVar{
						{
							Name:  "GRPC_CLIENT_KEY",
							Value: "/tmp/k8s-grpc-client/certs/tls.key",
						},
						{
							Name:  "GRPC_CLIENT_CERT",
							Value: "/tmp/k8s-grpc-client/certs/tls.crt",
						},
						{
							Name:  "GRPC_SERVER_CA",
							Value: "/tmp/k8s-grpc-server/certs/ca.crt",
						},
					},
					Command: lmevalRec.generateCmd(job),
					Args:    generateArgs(job),
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "client-cert",
							MountPath: "/tmp/k8s-grpc-client/certs",
							ReadOnly:  true,
						},
						{
							Name:      "server-cert",
							MountPath: "/tmp/k8s-grpc-server/certs",
							ReadOnly:  true,
						},
					},
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRootUser,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "shared", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "client-cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName:  lmevalRec.options.GrpcClientSecret,
							DefaultMode: &secretMode,
						},
					},
				},
				{
					Name: "server-cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName:  lmevalRec.options.GrpcServerSecret,
							DefaultMode: &secretMode,
							Items: []corev1.KeyToPath{
								{Key: "ca.crt", Path: "ca.crt"},
							},
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := lmevalRec.createPod(job)

	assert.Equal(t, expect, newPod)
}

func Test_EnvSecretsPod(t *testing.T) {
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:         "podimage:latest",
			DriverImage:      "driver:latest",
			ImagePullPolicy:  corev1.PullAlways,
			GrpcPort:         8088,
			GrpcService:      "grpc-service",
			GrpcServerSecret: "server-secret",
			GrpcClientSecret: "client-secret",
			grpcTLSMode:      TLSMode_mTLS,
		},
	}
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			Tasks: []string{"task1", "task2"},
			EnvSecrets: []lmesv1alpha1.EnvSecret{
				{
					Env: "my_env",
					SecretRef: &corev1.SecretKeySelector{
						Key: "my-key",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "my-secret",
						},
					},
				},
			},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "lm-eval-service",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: lmesv1alpha1.Version,
					Kind:       lmesv1alpha1.KindName,
					Name:       "test",
					Controller: &isController,
					UID:        "for-testing",
				},
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:            "driver",
					Image:           lmevalRec.options.DriverImage,
					ImagePullPolicy: lmevalRec.options.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "main",
					Image:           lmevalRec.options.PodImage,
					ImagePullPolicy: lmevalRec.options.ImagePullPolicy,
					Env: []corev1.EnvVar{
						{
							Name: "my_env",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									Key: "my-key",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "my-secret",
									},
								},
							},
						},
						{
							Name:  "GRPC_CLIENT_KEY",
							Value: "/tmp/k8s-grpc-client/certs/tls.key",
						},
						{
							Name:  "GRPC_CLIENT_CERT",
							Value: "/tmp/k8s-grpc-client/certs/tls.crt",
						},
						{
							Name:  "GRPC_SERVER_CA",
							Value: "/tmp/k8s-grpc-server/certs/ca.crt",
						},
					},
					Command: lmevalRec.generateCmd(job),
					Args:    generateArgs(job),
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "client-cert",
							MountPath: "/tmp/k8s-grpc-client/certs",
							ReadOnly:  true,
						},
						{
							Name:      "server-cert",
							MountPath: "/tmp/k8s-grpc-server/certs",
							ReadOnly:  true,
						},
					},
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRootUser,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "shared", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "client-cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName:  lmevalRec.options.GrpcClientSecret,
							DefaultMode: &secretMode,
						},
					},
				},
				{
					Name: "server-cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName:  lmevalRec.options.GrpcServerSecret,
							DefaultMode: &secretMode,
							Items: []corev1.KeyToPath{
								{Key: "ca.crt", Path: "ca.crt"},
							},
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := lmevalRec.createPod(job)
	// maybe only verify the envs: Containers[0].Env
	assert.Equal(t, expect, newPod)
}

func Test_FileSecretsPod(t *testing.T) {
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:         "podimage:latest",
			DriverImage:      "driver:latest",
			ImagePullPolicy:  corev1.PullAlways,
			GrpcPort:         8088,
			GrpcService:      "grpc-service",
			GrpcServerSecret: "server-secret",
			GrpcClientSecret: "client-secret",
			grpcTLSMode:      TLSMode_mTLS,
		},
	}
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			Tasks: []string{"task1", "task2"},
			FileSecrets: []lmesv1alpha1.FileSecret{
				{
					MountPath: "the_path",
					SecretRef: corev1.SecretVolumeSource{
						SecretName: "my-secret",
						Items: []corev1.KeyToPath{
							{
								Key:  "key1",
								Path: "path1",
							},
						},
					},
				},
			},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "lm-eval-service",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: lmesv1alpha1.Version,
					Kind:       lmesv1alpha1.KindName,
					Name:       "test",
					Controller: &isController,
					UID:        "for-testing",
				},
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:            "driver",
					Image:           lmevalRec.options.DriverImage,
					ImagePullPolicy: lmevalRec.options.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "main",
					Image:           lmevalRec.options.PodImage,
					ImagePullPolicy: lmevalRec.options.ImagePullPolicy,
					Env: []corev1.EnvVar{
						{
							Name:  "GRPC_CLIENT_KEY",
							Value: "/tmp/k8s-grpc-client/certs/tls.key",
						},
						{
							Name:  "GRPC_CLIENT_CERT",
							Value: "/tmp/k8s-grpc-client/certs/tls.crt",
						},
						{
							Name:  "GRPC_SERVER_CA",
							Value: "/tmp/k8s-grpc-server/certs/ca.crt",
						},
					},
					Command: lmevalRec.generateCmd(job),
					Args:    generateArgs(job),
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "client-cert",
							MountPath: "/tmp/k8s-grpc-client/certs",
							ReadOnly:  true,
						},
						{
							Name:      "server-cert",
							MountPath: "/tmp/k8s-grpc-server/certs",
							ReadOnly:  true,
						},
						{
							Name:      "secVol1",
							MountPath: "the_path",
							ReadOnly:  true,
						},
					},
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRootUser,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "shared", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "client-cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName:  lmevalRec.options.GrpcClientSecret,
							DefaultMode: &secretMode,
						},
					},
				},
				{
					Name: "server-cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName:  lmevalRec.options.GrpcServerSecret,
							DefaultMode: &secretMode,
							Items: []corev1.KeyToPath{
								{Key: "ca.crt", Path: "ca.crt"},
							},
						},
					},
				},
				{
					Name: "secVol1",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "my-secret",
							Items: []corev1.KeyToPath{
								{
									Key:  "key1",
									Path: "path1",
								},
							},
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := lmevalRec.createPod(job)
	// maybe only verify the envs: Containers[0].Env
	assert.Equal(t, expect, newPod)
}
