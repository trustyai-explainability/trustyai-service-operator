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
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/driver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	isController       = true
	runAsUser    int64 = 1000000
	runAsGroup   int64 = 1000000
)

func Test_SimplePod(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
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
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: defaultSecurityContext,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					SecurityContext: defaultSecurityContext,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
				},
			},
			SecurityContext: defaultPodSecurityContext,
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

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

func Test_WithCustomPod(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
	}
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			UID:       "for-testing",
			Labels: map[string]string{
				"custom/label1": "value1",
				"custom/label2": "value2",
			},
			Annotations: map[string]string{
				"custom/annotation1": "annotation1",
				"custom/annotation2": "annotation2",
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Pod: &lmesv1alpha1.LMEvalPodSpec{
				Container: &lmesv1alpha1.LMEvalContainer{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "additionalVolume",
							MountPath: "/test",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:  &runAsUser,
						RunAsGroup: &runAsGroup,
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "additionalVolume",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "mypvc",
								ReadOnly:  true,
							},
						},
					},
				},
				SecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot: &runAsNonRootUser,
				},
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchFields: []corev1.NodeSelectorRequirement{
										{
											Key:      "node",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"test"},
										},
									},
								},
							},
						},
					},
				},
				SideCars: []corev1.Container{
					{
						Name:    "sidecar1",
						Image:   "busybox",
						Command: []string{"sh", "-ec", "sleep 3600"},
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
				"app.kubernetes.io/name": "ta-lmes",
				"custom/label1":          "value1",
				"custom/label2":          "value2",
			},
			Annotations: map[string]string{
				"custom/annotation1": "annotation1",
				"custom/annotation2": "annotation2",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: defaultSecurityContext,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:  &runAsUser,
						RunAsGroup: &runAsGroup,
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "additionalVolume",
							MountPath: "/test",
						},
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
				},
				{
					Name:    "sidecar1",
					Image:   "busybox",
					Command: []string{"sh", "-ec", "sleep 3600"},
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRootUser,
			},
			Volumes: []corev1.Volume{
				{
					Name: "shared", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "additionalVolume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "mypvc",
							ReadOnly:  true,
						},
					},
				},
			},
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchFields: []corev1.NodeSelectorRequirement{
									{
										Key:      "node",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"test"},
									},
								},
							},
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)

	// with filter
	labelFilterPrefixes = append(labelFilterPrefixes, "custom/label1")
	annotationFilterPrefixes = append(annotationFilterPrefixes, "custom/annotation2")
	expect.Labels = map[string]string{
		"app.kubernetes.io/name": "ta-lmes",
		"custom/label2":          "value2",
	}
	expect.Annotations = map[string]string{
		"custom/annotation1": "annotation1",
	}

	newPod = CreatePod(svcOpts, job, log)
	assert.Equal(t, expect, newPod)
}

func Test_EnvSecretsPod(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
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
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Pod: &lmesv1alpha1.LMEvalPodSpec{
				Container: &lmesv1alpha1.LMEvalContainer{
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
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: defaultSecurityContext,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
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
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					SecurityContext: defaultSecurityContext,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
					},
				},
			},
			SecurityContext: defaultPodSecurityContext,
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

	newPod := CreatePod(svcOpts, job, log)
	// maybe only verify the envs: Containers[0].Env
	assert.Equal(t, expect, newPod)
}

func Test_FileSecretsPod(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
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
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Pod: &lmesv1alpha1.LMEvalPodSpec{
				Container: &lmesv1alpha1.LMEvalContainer{
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "secVol1",
							MountPath: "the_path",
							ReadOnly:  true,
						},
					},
				},
				Volumes: []corev1.Volume{
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
			},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: defaultSecurityContext,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: defaultSecurityContext,
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "secVol1",
							MountPath: "the_path",
							ReadOnly:  true,
						},
					},
				},
			},
			SecurityContext: defaultPodSecurityContext,
			Volumes: []corev1.Volume{
				{
					Name: "shared", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
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

	newPod := CreatePod(svcOpts, job, log)
	// maybe only verify the envs: Containers[0].Env
	assert.Equal(t, expect, newPod)
}

func Test_GenerateArgBatchSize(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:         "podimage:latest",
		DriverImage:      "driver:latest",
		ImagePullPolicy:  corev1.PullAlways,
		MaxBatchSize:     20,
		DefaultBatchSize: "4",
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
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
		},
	}

	// no batchSize in the job, use default batchSize
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "hf", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", svcOpts.DefaultBatchSize,
	}, generateArgs(svcOpts, job, log))

	// exceed the max-batch-size, use max-batch-size
	var biggerBatchSize = "30"
	job.Spec.BatchSize = &biggerBatchSize
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "hf", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", strconv.Itoa(svcOpts.MaxBatchSize),
	}, generateArgs(svcOpts, job, log))

	// normal batchSize
	var normalBatchSize = "16"
	job.Spec.BatchSize = &normalBatchSize
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "hf", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", "16",
	}, generateArgs(svcOpts, job, log))
}

func Test_ChatTemplate(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:         "podimage:latest",
		DriverImage:      "driver:latest",
		ImagePullPolicy:  corev1.PullAlways,
		MaxBatchSize:     20,
		DefaultBatchSize: "4",
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
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
		},
	}

	// no chat template
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", svcOpts.DefaultBatchSize,
	}, generateArgs(svcOpts, job, log))

	// chat template == false
	job.Spec.ChatTemplate = &lmesv1alpha1.ChatTemplate{Enabled: false}
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", svcOpts.DefaultBatchSize,
	}, generateArgs(svcOpts, job, log))

	// chat template == true
	job.Spec.ChatTemplate = &lmesv1alpha1.ChatTemplate{Enabled: true}
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", svcOpts.DefaultBatchSize, "--apply_chat_template",
	}, generateArgs(svcOpts, job, log))

	// chat template == some_template
	job.Spec.ChatTemplate = &lmesv1alpha1.ChatTemplate{Enabled: true, Name: "some_template"}
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", svcOpts.DefaultBatchSize, "--apply_chat_template", "some_template",
	}, generateArgs(svcOpts, job, log))

	// chat template == some with spaces template
	job.Spec.ChatTemplate = &lmesv1alpha1.ChatTemplate{Enabled: true, Name: "some template with spaces"}
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", svcOpts.DefaultBatchSize, "--apply_chat_template", "some template with spaces",
	}, generateArgs(svcOpts, job, log))
}

func Test_SystemInstruction(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:         "podimage:latest",
		DriverImage:      "driver:latest",
		ImagePullPolicy:  corev1.PullAlways,
		MaxBatchSize:     20,
		DefaultBatchSize: "4",
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
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
		},
	}

	// no system instruction
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", svcOpts.DefaultBatchSize,
	}, generateArgs(svcOpts, job, log))

	// system instruction == something
	job.Spec.SystemInstruction = "something"
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", svcOpts.DefaultBatchSize, "--system_instruction", "something",
	}, generateArgs(svcOpts, job, log))

	// system instruction == something with spaces
	job.Spec.SystemInstruction = "something with spaces"
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "test", "--model_args", "arg1=value1", "--tasks", "task1,task2", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", svcOpts.DefaultBatchSize, "--system_instruction", "something with spaces",
	}, generateArgs(svcOpts, job, log))
}

func Test_GenerateArgCmdTaskRecipes(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:         "podimage:latest",
		DriverImage:      "driver:latest",
		ImagePullPolicy:  corev1.PullAlways,
		MaxBatchSize:     Options.MaxBatchSize,
		DefaultBatchSize: Options.DefaultBatchSize,
	}
	var format = "unitxt.format"
	var numDemos = 5
	var demosPoolSize = 10
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
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
				TaskRecipes: []lmesv1alpha1.TaskRecipe{
					{
						Card:          lmesv1alpha1.Card{Name: "unitxt.card1"},
						Template:      &lmesv1alpha1.Template{Name: "unitxt.template"},
						Format:        &format,
						Metrics:       []lmesv1alpha1.Metric{{Name: "unitxt.metric1"}, {Name: "unitxt.metric2"}},
						NumDemos:      &numDemos,
						DemosPoolSize: &demosPoolSize,
					},
				},
			},
		},
	}

	// one TaskRecipe
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "hf", "--model_args", "arg1=value1", "--tasks", "task1,task2,tr_0", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", DefaultBatchSize,
	}, generateArgs(svcOpts, job, log))

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--task-recipe", "card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--",
	}, generateCmd(svcOpts, job))

	job.Spec.TaskList.TaskRecipes = append(job.Spec.TaskList.TaskRecipes,
		lmesv1alpha1.TaskRecipe{
			Card:          lmesv1alpha1.Card{Name: "unitxt.card2"},
			Template:      &lmesv1alpha1.Template{Name: "unitxt.template2"},
			Format:        &format,
			Metrics:       []lmesv1alpha1.Metric{{Name: "unitxt.metric3"}, {Name: "unitxt.metric4"}},
			NumDemos:      &numDemos,
			DemosPoolSize: &demosPoolSize,
		},
	)

	// two task recipes
	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "hf", "--model_args", "arg1=value1", "--tasks", "task1,task2,tr_0,tr_1", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", DefaultBatchSize,
	}, generateArgs(svcOpts, job, log))

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--task-recipe", "card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--task-recipe", "card=unitxt.card2,template=unitxt.template2,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--",
	}, generateCmd(svcOpts, job))
}

func Test_GenerateArgCmdCustomCard(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:         "podimage:latest",
		DriverImage:      "driver:latest",
		ImagePullPolicy:  corev1.PullAlways,
		MaxBatchSize:     Options.MaxBatchSize,
		DefaultBatchSize: Options.DefaultBatchSize,
	}
	var format = "unitxt.format"
	var numDemos = 5
	var demosPoolSize = 10
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
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
				TaskRecipes: []lmesv1alpha1.TaskRecipe{
					{
						Card: lmesv1alpha1.Card{
							Custom: `{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
						},
						Template:      &lmesv1alpha1.Template{Name: "unitxt.template"},
						Format:        &format,
						Metrics:       []lmesv1alpha1.Metric{{Name: "unitxt.metric1"}, {Name: "unitxt.metric2"}},
						NumDemos:      &numDemos,
						DemosPoolSize: &demosPoolSize,
					},
				},
			},
		},
	}

	assert.Equal(t, []string{
		"python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output", "--model", "hf", "--model_args", "arg1=value1", "--tasks", "task1,task2,tr_0", "--include_path", "/opt/app-root/src/my_tasks", "--batch_size", DefaultBatchSize,
	}, generateArgs(svcOpts, job, log))

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--custom-artifact", `card|custom_0|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--",
	}, generateCmd(svcOpts, job))

	// add second task using custom recipe + custom template
	job.Spec.TaskList.TaskRecipes = append(job.Spec.TaskList.TaskRecipes,
		lmesv1alpha1.TaskRecipe{
			Card: lmesv1alpha1.Card{
				Custom: `{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
			},
			Template: &lmesv1alpha1.Template{
				Ref: "tp_0",
			},
			Format:        &format,
			Metrics:       []lmesv1alpha1.Metric{{Name: "unitxt.metric3"}, {Name: "unitxt.metric4"}},
			NumDemos:      &numDemos,
			DemosPoolSize: &demosPoolSize,
		},
	)

	job.Spec.TaskList.CustomArtifacts = &lmesv1alpha1.CustomArtifacts{
		Templates: []lmesv1alpha1.CustomArtifact{
			{
				Name:  "tp_0",
				Value: `{ "__type__": "input_output_template", "instruction": "In the following task, you translate a {text_type}.", "input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.", "target_prefix": "Translation: ", "output_format": "{translation}", "postprocessors": [ "processors.lower_case" ] }`,
			},
		},
	}

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--custom-artifact", `card|custom_0|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-artifact", `card|custom_1|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_1,template=templates.tp_0,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-artifact", `template|tp_0|{ "__type__": "input_output_template", "instruction": "In the following task, you translate a {text_type}.", "input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.", "target_prefix": "Translation: ", "output_format": "{translation}", "postprocessors": [ "processors.lower_case" ] }`,
		"--",
	}, generateCmd(svcOpts, job))

	// add third task using normal card + custom system_prompt
	job.Spec.TaskList.TaskRecipes = append(job.Spec.TaskList.TaskRecipes,
		lmesv1alpha1.TaskRecipe{
			Card: lmesv1alpha1.Card{Name: "unitxt.card"},
			SystemPrompt: &lmesv1alpha1.SystemPrompt{
				Ref: "sp_0",
			},
			Format:        &format,
			Metrics:       []lmesv1alpha1.Metric{{Name: "unitxt.metric4"}, {Name: "unitxt.metric5"}},
			NumDemos:      &numDemos,
			DemosPoolSize: &demosPoolSize,
		},
	)

	job.Spec.TaskList.CustomArtifacts.SystemPrompts = []lmesv1alpha1.CustomArtifact{
		{
			Name:  "sp_0",
			Value: "this is a custom system promp",
		},
	}

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--custom-artifact", `card|custom_0|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-artifact", `card|custom_1|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_1,template=templates.tp_0,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--task-recipe", "card=unitxt.card,system_prompt=system_prompts.sp_0,metrics=[unitxt.metric4,unitxt.metric5],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-artifact", `template|tp_0|{ "__type__": "input_output_template", "instruction": "In the following task, you translate a {text_type}.", "input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.", "target_prefix": "Translation: ", "output_format": "{translation}", "postprocessors": [ "processors.lower_case" ] }`,
		"--custom-artifact", "system_prompt|sp_0|this is a custom system promp",
		"--",
	}, generateCmd(svcOpts, job))

	// add forth task using custom card + custom template + custom system_prompt
	// and reuse the template and system prompt
	job.Spec.TaskList.TaskRecipes = append(job.Spec.TaskList.TaskRecipes,
		lmesv1alpha1.TaskRecipe{
			Card: lmesv1alpha1.Card{
				Custom: `{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
			},
			Template: &lmesv1alpha1.Template{
				Ref: "tp_0",
			},
			SystemPrompt: &lmesv1alpha1.SystemPrompt{
				Ref: "sp_0",
			},
			Format:        &format,
			Metrics:       []lmesv1alpha1.Metric{{Name: "unitxt.metric6"}, {Name: "unitxt.metric7"}},
			NumDemos:      &numDemos,
			DemosPoolSize: &demosPoolSize,
		},
	)

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--custom-artifact", `card|custom_0|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-artifact", `card|custom_1|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_1,template=templates.tp_0,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--task-recipe", "card=unitxt.card,system_prompt=system_prompts.sp_0,metrics=[unitxt.metric4,unitxt.metric5],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-artifact", `card|custom_2|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_2,template=templates.tp_0,system_prompt=system_prompts.sp_0,metrics=[unitxt.metric6,unitxt.metric7],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-artifact", `template|tp_0|{ "__type__": "input_output_template", "instruction": "In the following task, you translate a {text_type}.", "input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.", "target_prefix": "Translation: ", "output_format": "{translation}", "postprocessors": [ "processors.lower_case" ] }`,
		"--custom-artifact", "system_prompt|sp_0|this is a custom system promp",
		"--",
	}, generateCmd(svcOpts, job))

	// add fifth task using regular card + custom template + custom system_prompt
	// both template and system prompt are new
	job.Spec.TaskList.TaskRecipes = append(job.Spec.TaskList.TaskRecipes,
		lmesv1alpha1.TaskRecipe{
			Card: lmesv1alpha1.Card{Name: "unitxt.card2"},
			Template: &lmesv1alpha1.Template{
				Ref: "tp_1",
			},
			SystemPrompt: &lmesv1alpha1.SystemPrompt{
				Ref: "sp_1",
			},
			Format:        &format,
			Metrics:       []lmesv1alpha1.Metric{{Name: "unitxt.metric6"}, {Name: "unitxt.metric7"}},
			NumDemos:      &numDemos,
			DemosPoolSize: &demosPoolSize,
		},
	)

	job.Spec.TaskList.CustomArtifacts.Templates = append(job.Spec.TaskList.CustomArtifacts.Templates, lmesv1alpha1.CustomArtifact{
		Name:  "tp_1",
		Value: `{ "__type__": "input_output_template", "instruction": "2In the following task, you translate a {text_type}.", "input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.", "target_prefix": "Translation: ", "output_format": "{translation}", "postprocessors": [ "processors.lower_case" ] }`,
	})

	job.Spec.TaskList.CustomArtifacts.SystemPrompts = append(job.Spec.TaskList.CustomArtifacts.SystemPrompts, lmesv1alpha1.CustomArtifact{
		Name:  "sp_1",
		Value: "this is a custom system promp2",
	})

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--custom-artifact", `card|custom_0|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-artifact", `card|custom_1|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_1,template=templates.tp_0,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--task-recipe", "card=unitxt.card,system_prompt=system_prompts.sp_0,metrics=[unitxt.metric4,unitxt.metric5],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-artifact", `card|custom_2|{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--task-recipe", "card=cards.custom_2,template=templates.tp_0,system_prompt=system_prompts.sp_0,metrics=[unitxt.metric6,unitxt.metric7],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--task-recipe", "card=unitxt.card2,template=templates.tp_1,system_prompt=system_prompts.sp_1,metrics=[unitxt.metric6,unitxt.metric7],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-artifact", `template|tp_0|{ "__type__": "input_output_template", "instruction": "In the following task, you translate a {text_type}.", "input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.", "target_prefix": "Translation: ", "output_format": "{translation}", "postprocessors": [ "processors.lower_case" ] }`,
		"--custom-artifact", `template|tp_1|{ "__type__": "input_output_template", "instruction": "2In the following task, you translate a {text_type}.", "input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.", "target_prefix": "Translation: ", "output_format": "{translation}", "postprocessors": [ "processors.lower_case" ] }`,
		"--custom-artifact", "system_prompt|sp_0|this is a custom system promp",
		"--custom-artifact", "system_prompt|sp_1|this is a custom system promp2",
		"--",
	}, generateCmd(svcOpts, job))
}

func Test_CustomCardValidation(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
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
			TaskList: lmesv1alpha1.TaskList{
				TaskRecipes: []lmesv1alpha1.TaskRecipe{
					{
						Card: lmesv1alpha1.Card{
							Custom: "invalid JSON",
						},
					},
				},
			},
		},
	}

	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "failed to parse the custom card")

	// no loader
	job.Spec.TaskList.TaskRecipes[0].Card.Custom = `
		{
			"__type__": "task_card",
			"preprocess_steps": [
				{
					"__type__": "copy",
					"field": "translation/en",
					"to_field": "text"
				},
				{
					"__type__": "copy",
					"field": "translation/de",
					"to_field": "translation"
				},
				{
					"__type__": "set",
					"fields": {
						"source_language": "english",
						"target_language": "dutch"
					}
				}
			],
			"task": "tasks.translation.directed",
			"templates": "templates.translation.directed.all"
		}`
	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "missing loader definition")

	// ok
	job.Spec.TaskList.TaskRecipes[0].Card.Custom = `
		{
			"__type__": "task_card",
			"loader": {
				"__type__": "load_hf",
				"path": "wmt16",
				"name": "de-en"
			},
			"preprocess_steps": [
				{
					"__type__": "copy",
					"field": "translation/en",
					"to_field": "text"
				},
				{
					"__type__": "copy",
					"field": "translation/de",
					"to_field": "translation"
				},
				{
					"__type__": "set",
					"fields": {
						"source_language": "english",
						"target_language": "dutch"
					}
				}
			],
			"task": "tasks.translation.directed",
			"templates": "templates.translation.directed.all"
		}`

	assert.Nil(t, lmevalRec.validateCustomRecipes(job, log))

	job.Spec.TaskList.TaskRecipes[0].Template = &lmesv1alpha1.Template{
		Ref: "tp_0",
	}

	// missing custom template
	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "the reference name of the custom template is not defined: tp_0")

	job.Spec.TaskList.CustomArtifacts = &lmesv1alpha1.CustomArtifacts{
		Templates: []lmesv1alpha1.CustomArtifact{
			{
				Name: "tp_0",
				Value: `
					{
						"__type__": "input_output_template",
						"instruction": "In the following task, you translate a {text_type}.",
						"input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.",
						"target_prefix": "Translation: ",
						"output_format": "{translation}",
						"postprocessors": [
							"processors.lower_case"
						]
					}
				`,
			},
		},
	}

	// pass
	assert.Nil(t, lmevalRec.validateCustomRecipes(job, log))

	job.Spec.TaskList.CustomArtifacts.Templates = append(job.Spec.TaskList.CustomArtifacts.Templates, lmesv1alpha1.CustomArtifact{
		Name: "tp_1",
		Value: `
			{
				"__type__": "input_output_template",
				"instruction": "In the following task, you translate a {text_type}.",
				"input_format": "Translate this {text_type} from {source_language} to {target_language}: {text}.",
				"target_prefix": "Translation: ",
				"postprocessors": [
					"processors.lower_case"
				]
			}
		`,
	})

	job.Spec.TaskList.TaskRecipes[0].Template = &lmesv1alpha1.Template{
		Ref: "tp_1",
	}

	// missing outout_format property
	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "missing output_format definition")
}

func Test_CustomMetrics(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
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
			TaskList: lmesv1alpha1.TaskList{
				TaskRecipes: []lmesv1alpha1.TaskRecipe{
					{
						Card: lmesv1alpha1.Card{
							Name: "cards.wnli",
						},
						Template: &lmesv1alpha1.Template{
							Name: "templates.classification.multi_class.relation.default",
						},
						Metrics: []lmesv1alpha1.Metric{
							{
								Ref: "my-metric",
							},
						},
					},
				},
				CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
					Metrics: []lmesv1alpha1.CustomArtifact{
						{Name: "no-my-metric", Value: "invalid"},
					},
				},
			},
		},
	}

	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "the reference name of the custom metric is not defined: my-metric")

	// invalid JSON object
	job.Spec.TaskList.CustomArtifacts.Metrics[0].Name = "my-metric"

	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "failed to parse the custom metric: my-metric")

	// no loader
	job.Spec.TaskList.CustomArtifacts.Metrics[0].Value = `
		{
			"missing": "__type__ property"
		}`

	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "missing __type__ definition")

	// ok
	job.Spec.TaskList.CustomArtifacts.Metrics[0].Value = `
		{
			"__type__": "llm_as_judge",
			"inference_model": {
				"__type__": "hf_pipeline_based_inference_engine",
				"model_name": "mistralai/Mistral-7B-Instruct-v0.2",
				"max_new_tokens": 256,
				"use_fp16": true
			},
			"template": "templates.response_assessment.rating.mt_bench_single_turn",
			"task": "rating.single_turn",
			"format": "formats.models.mistral.instruction",
			"main_score": "mistral_7b_instruct_v0_2_huggingface_template_mt_bench_single_turn"
		}`

	//ok
	assert.Nil(t, lmevalRec.validateCustomRecipes(job, log))
}

func Test_CustomTask(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
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
			TaskList: lmesv1alpha1.TaskList{
				TaskRecipes: []lmesv1alpha1.TaskRecipe{
					{
						Card: lmesv1alpha1.Card{
							Name: "cards.wnli",
						},
						Template: &lmesv1alpha1.Template{
							Name: "templates.classification.multi_class.relation.default",
						},
						Task: &lmesv1alpha1.Task{
							Ref: "mytask",
						},
					},
				},
				CustomArtifacts: &lmesv1alpha1.CustomArtifacts{
					Tasks: []lmesv1alpha1.CustomArtifact{
						{Name: "mytask-noexist", Value: "invalid"},
					},
				},
			},
		},
	}

	// no custom task
	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "the reference name of the custom task is not defined: mytask")

	job.Spec.TaskList.CustomArtifacts.Tasks[0].Name = "mytask"
	// invalid JSON object
	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "failed to parse the custom task:mytask")

	// no __type__
	job.Spec.TaskList.CustomArtifacts.Tasks[0].Value = `
		{
			"missing": "__type__ property"
		}`

	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "missing __type__ definition")

	// no input_fields
	job.Spec.TaskList.CustomArtifacts.Tasks[0].Value = `
		{
			"__type__": "task"
		}`

	assert.ErrorContains(t, lmevalRec.validateCustomRecipes(job, log), "missing input_fields definition")

	// ok
	job.Spec.TaskList.CustomArtifacts.Tasks[0].Value = `
		{
			"__type__": "task",
			"input_fields": {
				"input": "str",
				"type_of_input": "str",
				"type_of_output": "str"
			},
			"reference_fields": {
				"output": "str"
			},
			"prediction_type": "str",
			"metrics": [
				"metrics.normalized_sacrebleu"
			],
			"augmentable_inputs": [
				"input"
			],
			"defaults": {
				"type_of_output": "Text"
			}
		}`

	//ok
	assert.Nil(t, lmevalRec.validateCustomRecipes(job, log))
}

func Test_ConcatTasks(t *testing.T) {
	tasks := concatTasks(lmesv1alpha1.TaskList{
		TaskNames: []string{"task1", "task2"},
		TaskRecipes: []lmesv1alpha1.TaskRecipe{
			{Template: &lmesv1alpha1.Template{Name: "template3"}, Card: lmesv1alpha1.Card{Name: "format3"}},
		},
	})

	assert.Equal(t, []string{"task1", "task2", driver.TaskRecipePrefix + "_0"}, tasks)
}

func Test_ManagedPVC(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
	}

	jobName := "test"
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Outputs: &lmesv1alpha1.Outputs{
				PersistentVolumeClaimManaged: &lmesv1alpha1.PersistentVolumeClaimManaged{
					Size: "5Gi",
				},
			},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: defaultSecurityContext,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: defaultSecurityContext,
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},

					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "outputs",
							MountPath: "/opt/app-root/src/output",
						},
					},
				},
			},
			SecurityContext: defaultPodSecurityContext,
			Volumes: []corev1.Volume{
				{
					Name: "shared", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "outputs", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: jobName + "-pvc",
							ReadOnly:  false,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

func Test_ExistingPVC(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
	}

	jobName := "test"
	pvcName := "my-pvc"
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "local-completions",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Outputs: &lmesv1alpha1.Outputs{
				PersistentVolumeClaimName: &pvcName,
			},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: defaultSecurityContext,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: defaultSecurityContext,
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "outputs",
							MountPath: "/opt/app-root/src/output",
						},
					},
				},
			},
			SecurityContext: defaultPodSecurityContext,
			Volumes: []corev1.Volume{
				{
					Name: "shared", VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "outputs", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
							ReadOnly:  false,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

// Test_PVCPreference tests that if both PVC modes are specified, managed PVC will be preferred and existing PVC will be ignored
func Test_PVCPreference(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
	}

	jobName := "test"
	pvcName := "my-pvc"
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "local-completions",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Outputs: &lmesv1alpha1.Outputs{
				PersistentVolumeClaimName: &pvcName,
				PersistentVolumeClaimManaged: &lmesv1alpha1.PersistentVolumeClaimManaged{
					Size: "5Gi",
				},
			},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "outputs",
							MountPath: "/opt/app-root/src/output",
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
					Name: "outputs", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: jobName + "-pvc",
							ReadOnly:  false,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

// Test_OfflineMode tests that if the offline mode is set the configuration is correct
func Test_OfflineMode(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
	}

	jobName := "test"
	pvcName := "my-pvc"
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "local-completions",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: &pvcName,
				},
			},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "offline",
							MountPath: "/opt/app-root/src/hf_home",
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
					Name: "offline", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
							ReadOnly:  false,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

// Test_ProtectedVars tests that if the protected env vars are set from spec.pod mode
// they will not be changed in the pod
func Test_ProtectedVars(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
	}

	jobName := "test"
	pvcName := "my-pvc"
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: &pvcName,
				},
			},
			Pod: &lmesv1alpha1.LMEvalPodSpec{
				Container: &lmesv1alpha1.LMEvalContainer{
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "0",
						},
						{
							Name:  "NOT_PROTECTED",
							Value: "True",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "1",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "True",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "1",
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
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "NOT_PROTECTED",
							Value: "True",
						},
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},

						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "offline",
							MountPath: "/opt/app-root/src/hf_home",
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
					Name: "offline", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
							ReadOnly:  false,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

// Test_OnlineModeDisabled tests that if the online mode is set, but the controller disables it
// it will still run in offline mode
func Test_OnlineModeDisabled(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:           "podimage:latest",
		DriverImage:        "driver:latest",
		ImagePullPolicy:    corev1.PullAlways,
		AllowOnline:        false,
		AllowCodeExecution: false,
	}

	jobName := "test"
	pvcName := "my-pvc"
	allowOnline := true
	allowCodeExecution := true
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			AllowOnline:        &allowOnline,
			AllowCodeExecution: &allowCodeExecution,
			Model:              "local-completions",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: &pvcName,
				},
			},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "offline",
							MountPath: "/opt/app-root/src/hf_home",
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
					Name: "offline", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
							ReadOnly:  false,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

// Test_OnlineMode tests that if the online mode is set the configuration is correct
func Test_OnlineMode(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
		AllowOnline:     true,
	}

	allowOnline := true
	jobName := "test"
	pvcName := "my-pvc"
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: &pvcName,
				},
			},
			AllowOnline: &allowOnline,
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "offline",
							MountPath: "/opt/app-root/src/hf_home",
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
					Name: "offline", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
							ReadOnly:  false,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

// Test_AllowCodeOnlineMode tests that if the online mode and allow code is set the configuration is correct
func Test_AllowCodeOnlineMode(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:           "podimage:latest",
		DriverImage:        "driver:latest",
		ImagePullPolicy:    corev1.PullAlways,
		AllowOnline:        true,
		AllowCodeExecution: true,
	}

	jobName := "test"
	pvcName := "my-pvc"
	allowOnline := true
	allowCode := true
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: &pvcName,
				},
			},
			AllowOnline:        &allowOnline,
			AllowCodeExecution: &allowCode,
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "1",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "1",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "True",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "1",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "offline",
							MountPath: "/opt/app-root/src/hf_home",
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
					Name: "offline", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
							ReadOnly:  false,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

// Test_AllowCodeOfflineMode tests that if the online mode is set the configuration is correct
func Test_AllowCodeOfflineMode(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:           "podimage:latest",
		DriverImage:        "driver:latest",
		ImagePullPolicy:    corev1.PullAlways,
		AllowOnline:        true,
		AllowCodeExecution: true,
	}

	jobName := "test"
	pvcName := "my-pvc"
	allowCode := true
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: &pvcName,
				},
			},
			AllowCodeExecution: &allowCode,
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "1",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "1",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "True",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "1",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "offline",
							MountPath: "/opt/app-root/src/hf_home",
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
					Name: "offline", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
							ReadOnly:  false,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

// Test_OfflineModeWithOutput tests that if the offline mode is set the configuration is correct, even when custom output is set
func Test_OfflineModeWithOutput(t *testing.T) {
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
	}

	jobName := "test"
	offlinePvcName := "offline-pvc"
	outputPvcName := "output-pvc"
	var job = &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: "default",
			UID:       "for-testing",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       lmesv1alpha1.KindName,
			APIVersion: lmesv1alpha1.Version,
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: &offlinePvcName,
				},
			},
			Outputs: &lmesv1alpha1.Outputs{
				PersistentVolumeClaimName: &outputPvcName,
			},
		},
	}

	expect := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "ta-lmes",
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
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
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
					Image:           svcOpts.PodImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         generateCmd(svcOpts, job),
					Args:            generateArgs(svcOpts, job, log),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: int32(svcOpts.DriverPort),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "HF_HUB_DISABLE_TELEMETRY",
							Value: "1",
						},
						{
							Name:  "DO_NOT_TRACK",
							Value: "1",
						},
						{
							Name:  "TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
							Value: "0",
						},
						{
							Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
							Value: "False",
						},
						{
							Name:  "HF_ALLOW_CODE_EVAL",
							Value: "0",
						},
						{
							Name:  "HF_DATASETS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_HUB_OFFLINE",
							Value: "1",
						},
						{
							Name:  "TRANSFORMERS_OFFLINE",
							Value: "1",
						},
						{
							Name:  "HF_EVALUATE_OFFLINE",
							Value: "1",
						},
						{
							Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
							Value: "True",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
						{
							Name:      "outputs",
							MountPath: "/opt/app-root/src/output",
						},
						{
							Name:      "offline",
							MountPath: "/opt/app-root/src/hf_home",
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
					Name: "outputs", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: outputPvcName,
							ReadOnly:  false,
						},
					},
				},
				{
					Name: "offline", VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: offlinePvcName,
							ReadOnly:  false,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := CreatePod(svcOpts, job, log)

	assert.Equal(t, expect, newPod)
}

func Test_CustomTasksGitSource(t *testing.T) {
	const namespace = "default"

	// Create service options
	log := log.FromContext(context.Background())
	svcOpts := &serviceOptions{
		PodImage:        "podimage:latest",
		DriverImage:     "driver:latest",
		ImagePullPolicy: corev1.PullAlways,
	}

	testCases := []struct {
		name                string
		customTaskGitURL    string
		customTaskGitBranch *string
		customTaskGitCommit *string
		customTaskGitPath   string
		taskNames           []string
		expectedBranch      string
		expectedCommit      string
		expectError         bool
		errorContains       string
	}{
		{
			name:                "Complete git example",
			customTaskGitURL:    "https://github.com/trustyai-explainability/repo.git",
			customTaskGitBranch: ptr.To("lmeval-tasks"),
			customTaskGitCommit: ptr.To("abcdef1234567890abcdef1234567890abcdef12"),
			customTaskGitPath:   "path/to/tasks",
			taskNames:           []string{"custom_task1", "custom_task2"},
			expectedBranch:      "lmeval-tasks",
			expectedCommit:      "abcdef1234567890abcdef1234567890abcdef12",
			expectError:         false,
		},
		{
			name:                "Default branch",
			customTaskGitURL:    "https://github.com/trustyai-explainability/repo.git",
			customTaskGitBranch: nil,
			customTaskGitCommit: ptr.To("abcdef1234567890abcdef1234567890abcdef12"),
			customTaskGitPath:   "path/to/tasks",
			taskNames:           []string{"custom_task1"},
			expectedBranch:      "",
			expectedCommit:      "abcdef1234567890abcdef1234567890abcdef12",
			expectError:         false,
		},
		{
			name:                "Default commit",
			customTaskGitURL:    "https://github.com/trustyai-explainability/repo.git",
			customTaskGitBranch: ptr.To("lmeval-tasks"),
			customTaskGitCommit: nil,
			customTaskGitPath:   "path/to/tasks",
			taskNames:           []string{"custom_task1"},
			expectedBranch:      "lmeval-tasks",
			expectedCommit:      "",
			expectError:         false,
		},
		{
			name:                "Default branch and commit",
			customTaskGitURL:    "https://github.com/trustyai-explainability/repo.git",
			customTaskGitBranch: nil,
			customTaskGitCommit: nil,
			customTaskGitPath:   "path/to/tasks",
			taskNames:           []string{"custom_task1"},
			expectedBranch:      "",
			expectedCommit:      "",
			expectError:         false,
		},
		{
			name:                "Short commit hash",
			customTaskGitURL:    "https://github.com/trustyai-explainability/repo.git",
			customTaskGitBranch: ptr.To("main"),
			customTaskGitCommit: ptr.To("abcdef1"),
			customTaskGitPath:   "path/to/tasks",
			taskNames:           []string{"custom_task1"},
			expectedBranch:      "main",
			expectedCommit:      "abcdef1",
			expectError:         false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := &lmesv1alpha1.LMEvalJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-custom-tasks-job",
					Namespace: namespace,
				},
				Spec: lmesv1alpha1.LMEvalJobSpec{
					Model: "test-model",
					TaskList: lmesv1alpha1.TaskList{
						TaskNames: tc.taskNames,
						CustomTasks: &lmesv1alpha1.CustomTasks{
							Source: lmesv1alpha1.CustomTaskSource{
								GitSource: lmesv1alpha1.GitSource{
									URL:    tc.customTaskGitURL,
									Branch: tc.customTaskGitBranch,
									Commit: tc.customTaskGitCommit,
									Path:   tc.customTaskGitPath,
								},
							},
						},
					},
				},
			}

			pod := CreatePod(svcOpts, job, log)

			require.NotNil(t, pod)

			assert.Equal(t, job.Name, pod.Name)

			found := false
			for _, container := range pod.Spec.Containers {
				if container.Name == "main" {
					cmdLine := strings.Join(container.Command, " ")
					if strings.Contains(cmdLine, "--custom-task-git-url") {
						found = true
					}

					argsLine := strings.Join(container.Args, " ")
					for _, taskName := range tc.taskNames {
						assert.Contains(t, argsLine, taskName, "Task name should be included in command args")
					}

					break
				}
			}
			assert.True(t, found, "Custom task git configuration should be included in container command")

			volumeMountFound := false
			for _, container := range pod.Spec.Containers {
				if container.Name == "main" {
					for _, volumeMount := range container.VolumeMounts {
						if volumeMount.Name == "shared" {
							volumeMountFound = true
							break
						}
					}
					break
				}
			}
			assert.True(t, volumeMountFound, "Container should have a volume mount for git operations")
		})
	}
}

func Test_CustomTasksGitSourceOfflineMode(t *testing.T) {
	svcOpts := &serviceOptions{
		PodImage:            "test/pod:latest",
		DriverImage:         "test/driver:latest",
		PodCheckingInterval: 5 * time.Second,
		ImagePullPolicy:     corev1.PullIfNotPresent,
		DefaultBatchSize:    DefaultBatchSize,
		MaxBatchSize:        DefaultMaxBatchSize,
		DetectDevice:        false,
		AllowOnline:         false, // Offline mode
		AllowCodeExecution:  false,
		DriverPort:          driver.DefaultPort,
	}

	origOptions := Options
	Options = svcOpts
	defer func() { Options = origOptions }()

	allowOnline := false
	job := &lmesv1alpha1.LMEvalJob{
		TypeMeta: v1.TypeMeta{
			Kind:       "LMEvalJob",
			APIVersion: "trustyai.opendatahub.io/v1alpha1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-job",
			Namespace: "test-ns",
		},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model: "hf",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "pretrained", Value: "hf/model"},
			},
			AllowOnline: &allowOnline,
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"custom_task1", "custom_task2"},
				CustomTasks: &lmesv1alpha1.CustomTasks{
					Source: lmesv1alpha1.CustomTaskSource{
						GitSource: lmesv1alpha1.GitSource{
							URL:  "https://github.com/trustyai-explainability/custom-tasks.git",
							Path: "tasks",
						},
					},
				},
			},
		},
	}

	logger := logr.Discard()

	pod := CreatePod(Options, job, logger)

	if pod == nil {
		t.Fatal("pod should not be nil")
	}

	mainContainer := getContainer(pod)
	if mainContainer == nil {
		t.Fatal("Main container not found")
	}

	if len(mainContainer.Command) == 0 || mainContainer.Command[0] != DestDriverPath {
		t.Fatalf("Expected main container command to start with %s, got %v", DestDriverPath, mainContainer.Command)
	}

	hasGitURL := ContainsStr(mainContainer.Command, "--custom-task-git-url")
	if !hasGitURL {
		t.Fatal("Expected --custom-task-git-url parameter not found")
	}

	expectedOfflineVars := []string{
		"HF_DATASETS_OFFLINE=1",
		"HF_HUB_OFFLINE=1",
		"TRANSFORMERS_OFFLINE=1",
		"HF_EVALUATE_OFFLINE=1",
		"UNITXT_USE_ONLY_LOCAL_CATALOGS=True",
	}

	for _, expectedVar := range expectedOfflineVars {
		found := false
		for _, envVar := range mainContainer.Env {
			if fmt.Sprintf("%s=%s", envVar.Name, envVar.Value) == expectedVar {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected environment variable %s not found", expectedVar)
		}
	}

	cmdArgs := strings.Join(mainContainer.Command, " ")
	if !strings.Contains(cmdArgs, "--custom-task-git-url") {
		t.Error("Expected git URL in command args not found")
	}
	if !strings.Contains(cmdArgs, "--task-name custom_task1") {
		t.Error("Expected task name custom_task1 in command args not found")
	}
	if !strings.Contains(cmdArgs, "--task-name custom_task2") {
		t.Error("Expected task name custom_task2 in command args not found")
	}

	if !ContainsStr(job.Spec.TaskList.TaskNames, "custom_task1") {
		t.Error("Expected custom task name custom_task1 in job spec not found")
	}
	if !ContainsStr(job.Spec.TaskList.TaskNames, "custom_task2") {
		t.Error("Expected custom task name custom_task2 in job spec not found")
	}
}

func getContainer(pod *corev1.Pod) *corev1.Container {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "main" {
			return &pod.Spec.Containers[i]
		}
	}
	return nil
}

func ContainsStr(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

func Test_ControllerIntegration(t *testing.T) {
	t.Run("ValidationCalledInController", func(t *testing.T) {
		// This test verifies that validation is properly integrated into the controller
		ctx := context.Background()
		log := log.FromContext(ctx)

		// Create an invalid job that should fail validation
		invalidJob := &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-job",
				Namespace: "test",
			},
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf; echo pwned",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"task"},
				},
			},
		}

		// Test that ValidateUserInput rejects the invalid job
		err := ValidateUserInput(invalidJob)
		assert.Error(t, err, "Controller validation should reject invalid job")
		assert.Contains(t, err.Error(), "invalid model", "Should mention model validation failure")

		// Create a safe job that should pass validation
		safeJob := &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "safe-job",
				Namespace: "test",
			},
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model: "hf",
				TaskList: lmesv1alpha1.TaskList{
					TaskNames: []string{"winogrande"},
				},
			},
		}

		// Test that ValidateUserInput accepts the safe job
		err = ValidateUserInput(safeJob)
		assert.NoError(t, err, "Controller validation should accept safe job")

		// Test command generation for the safe job
		svcOpts := &serviceOptions{
			DefaultBatchSize: "auto",
		}
		args := generateArgs(svcOpts, safeJob, log)
		assert.Greater(t, len(args), 0, "Should generate command arguments")
		assert.Equal(t, "python", args[0], "Should start with python")
	})
}

func Test_AllowCodeExecution(t *testing.T) {
	ctx := context.Background()
	log := log.FromContext(ctx)

	svcOpts := &serviceOptions{DefaultBatchSize: "1"}
	allowCode := true
	job := &lmesv1alpha1.LMEvalJob{
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model:              "hf",
			TaskList:           lmesv1alpha1.TaskList{TaskNames: []string{"task1"}},
			AllowCodeExecution: &allowCode,
		},
	}

	args := generateArgs(svcOpts, job, log)
	assert.Contains(t, args, "--confirm_run_unsafe_code")
}
