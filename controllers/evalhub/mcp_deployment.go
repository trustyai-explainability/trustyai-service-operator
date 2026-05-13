package evalhub

import (
	"context"
	"fmt"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func mcpDeploymentName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-mcp"
}

func mcpLabels(instance *evalhubv1alpha1.EvalHub) map[string]string {
	return map[string]string{
		"app":       "eval-hub",
		"instance":  instance.Name,
		"component": "mcp",
	}
}

// reconcileMCPDeployment creates or updates the Deployment for the MCP server.
// If MCP is disabled, any existing MCP deployment is deleted.
func (r *EvalHubReconciler) reconcileMCPDeployment(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	name := mcpDeploymentName(instance)

	if !instance.Spec.IsMCPEnabled() {
		return r.deleteMCPResource(ctx, instance, &appsv1.Deployment{}, name)
	}

	log.Info("Reconciling MCP Deployment", "name", name)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
		},
	}

	getErr := r.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	desiredSpec, err := r.buildMCPDeploymentSpec(ctx, instance)
	if err != nil {
		return err
	}

	if errors.IsNotFound(getErr) {
		deployment.Spec = desiredSpec
		if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
			return err
		}
		log.Info("Creating MCP Deployment", "name", name)
		return r.Create(ctx, deployment)
	}

	deployment.Spec = desiredSpec
	if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
		return err
	}
	log.Info("Updating MCP Deployment", "name", name)
	return r.Update(ctx, deployment)
}

func (r *EvalHubReconciler) buildMCPDeploymentSpec(ctx context.Context, instance *evalhubv1alpha1.EvalHub) (appsv1.DeploymentSpec, error) {
	labels := mcpLabels(instance)
	mcpSpec := instance.Spec.MCP

	image := mcpSpec.Image
	if image == "" {
		var err error
		image, err = r.getMCPImage(ctx)
		if err != nil {
			log.FromContext(ctx).Error(err, "Error getting MCP image from ConfigMap, using default "+defaultMCPImage)
			image = defaultMCPImage
		}
	}

	transport := mcpSpec.Transport
	if transport == "" {
		transport = "http-sse"
	}

	evalHubServiceURL := fmt.Sprintf("https://%s.%s.svc.cluster.local:%d", instance.Name, instance.Namespace, servicePort)

	defaultEnvVars := []corev1.EnvVar{
		{Name: "EVALHUB_TRANSPORT", Value: transport},
		{Name: "EVALHUB_HOST", Value: "0.0.0.0"},
		{Name: "EVALHUB_PORT", Value: fmt.Sprintf("%d", mcpContainerPort)},
		{Name: "EVALHUB_BASE_URL", Value: evalHubServiceURL},
		{Name: "EVALHUB_TLS_CERT_FILE", Value: mcpTLSMountPath + "/" + tlsCertFile},
		{Name: "EVALHUB_TLS_KEY_FILE", Value: mcpTLSMountPath + "/" + tlsKeyFile},
		{Name: "EVALHUB_CA_CERT_PATH", Value: mcpServiceCAMountPath + "/" + serviceCACertFile},
		{Name: "EVALHUB_INSECURE_SKIP_VERIFY", Value: "false"},
	}

	if mcpSpec.AuthSecret != "" {
		defaultEnvVars = append(defaultEnvVars, corev1.EnvVar{
			Name: "EVALHUB_AUTH_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: mcpSpec.AuthSecret},
					Key:                  "token",
				},
			},
		})
	}

	env := mergeEnvVars(defaultEnvVars, mcpSpec.Env)

	resources := defaultMCPResourceRequirements
	if mcpSpec.Resources.Requests != nil || mcpSpec.Resources.Limits != nil {
		resources = mcpSpec.Resources
	}

	tlsSecretName := mcpDeploymentName(instance) + "-tls"

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      mcpConfigVolumeName,
			MountPath: mcpConfigMountPath,
			ReadOnly:  true,
		},
		{
			Name:      tlsSecretName,
			MountPath: mcpTLSMountPath,
			ReadOnly:  true,
		},
		{
			Name:      mcpServiceCAVolumeName,
			MountPath: mcpServiceCAMountPath,
			ReadOnly:  true,
		},
	}

	container := corev1.Container{
		Name:            mcpContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullAlways,
		Ports: []corev1.ContainerPort{
			{
				Name:          "https",
				ContainerPort: mcpContainerPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env:             env,
		Resources:       resources,
		SecurityContext: defaultSecurityContext,
		VolumeMounts:    volumeMounts,
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt(mcpContainerPort),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
			InitialDelaySeconds: 15,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt(mcpContainerPort),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
			TimeoutSeconds:      3,
			FailureThreshold:    3,
		},
	}

	volumes := []corev1.Volume{
		{
			Name: mcpConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: mcpDeploymentName(instance) + "-config",
					},
				},
			},
		},
		{
			Name: tlsSecretName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: tlsSecretName,
				},
			},
		},
		{
			Name: mcpServiceCAVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: instance.Name + "-service-ca",
					},
				},
			},
		},
	}

	podTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: generateServiceAccountName(instance),
			Containers:         []corev1.Container{container},
			SecurityContext:    defaultPodSecurityContext,
			RestartPolicy:      corev1.RestartPolicyAlways,
			Volumes:            volumes,
		},
	}

	replicas := mcpSpec.GetMCPReplicas()
	return appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: labels,
		},
		Template: podTemplate,
		Strategy: appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
			RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
				MaxSurge:       &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
			},
		},
	}, nil
}

func (r *EvalHubReconciler) getMCPImage(ctx context.Context) (string, error) {
	namespace := r.Namespace
	if namespace == "" {
		namespace = "trustyai-service-operator-system"
	}
	return utils.GetImageFromConfigMapWithFallback(ctx, r.Client, configMapMCPImageKey, configMapName, namespace, defaultMCPImage)
}

// deleteMCPResource deletes a namespaced resource if it exists.
func (r *EvalHubReconciler) deleteMCPResource(ctx context.Context, instance *evalhubv1alpha1.EvalHub, obj client.Object, name string) error {
	obj.SetName(name)
	obj.SetNamespace(instance.Namespace)
	err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info("Deleting MCP resource (MCP disabled)", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", name)
	return r.Delete(ctx, obj)
}
