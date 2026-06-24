package evalhub

import (
	"context"
	"fmt"
	"strconv"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/images"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func mcpDeploymentName(instance *evalhubv1.EvalHub) string {
	return instance.Name + "-mcp"
}

func mcpLabels(instance *evalhubv1.EvalHub) map[string]string {
	return map[string]string{
		"app":       "eval-hub",
		"instance":  instance.Name,
		"component": "mcp",
	}
}

// reconcileMCPDeployment creates or updates the Deployment for the MCP server.
// If MCP is disabled, any existing MCP deployment is deleted.
func (r *EvalHubReconciler) reconcileMCPDeployment(ctx context.Context, instance *evalhubv1.EvalHub) error {
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

func (r *EvalHubReconciler) buildMCPDeploymentSpec(ctx context.Context, instance *evalhubv1.EvalHub) (appsv1.DeploymentSpec, error) {
	labels := mcpLabels(instance)
	mcpSpec := instance.Spec.MCP

	image := mcpSpec.Image
	if image == "" {
		var err error
		image, err = images.ResolveImage(ctx, r.Client, images.EvalHubImageKey, configMapName, r.Namespace, defaultEvalHubImage)
		if err != nil {
			return appsv1.DeploymentSpec{}, fmt.Errorf("resolving EvalHub image for MCP: %w", err)
		}
	}

	kubeRBACProxyImage, err := images.ResolveImage(ctx, r.Client, images.KubeRBACProxyKey, configMapName, r.Namespace, "")
	if err != nil {
		return appsv1.DeploymentSpec{}, fmt.Errorf("resolving kube-rbac-proxy image for MCP: %w", err)
	}

	settings := mergeEvalHubDeploymentOperatorSettings(ctx, r.readOperatorConfigMapData(ctx))

	clientTransport := mcpClientTransport(mcpSpec)
	transportEnv := mcpTransportEnv(mcpSpec)
	configPath := mcpConfigFilePath()

	evalHubServiceURL := fmt.Sprintf("https://%s.%s.svc.cluster.local:%d", instance.Name, instance.Namespace, servicePort)

	defaultEnvVars := []corev1.EnvVar{
		{Name: "EVALHUB_TRANSPORT", Value: transportEnv},
		{Name: "EVALHUB_HOST", Value: "127.0.0.1"},
		{Name: "EVALHUB_PORT", Value: fmt.Sprintf("%d", mcpAppPort)},
		{Name: "EVALHUB_BASE_URL", Value: evalHubServiceURL},
		{Name: "EVALHUB_TENANT", Value: instance.Namespace},
		{Name: "EVALHUB_CA_CERT_PATH", Value: mcpServiceCAMountPath + "/" + serviceCACertFile},
		{Name: "EVALHUB_INSECURE", Value: "false"},
	}

	if mcpSpec.AuthSecret != "" {
		defaultEnvVars = append(defaultEnvVars, corev1.EnvVar{
			Name: "EVALHUB_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: mcpSpec.AuthSecret},
					Key:                  "token",
				},
			},
		})
	}

	env := mergeEnvVars(defaultEnvVars, mcpSpec.Env,
		"EVALHUB_HOST", "EVALHUB_PORT", "EVALHUB_TLS_CERT_FILE", "EVALHUB_TLS_KEY_FILE")

	resources := defaultMCPResourceRequirements
	if mcpSpec.Resources.Requests != nil || mcpSpec.Resources.Limits != nil {
		resources = mcpSpec.Resources
	}

	tlsSecretName := mcpDeploymentName(instance) + "-tls"
	mcpConfigCMName := mcpDeploymentName(instance) + "-config"

	mcpVolumeMounts := []corev1.VolumeMount{
		{
			Name:      mcpConfigVolumeName,
			MountPath: mcpConfigMountPath,
			ReadOnly:  true,
		},
		{
			Name:      mcpServiceCAVolumeName,
			MountPath: mcpServiceCAMountPath,
			ReadOnly:  true,
		},
	}

	mcpContainer := corev1.Container{
		Name:            mcpContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullAlways,
		Command:         []string{mcpBinaryPath},
		Args: []string{
			"--config", configPath,
			"--transport", clientTransport,
			"--host", "127.0.0.1",
			"--port", strconv.Itoa(mcpAppPort),
			"--auth-type", "rbac-proxy", // when running here we are always behind the kube-rbac-proxy
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "mcp",
				ContainerPort: mcpAppPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env:             env,
		Resources:       resources,
		SecurityContext: defaultSecurityContext,
		VolumeMounts:    mcpVolumeMounts,
	}

	upstreamURL := fmt.Sprintf("http://127.0.0.1:%d/", mcpAppPort)

	kubeRBACProxyContainer := corev1.Container{
		Name:            kubeRBACProxyContainerName,
		Image:           kubeRBACProxyImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args: []string{
			"--secure-listen-address=0.0.0.0:" + fmt.Sprintf("%d", mcpServicePort),
			"--upstream=" + upstreamURL,
			"--config-file=" + kubeRBACProxyConfigMountPath,
			"--tls-cert-file=" + tlsSecretMountPath + "/" + tlsCertFile,
			"--tls-private-key-file=" + tlsSecretMountPath + "/" + tlsKeyFile,
			"--proxy-endpoints-port=" + fmt.Sprintf("%d", kubeRBACProxyHealthPort),
			"--ignore-paths=" + mcpHealthPath,
			"--auth-header-fields-enabled",
			"--auth-header-user-field-name=X-User",
			"--v=0",
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "https",
				ContainerPort: mcpServicePort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "proxy-healthz",
				ContainerPort: kubeRBACProxyHealthPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Resources:       settings.KubeRBACProxyResources,
		SecurityContext: defaultSecurityContext,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      mcpConfigVolumeName,
				MountPath: kubeRBACProxyConfigMountPath,
				SubPath:   evalHubAuthConfigMapKey,
				ReadOnly:  true,
			},
			{
				Name:      tlsSecretName,
				MountPath: tlsSecretMountPath,
				ReadOnly:  true,
			},
		},
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   mcpHealthPath,
					Port:   intstr.FromInt(mcpServicePort),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
			PeriodSeconds:    10,
			TimeoutSeconds:   5,
			FailureThreshold: 30,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   mcpHealthPath,
					Port:   intstr.FromInt(mcpServicePort),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
			PeriodSeconds:    10,
			TimeoutSeconds:   5,
			FailureThreshold: 3,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   kubeRBACProxyHealthPath,
					Port:   intstr.FromInt(kubeRBACProxyHealthPort),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
	}

	volumes := []corev1.Volume{
		{
			Name: mcpConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: mcpConfigCMName,
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
			Containers:         []corev1.Container{mcpContainer, kubeRBACProxyContainer},
			SecurityContext:    defaultPodSecurityContext,
			RestartPolicy:      corev1.RestartPolicyAlways,
			Volumes:            volumes,
		},
	}

	replicas := instance.Spec.GetMCPReplicas()
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

// deleteMCPResource deletes a namespaced resource if it exists.
func (r *EvalHubReconciler) deleteMCPResource(ctx context.Context, instance *evalhubv1.EvalHub, obj client.Object, name string) error {
	obj.SetName(name)
	obj.SetNamespace(instance.Namespace)
	err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info("Deleting MCP resource (MCP disabled)",
		"resource", fmt.Sprintf("%T", obj),
		"name", name,
		"namespace", instance.Namespace,
	)
	return r.Delete(ctx, obj)
}
