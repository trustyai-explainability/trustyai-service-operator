package guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	allowPrivilegeEscalation = false
	privileged               = false
	runAsNonRootUser         = true
	command                  = []string{"/app/bin/fms-guardrails-orchestr8"}
	devTerminationPath       = "/dev/termination-log"
	resources                = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:            resource.MustParse("1"),
			corev1.ResourceMemory:         resource.MustParse("2Gi"),
			corev1.ResourceRequestsCPU:    resource.MustParse("1"),
			corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
		},
	}
	readinessProbe = &corev1.Probe{
		HTTPGet: &corev1.HTTPGetAction{
			Path:   "/health",
			Port:   8033,
			Scheme: "HTTP",
		},
		InitialDelaySeconds: 5,
		TimeoutSeconds:      5,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}
)

func TestSimpleDeployment(t *testing.T) {
	log := log.FromContext(context.Background())

	var orchestrator = &guardrailsv1alpha1.GuardrailsOrchestrator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-orchestrator",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "GuardrailsOrchestrator",
			APIVersion: "guardrails.trusty.ai/v1alpha1",
		},
		Spec: guardrailsv1alpha1.GuardrailsOrchestratorSpec{
			Replicas: 1,
		},
	}

	expect := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        guardrailsv1alpha1.Name,
			Namespace:   "default",
			Annotations: map[string]string{},
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32(1),
			Selector: &metav1.LabelSelector{},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":         "fmstack-nlp",
						"component":   orchestrator.Name,
						"deploy-name": orchestrator.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            orchestrator.Name,
							Image:           orchestrator.Spec.DefaultContainerImage,
							ImagePullPolicy: corev1.PullAlways,
							Command:         command,
							Env: []corev1.EnvVar{
								{
									Name:  "HTTPS_PORT",
									Value: orchestrator.HTTPSPort,
								},
							},
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
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "server-tls",
									ReadOnly:  true,
									MountPath: "/tls/server",
								},
							},
							Resources:              resources,
							ReadinessProbe:         readinessProbe,
							TerminationMessagePath: devTerminationPath,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8033,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "server-tls",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  "caikitstack-caikit-inf-tls",
									DefaultMode: 256,
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	newDeployment := createDeployment(orchestrator, log)
	assert.Equal(t, expect, newDeployment)
}

func TestEnvVarsDeployment(t *testing.T) {
	log := log.FromContext(context.Background())

	var orchestrator = &guardrailsv1alpha1.GuardrailsOrchestrator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-orchestrator",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "GuardrailsOrchestrator",
			APIVersion: "guardrails.trusty.ai/v1alpha1",
		},
		Spec: guardrailsv1alpha1.GuardrailsOrchestratorSpec{
			Replicas: 1,
		},
	}

}
