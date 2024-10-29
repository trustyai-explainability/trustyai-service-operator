package guardrails

import (
	"context"

	"github.com/go-logr/logr"
	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func createDeployment(orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator, log logr.Logger) *appsv1.Deployment {
	var allowPrivilegeEscalation = false
	var privileged = false
	var runAsNonRootUser = true
	var command = []string{"/app/bin/fms-guardrails-orchestr8"}
	var devTerminationPath = "/dev/termination-log"

	var envVars = []corev1.EnvVar{
		{
			Name:  "HTTPS_PORT",
			Value: HTTPSPort,
		},
	}
	envVars = append(envVars, orchestrator.Spec.Pod.GetContainer().Env...)

	var volumeMounts = []corev1.VolumeMount{
		{
			Name:      "server-tls",
			ReadOnly:  true,
			MountPath: "/tls/server",
		},
	}
	volumeMounts = append(volumeMounts, orchestrator.Spec.Pod.GetContainer().VolumeMounts...)

	var volumes = []corev1.Volume{}
	volumes = append(volumes, orchestrator.Spec.Pod.Volumes...)

	var labels = map[string]string{
		"app":         "fmstack-nlp",
		"component":   orchestrator.Name,
		"deploy-name": orchestrator.Name,
	}

	var annotations = map[string]string{}

	var resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:            resource.MustParse("1"),
			corev1.ResourceMemory:         resource.MustParse("2Gi"),
			corev1.ResourceRequestsCPU:    resource.MustParse("1"),
			corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
		},
	}

	var readinessProbe = &corev1.Probe{
		HTTPGet: &corev1.HTTPGetAction{
			Path:   "/health",
			Port:   HTTPSPort,
			Scheme: "HTTP",
		},
		InitialDelaySeconds: 5,
		TimeoutSeconds:      5,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}

	var ports = []corev1.ContainerPort{{
		Name:          "http",
		ContainerPort: int32(443),
		Protocol:      corev1.ProtocolTCP,
	}}

	deployment := appsv1.Deployment{
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
							Image:           orchestrator.Spec.Pod.GetContainer().Image,
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
