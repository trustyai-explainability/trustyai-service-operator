package guardrails

import (
	"context"

	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func createDeployment(orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) *appsv1.Deployment {
	var allowPrivilegeEscalation = false
	var privileged = false
	var runAsNonRootUser = true
	var command = []string{"/app/bin/fms-guardrails-orchestr8"}
	var devTerminationPath = "/dev/termination-log"

	envVars := []corev1.EnvVar{
		{
			Name:  "HTTPS_PORT",
			Value: HTTPSPort,
		},
	}
	envVars = append(envVars, orchestrator.Spec.Pod.GetContainer().GetEnv()...)

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "server-tls",
			ReadOnly:  true,
			MountPath: "/tls/server",
		},
	}
	volumeMounts = append(volumeMounts, orchestrator.Spec.Pod.GetContainer().GetVolumeMounts()...)

	volumes := []corev1.Volume{
		{
			Name: "server-tls",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "caikitstack-caikit-inf-tls",
					// DefaultMode: 256,
				},
			},
		},
	}

	volumes = append(volumes, orchestrator.Spec.Pod.GetVolumes()...)

	labels := map[string]string{
		"app":         "fmstack-nlp",
		"component":   "test-orchestrator",
		"deploy-name": "test-orchestrator",
	}

	annotations := map[string]string{}

	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:            resource.MustParse("1"),
			corev1.ResourceMemory:         resource.MustParse("2Gi"),
			corev1.ResourceRequestsCPU:    resource.MustParse("1"),
			corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
		},
	}

	readinessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/health",
				Port:   intstr.IntOrString{IntVal: 443},
				Scheme: "HTTP",
			},
		},
		InitialDelaySeconds: 5,
		TimeoutSeconds:      5,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}

	ports := []corev1.ContainerPort{{
		Name:          "http",
		ContainerPort: int32(8033),
		Protocol:      corev1.ProtocolTCP,
	}}

	deployment := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        orchestrator.Name,
			Namespace:   orchestrator.Namespace,
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &orchestrator.Spec.Replicas,
			Selector: &metav1.LabelSelector{},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            orchestrator.Name,
							Image:           "quay.io/rh-ee-mmisiura/fms-orchestr8-nlp:latest",
							ImagePullPolicy: corev1.PullAlways,
							Command:         command,
							Env:             envVars,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: &allowPrivilegeEscalation,
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{
										"ALL",
									},
								},
								Privileged:   &privileged,
								RunAsNonRoot: &runAsNonRootUser,
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
							VolumeMounts:           volumeMounts,
							Resources:              resources,
							ReadinessProbe:         readinessProbe,
							TerminationMessagePath: devTerminationPath,
							Ports:                  ports,
						},
					},
					Volumes:       volumes,
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	return &deployment
}

func (r *GuardrailsReconciler) deleteDeployment(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {
	deployment := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      orchestrator.Name,
			Namespace: orchestrator.Namespace,
		},
	}
	return r.Delete(ctx, &deployment)
}
