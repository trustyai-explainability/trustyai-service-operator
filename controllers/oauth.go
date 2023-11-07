package controllers

import (
	"context"
	"fmt"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type OAuthConfig struct {
	ProxyImage string
}

func InjectOAuthProxy(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, oauth OAuthConfig) corev1.Container {
	// https://pkg.go.dev/k8s.io/api/core/v1#Container
	proxyContainer := corev1.Container{
		Name:            "oauth-proxy",
		Image:           oauth.ProxyImage,
		ImagePullPolicy: corev1.PullAlways,
		Env: []corev1.EnvVar{{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		}},
		Args: []string{
			"--cookie-secret=SECRET",
			"--https-address=:8443",
			fmt.Sprintf("--openshift-service-account=%s", instance.Name+"-proxy"),
			"--provider=openshift",
			"--tls-cert=/etc/tls/private/tls.crt",
			"--tls-key=/etc/tls/private/tls.key",
			"--upstream=http://localhost:8080",
			"--skip-auth-regex='(^/metrics|^/apis/v1beta1/healthz)'",
			"'--openshift-sar={\"namespace\":\"" + instance.Namespace + "\",\"resource\":\"pods\",\"verb\":\"get\"}'",
			"'--openshift-delegate-urls={\"/\": {\"namespace\": \"" + instance.Namespace + "\", \"resource\": \"pods\", \"verb\": \"get\"}}'",
		},
		Ports: []corev1.ContainerPort{{
			Name:          OAuthServicePortName,
			ContainerPort: 8443,
			Protocol:      corev1.ProtocolTCP,
		}},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/oauth/healthz",
					Port:   intstr.FromString(OAuthServicePortName),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
			InitialDelaySeconds: 30,
			TimeoutSeconds:      1,
			PeriodSeconds:       5,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/oauth/healthz",
					Port:   intstr.FromString(OAuthServicePortName),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
			InitialDelaySeconds: 5,
			TimeoutSeconds:      1,
			PeriodSeconds:       5,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				"cpu":    resource.MustParse("100m"),
				"memory": resource.MustParse("64Mi"),
			},
			Limits: corev1.ResourceList{
				"cpu":    resource.MustParse("100m"),
				"memory": resource.MustParse("64Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			//{
			//	Name:      "oauth-config",
			//	MountPath: "/etc/oauth/config",
			//},
			{
				Name:      instance.Name + "-tls",
				MountPath: "/etc/tls/private",
			},
			//{
			//	MountPath: "/etc/oauth/client",
			//	Name:      "oauth-client",
			//},
		},
	}
	return proxyContainer
}

func InjectOAuthVolumes(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, oauth OAuthConfig) []corev1.Volume {

	volumes := []corev1.Volume{
		{
			Name: instance.Name + "-tls",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: instance.Name + "-tls",
				},
			},
		},
	}
	return volumes
}

// NewTrustyAIOAuthService defines the desired OAuth service object
func NewTrustyAIOAuthService(instance *trustyaiopendatahubiov1alpha1.TrustyAIService) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-tls",
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"trustyai-service-name": instance.Name,
			},
			Annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": instance.Name + "-tls",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       OAuthServicePortName,
				Port:       OAuthServicePort,
				TargetPort: intstr.FromInt(8443),
				Protocol:   corev1.ProtocolTCP,
			}},
			Selector: map[string]string{
				"app": instance.Name,
			},
		},
	}
}

// ReconcileOAuthService will manage the OAuth service reconciliation required
// by the service's OAuth proxy
func (r *TrustyAIServiceReconciler) ReconcileOAuthService(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {

	// Generate the desired OAuth service
	desiredService := NewTrustyAIOAuthService(instance)

	// Create the OAuth service if it does not already exist
	foundService := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      desiredService.GetName(),
		Namespace: instance.GetNamespace(),
	}, foundService)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating OAuth Service")
			// Add .metatada.ownerReferences to the OAuth service to be deleted by
			// the Kubernetes garbage collector if the service is deleted
			err = ctrl.SetControllerReference(instance, desiredService, r.Scheme)
			if err != nil {
				log.FromContext(ctx).Error(err, "Unable to add OwnerReference to the OAuth Service")
				return err
			}
			// Create the OAuth service in the Openshift cluster
			err = r.Create(ctx, desiredService)
			if err != nil && !errors.IsAlreadyExists(err) {
				log.FromContext(ctx).Error(err, "Unable to create the OAuth Service")
				return err
			}
		} else {
			log.FromContext(ctx).Error(err, "Unable to fetch the OAuth Service")
			return err
		}
	}

	return nil
}
