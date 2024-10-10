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
	isController                   = true
	allowPrivilegeEscalation       = false
	runAsNonRootUser               = true
	runAsUser                int64 = 1001030000
)

func Test_SimplePod(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:        "podimage:latest",
			DriverImage:     "driver:latest",
			ImagePullPolicy: corev1.PullAlways,
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
					Command:         lmevalRec.generateCmd(job),
					Args:            lmevalRec.generateArgs(job, log),
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

	newPod := lmevalRec.createPod(job, log)

	assert.Equal(t, expect, newPod)
}

func Test_WithLabelsAnnotationsResourcesVolumes(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:        "podimage:latest",
			DriverImage:     "driver:latest",
			ImagePullPolicy: corev1.PullAlways,
		},
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
				},
				Volumes: []corev1.Volume{
					{
						Name: "addtionalVolume",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "mypvc",
								ReadOnly:  true,
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
					Command:         lmevalRec.generateCmd(job),
					Args:            lmevalRec.generateArgs(job, log),
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
							Name:      "additionalVolume",
							MountPath: "/test",
						},
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
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
					Name: "addtionalVolume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "mypvc",
							ReadOnly:  true,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	newPod := lmevalRec.createPod(job, log)

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

	newPod = lmevalRec.createPod(job, log)
	assert.Equal(t, expect, newPod)
}

func Test_EnvSecretsPod(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:        "podimage:latest",
			DriverImage:     "driver:latest",
			ImagePullPolicy: corev1.PullAlways,
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
					},
					Command: lmevalRec.generateCmd(job),
					Args:    lmevalRec.generateArgs(job, log),
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

	newPod := lmevalRec.createPod(job, log)
	// maybe only verify the envs: Containers[0].Env
	assert.Equal(t, expect, newPod)
}

func Test_FileSecretsPod(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:        "podimage:latest",
			DriverImage:     "driver:latest",
			ImagePullPolicy: corev1.PullAlways,
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
					Command:         lmevalRec.generateCmd(job),
					Args:            lmevalRec.generateArgs(job, log),
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

	newPod := lmevalRec.createPod(job, log)
	// maybe only verify the envs: Containers[0].Env
	assert.Equal(t, expect, newPod)
}

func Test_GenerateArgBatchSize(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:         "podimage:latest",
			DriverImage:      "driver:latest",
			ImagePullPolicy:  corev1.PullAlways,
			MaxBatchSize:     24,
			DefaultBatchSize: 8,
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
			TaskList: lmesv1alpha1.TaskList{
				TaskNames: []string{"task1", "task2"},
			},
		},
	}

	// no batchSize in the job, use default batchSize
	assert.Equal(t, []string{
		"sh", "-ec",
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2 --include_path /opt/app-root/src/my_tasks --batch_size 8",
	}, lmevalRec.generateArgs(job, log))

	// exceed the max-batch-size, use max-batch-size
	var biggerBatchSize = 30
	job.Spec.BatchSize = &biggerBatchSize
	assert.Equal(t, []string{
		"sh", "-ec",
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2 --include_path /opt/app-root/src/my_tasks --batch_size 24",
	}, lmevalRec.generateArgs(job, log))

	// normal batchSize
	var normalBatchSize = 16
	job.Spec.BatchSize = &normalBatchSize
	assert.Equal(t, []string{
		"sh", "-ec",
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2 --include_path /opt/app-root/src/my_tasks --batch_size 16",
	}, lmevalRec.generateArgs(job, log))
}

func Test_GenerateArgCmdTaskRecipes(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:         "podimage:latest",
			DriverImage:      "driver:latest",
			ImagePullPolicy:  corev1.PullAlways,
			DefaultBatchSize: DefaultBatchSize,
			MaxBatchSize:     DefaultMaxBatchSize,
		},
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
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2,tr_0 --include_path /opt/app-root/src/my_tasks --batch_size 8",
	}, lmevalRec.generateArgs(job, log))

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--task-recipe", "card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--",
	}, lmevalRec.generateCmd(job))

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
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2,tr_0,tr_1 --include_path /opt/app-root/src/my_tasks --batch_size 8",
	}, lmevalRec.generateArgs(job, log))

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--task-recipe", "card=unitxt.card1,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--task-recipe", "card=unitxt.card2,template=unitxt.template2,metrics=[unitxt.metric3,unitxt.metric4],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--",
	}, lmevalRec.generateCmd(job))
}

func Test_GenerateArgCmdCustomCard(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:         "podimage:latest",
			DriverImage:      "driver:latest",
			ImagePullPolicy:  corev1.PullAlways,
			DefaultBatchSize: DefaultBatchSize,
			MaxBatchSize:     DefaultMaxBatchSize,
		},
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
							Custom: `{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "deutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
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
		"python -m lm_eval --output_path /opt/app-root/src/output --model test --model_args arg1=value1 --tasks task1,task2,tr_0 --include_path /opt/app-root/src/my_tasks --batch_size 8",
	}, lmevalRec.generateArgs(job, log))

	assert.Equal(t, []string{
		"/opt/app-root/src/bin/driver",
		"--output-path", "/opt/app-root/src/output",
		"--task-recipe", "card=cards.custom_0,template=unitxt.template,metrics=[unitxt.metric1,unitxt.metric2],format=unitxt.format,num_demos=5,demos_pool_size=10",
		"--custom-card", `{ "__type__": "task_card", "loader": { "__type__": "load_hf", "path": "wmt16", "name": "de-en" }, "preprocess_steps": [ { "__type__": "copy", "field": "translation/en", "to_field": "text" }, { "__type__": "copy", "field": "translation/de", "to_field": "translation" }, { "__type__": "set", "fields": { "source_language": "english", "target_language": "deutch" } } ], "task": "tasks.translation.directed", "templates": "templates.translation.directed.all" }`,
		"--",
	}, lmevalRec.generateCmd(job))
}

func Test_CustomCardValidation(t *testing.T) {
	log := log.FromContext(context.Background())
	lmevalRec := LMEvalJobReconciler{
		Namespace: "test",
		options: &ServiceOptions{
			PodImage:        "podimage:latest",
			DriverImage:     "driver:latest",
			ImagePullPolicy: corev1.PullAlways,
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
						"target_language": "deutch"
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
						"target_language": "deutch"
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
