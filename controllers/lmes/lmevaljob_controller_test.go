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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/driver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			Model: "test",
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
			Model: "test",
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
			Model: "test",
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
			Model: "test",
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
			Model: "test",
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
		"sh", "-ec",
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2 --include_path /opt/app-root/src/my_tasks --batch_size " + svcOpts.DefaultBatchSize,
	}, generateArgs(svcOpts, job, log))

	// exceed the max-batch-size, use max-batch-size
	var biggerBatchSize = "30"
	job.Spec.BatchSize = &biggerBatchSize
	assert.Equal(t, []string{
		"sh", "-ec",
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2 --include_path /opt/app-root/src/my_tasks --batch_size " + strconv.Itoa(svcOpts.MaxBatchSize),
	}, generateArgs(svcOpts, job, log))

	// normal batchSize
	var normalBatchSize = "16"
	job.Spec.BatchSize = &normalBatchSize
	assert.Equal(t, []string{
		"sh", "-ec",
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2 --include_path /opt/app-root/src/my_tasks --batch_size 16",
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
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
				TaskRecipes: []lmesv1alpha1.TaskRecipe{
					{
						Card:          lmesv1alpha1.Card{Name: "unitxt.card1"},
						Template:      "unitxt.template",
						Format:        &format,
						Metrics:       []string{"unitxt.metric1", "unitxt.metric2"},
						NumDemos:      &numDemos,
						DemosPoolSize: &demosPoolSize,
					},
				},
			},
		},
	}

	// one TaskRecipe
	assert.Equal(t, []string{
		"sh", "-ec",
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2,tr_0 --include_path /opt/app-root/src/my_tasks --batch_size " + DefaultBatchSize,
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
			Template:      "unitxt.template2",
			Format:        &format,
			Metrics:       []string{"unitxt.metric3", "unitxt.metric4"},
			NumDemos:      &numDemos,
			DemosPoolSize: &demosPoolSize,
		},
	)

	// two task recipes
	// one TaskRecipe
	assert.Equal(t, []string{
		"sh", "-ec",
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2,tr_0,tr_1 --include_path /opt/app-root/src/my_tasks --batch_size " + DefaultBatchSize,
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
			Model: "test",
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
						Template:      "unitxt.template",
						Format:        &format,
						Metrics:       []string{"unitxt.metric1", "unitxt.metric2"},
						NumDemos:      &numDemos,
						DemosPoolSize: &demosPoolSize,
					},
				},
			},
		},
	}

	assert.Equal(t, []string{
		"sh", "-ec",
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2,tr_0 --include_path /opt/app-root/src/my_tasks --batch_size " + DefaultBatchSize,
	}, generateArgs(svcOpts, job, log))

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--task-recipe", "card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-card", `{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "dutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
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

	assert.ErrorContains(t, lmevalRec.validateCustomCard(job, log), "custom card is not a valid JSON string")

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
	assert.ErrorContains(t, lmevalRec.validateCustomCard(job, log), "no loader definition in the custom card")

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

	assert.Nil(t, lmevalRec.validateCustomCard(job, log))
}

func Test_ConcatTasks(t *testing.T) {
	tasks := concatTasks(lmesv1alpha1.TaskList{
		TaskNames: []string{"task1", "task2"},
		TaskRecipes: []lmesv1alpha1.TaskRecipe{
			{Template: "template3", Card: lmesv1alpha1.Card{Name: "format3"}},
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
			Model: "test",
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
			Model: "test",
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
			Model: "test",
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

func Test_ValidateBatchSize(t *testing.T) {
	maxBatchSize := 32
	logger := log.Log.WithName("tests")
	scenarios := []struct {
		provided  string
		validated string
	}{
		{"5", "5"},
		{"auto", "auto"},
		{"auto:3", "auto:3"},
		{"auto:0", "auto:" + strconv.Itoa(maxBatchSize)},
		{"auto:-5", "auto:" + strconv.Itoa(maxBatchSize)},
		{"64", strconv.Itoa(maxBatchSize)},
		{"-5", DefaultBatchSize},
		{"invalid", DefaultBatchSize},
		{"0", DefaultBatchSize},
		{"auto:auto", "auto:" + strconv.Itoa(maxBatchSize)},
	}

	for _, scenario := range scenarios {
		result := validateBatchSize(scenario.provided, maxBatchSize, logger)
		if result != scenario.validated {
			t.Errorf("validateBatchSize(%q) = %q; want %q", scenario.provided, result, scenario.validated)
		}
	}
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
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: pvcName,
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
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: pvcName,
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
			Model:              "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: pvcName,
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
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: pvcName,
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
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: pvcName,
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
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: pvcName,
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
			Model: "test",
			ModelArgs: []lmesv1alpha1.Arg{
				{Name: "arg1", Value: "value1"},
			},
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
			Offline: &lmesv1alpha1.OfflineSpec{
				StorageSpec: lmesv1alpha1.OfflineStorageSpec{
					PersistentVolumeClaimName: offlinePvcName,
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
