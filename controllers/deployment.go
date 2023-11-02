package controllers

import (
	"context"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"
)

// createOAuthProxyContainer creates a OAuth-Proxy container specification
func createOAuthProxyContainer() corev1.Container {
	return corev1.Container{
		Name:  "oauth-proxy",
		Image: "registry.redhat.io/openshift4/ose-oauth-proxy@sha256:4bef31eb993feb6f1096b51b4876c65a6fb1f4401fee97fa4f4542b6b7c9bc46",
		Env: []corev1.EnvVar{
			{
				Name: "NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "metadata.namespace",
					},
				},
			},
		},
		Args: []string{
			"--https-address=:8443",
			"--provider=openshift",
			"--upstream=http://localhost:8080",
			"--tls-cert=/etc/tls/private/tls.crt",
			"--tls-key=/etc/tls/private/tls.key",
			"--client-id=trustyai-oauth-client",
			"--client-secret-file=/etc/oauth/client/secret",
			"--cookie-secret=SECRET",
			"'--openshift-delegate-urls={\"/\": {\"namespace\": \"{{.AuthNamespace}}\", \"resource\": \"services\", \"verb\": \"get\"}}'",
			"'--openshift-sar={\"namespace\": \"{{.AuthNamespace}}\", \"resource\": \"services\", \"verb\": \"get\"}'",
			"--skip-auth-regex='(^/metrics|^/apis/v1beta1/healthz)'",
		},
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: 8443,
				Name:          "https",
			},
		},
		LivenessProbe: &corev1.Probe{
			InitialDelaySeconds: 30,
			TimeoutSeconds:      1,
			PeriodSeconds:       5,
			SuccessThreshold:    1,
			FailureThreshold:    3,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/oauth/healthz",
					Port:   intstr.FromInt(8443),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},

		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/oauth/healthz",
					Port:   intstr.FromInt(8443),
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
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				MountPath: "/etc/tls/private",
				Name:      "proxy-tls",
			},
			{
				MountPath: "/etc/oauth/config",
				Name:      "oauth-config",
			},
			{
				MountPath: "/etc/oauth/client",
				Name:      "oauth-client",
			},
		},
	}
}

func (r *TrustyAIServiceReconciler) createDeploymentObject(ctx context.Context, cr *trustyaiopendatahubiov1alpha1.TrustyAIService, image string) *appsv1.Deployment {
	labels := getCommonLabels(cr.Name)
	pvcName := generatePVCName(cr)

	replicas := int32(1)
	if cr.Spec.Replicas == nil {
		cr.Spec.Replicas = &replicas
	}

	var batchSize int
	if cr.Spec.Metrics.BatchSize == nil {
		batchSize = 5000
	} else {
		batchSize = *cr.Spec.Metrics.BatchSize
	}

	// Create the OAuth-Proxy container spec
	oauthProxyContainer := InjectOAuthProxy(cr, OAuthConfig{ProxyImage: OAuthProxyImage})

	containers := []corev1.Container{
		{
			Name:  containerName,
			Image: image,
			Env: []corev1.EnvVar{
				{
					Name:  "STORAGE_DATA_FILENAME",
					Value: cr.Spec.Data.Filename,
				},
				{
					Name:  "SERVICE_STORAGE_FORMAT",
					Value: cr.Spec.Storage.Format,
				},
				{
					Name:  "STORAGE_DATA_FOLDER",
					Value: cr.Spec.Storage.Folder,
				},
				{
					Name:  "SERVICE_DATA_FORMAT",
					Value: cr.Spec.Data.Format,
				},
				{
					Name:  "SERVICE_METRICS_SCHEDULE",
					Value: cr.Spec.Metrics.Schedule,
				},
				{
					Name:  "SERVICE_BATCH_SIZE",
					Value: strconv.Itoa(batchSize),
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      volumeMountName,
					MountPath: cr.Spec.Storage.Folder,
					ReadOnly:  false,
				},
			},
		},
		oauthProxyContainer,
	}

	volume := corev1.Volume{

		Name: volumeMountName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvcName,
				ReadOnly:  false,
			},
		},
	}
	volumes := InjectOAuthVolumes(cr, OAuthConfig{ProxyImage: OAuthProxyImage})

	volumes = append(volumes, volume)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: cr.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"prometheus.io/path":   "/q/metrics",
						"prometheus.io/scheme": "http",
						"prometheus.io/scrape": "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: containers,
					Volumes:    volumes,
				},
			},
		},
	}
	return deployment
}

// reconcileDeployment returns a Deployment object with the same name/namespace as the cr
func (r *TrustyAIServiceReconciler) createDeployment(ctx context.Context, cr *trustyaiopendatahubiov1alpha1.TrustyAIService, imageName string) error {

	pvcName := generatePVCName(cr)

	pvc := &corev1.PersistentVolumeClaim{}
	pvcerr := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: cr.Namespace}, pvc)
	if pvcerr != nil {
		log.FromContext(ctx).Error(pvcerr, "PVC not found")
		return pvcerr
	}
	if pvcerr == nil {
		// The PVC is ready. We can now create the Deployment.
		deployment := r.createDeploymentObject(ctx, cr, imageName)

		if err := ctrl.SetControllerReference(cr, deployment, r.Scheme); err != nil {
			log.FromContext(ctx).Error(err, "Error setting TrustyAIService as owner of Deployment.")
			return err
		}
		log.FromContext(ctx).Info("Creating Deployment.")
		err := r.Create(ctx, deployment)
		if err != nil {
			log.FromContext(ctx).Error(err, "Error creating Deployment.")
			return err
		}
		// Created successfully
		return nil

	} else {
		return ErrPVCNotReady
	}

}

func (r *TrustyAIServiceReconciler) ensureDeployment(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {

	// Get image and tag from ConfigMap
	// If there's a ConfigMap with custom images, it is only applied when the operator is first deployed
	// Changing (or creating) the ConfigMap after the operator is deployed will not have any effect
	image, err := r.getImageFromConfigMap(ctx)
	if err != nil {
		return err
	}

	deploy := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, deploy)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment does not exist, create it
			log.FromContext(ctx).Info("Could not find Deployment. Creating it.")
			return r.createDeployment(ctx, instance, image)
		}

		// Some other error occurred when trying to get the Deployment
		return err
	}
	// Deployment is ready and using the PVC
	return nil
}
