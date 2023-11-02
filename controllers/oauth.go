package controllers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
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
		// TODO: Change SA
		Args: []string{
			"--provider=openshift",
			"--https-address=:8443",
			"--http-address=",
			"--openshift-service-account=default",
			"--cookie-secret-file=/etc/oauth/config/cookie_secret",
			"--cookie-expire=24h0m0s",
			"--tls-cert=/etc/tls/private/tls.crt",
			"--tls-key=/etc/tls/private/tls.key",
			"--upstream=http://localhost:8080",
			"--upstream-ca=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
			"--skip-provider-button",
			"'--openshift-delegate-urls={\"/\": {\"namespace\": \"" + instance.GetNamespace() + "\", \"resource\": \"services\", \"verb\": \"get\"}}'",
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
			{
				Name:      "oauth-config",
				MountPath: "/etc/oauth/config",
			},
			{
				Name:      "tls-certificates",
				MountPath: "/etc/tls/private",
			},
		},
	}
	return proxyContainer
}

func InjectOAuthVolumes(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, oauth OAuthConfig) []corev1.Volume {

	volumes := []corev1.Volume{
		// Add the OAuth configuration volume:
		// https://pkg.go.dev/k8s.io/api/core/v1#Volume
		{
			Name: "oauth-config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  instance.Name + "-oauth-config",
					DefaultMode: pointer.Int32Ptr(420),
				},
			},
		},
		// Add the TLS certificates volume:
		// https://pkg.go.dev/k8s.io/api/core/v1#Volume
		{
			Name: "tls-certificates",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  instance.Name + "-tls",
					DefaultMode: pointer.Int32Ptr(420),
				},
			},
		},
	}
	return volumes
}

// NewTrustyAIServiceOAuthSecret defines the desired OAuth secret object
func NewTrustyAIServiceOAuthSecret(instance *trustyaiopendatahubiov1alpha1.TrustyAIService) *corev1.Secret {
	// Generate the cookie secret for the OAuth proxy
	cookieSeed := make([]byte, 16)
	_, _ = rand.Read(cookieSeed)
	cookieSecret := base64.StdEncoding.EncodeToString(
		[]byte(base64.StdEncoding.EncodeToString(cookieSeed)))

	// Create a Kubernetes secret to store the cookie secret
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-oauth-config",
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"trustyai-service": instance.Name,
			},
		},
		StringData: map[string]string{
			"cookie_secret": cookieSecret,
		},
	}
}

// ReconcileOAuthSecret will manage the OAuth secret reconciliation required by
// the service's OAuth proxy
func (r *TrustyAIServiceReconciler) reconcileOAuthSecret(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {

	// Generate the desired OAuth secret
	desiredSecret := NewTrustyAIServiceOAuthSecret(instance)

	// Create the OAuth secret if it does not already exist
	foundSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      desiredSecret.Name,
		Namespace: instance.Namespace,
	}, foundSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating OAuth Secret")
			// Add .metatada.ownerReferences to the OAuth secret to be deleted by
			// the Kubernetes garbage collector if the service is deleted
			err = ctrl.SetControllerReference(instance, desiredSecret, r.Scheme)
			if err != nil {
				log.FromContext(ctx).Error(err, "Unable to add OwnerReference to the OAuth Secret")
				return err
			}
			// Create the OAuth secret in the Openshift cluster
			err = r.Create(ctx, desiredSecret)
			if err != nil && !errors.IsAlreadyExists(err) {
				log.FromContext(ctx).Error(err, "Unable to create the OAuth Secret")
				return err
			}
		} else {
			log.FromContext(ctx).Error(err, "Unable to fetch the OAuth Secret")
			return err
		}
	}

	return nil
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
