package evalhub

import (
	"context"
	"fmt"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// reconcileDeployment creates or updates the Deployment for EvalHub
func (r *EvalHubReconciler) reconcileDeployment(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	log.Info("Reconciling Deployment", "name", instance.Name)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}

	// Check if Deployment already exists
	getErr := r.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	// Define the desired deployment spec
	desiredSpec, err := r.buildDeploymentSpec(ctx, instance)
	if err != nil {
		return err
	}

	if errors.IsNotFound(getErr) {
		// Create new Deployment
		deployment.Spec = desiredSpec
		if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
			return err
		}
		log.Info("Creating Deployment", "name", deployment.Name)
		return r.Create(ctx, deployment)
	} else {
		// Update existing Deployment
		deployment.Spec = desiredSpec
		if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
			return err
		}
		log.Info("Updating Deployment", "name", deployment.Name)
		return r.Update(ctx, deployment)
	}
}

// buildDeploymentSpec builds the deployment specification for EvalHub
func (r *EvalHubReconciler) buildDeploymentSpec(ctx context.Context, instance *evalhubv1alpha1.EvalHub) (appsv1.DeploymentSpec, error) {
	labels := map[string]string{
		"app":       "eval-hub",
		"instance":  instance.Name,
		"component": "api",
	}

	// Get image from ConfigMap with fallback
	evalHubImage, err := r.getEvalHubImage(ctx)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error getting EvalHub image from ConfigMap. Using the default image value of "+defaultEvalHubImage)
		evalHubImage = defaultEvalHubImage
	}

	// Build default environment variables
	defaultEnvVars := []corev1.EnvVar{
		{
			Name:  "API_HOST",
			Value: "0.0.0.0",
		},
		{
			Name:  "API_PORT",
			Value: "8000",
		},
		{
			Name:  "LOG_LEVEL",
			Value: "INFO",
		},
		{
			Name:  "MAX_CONCURRENT_EVALUATIONS",
			Value: "10",
		},
		{
			Name:  "DEFAULT_TIMEOUT_MINUTES",
			Value: "60",
		},
		{
			Name:  "MAX_RETRY_ATTEMPTS",
			Value: "3",
		},
		{
			Name:  "CONFIG_PATH",
			Value: "/etc/evalhub/config.yaml",
		},
		{
			Name:  "PROVIDERS_CONFIG_PATH",
			Value: "/etc/evalhub/providers.yaml",
		},
	}

	// Merge environment variables with CR values taking precedence
	env := mergeEnvVars(defaultEnvVars, instance.Spec.Env)

	// Container definition based on k8s examples
	container := corev1.Container{
		Name:            containerName,
		Image:           evalHubImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: containerPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env:             env,
		Resources:       defaultResourceRequirements,
		SecurityContext: defaultSecurityContext,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "evalhub-config",
				MountPath: "/etc/evalhub",
				ReadOnly:  true,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/api/v1/health",
					Port: intstr.FromString("http"),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/api/v1/health",
					Port: intstr.FromString("http"),
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       5,
			TimeoutSeconds:      3,
			FailureThreshold:    3,
		},
	}

	// Get kube-rbac-proxy image from ConfigMap - REQUIRED, no fallback
	kubeRBACProxyImage, err := r.getImageFromConfigMap(ctx, configMapKubeRBACProxyImageKey)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get kube-rbac-proxy image from operator ConfigMap")
		return appsv1.DeploymentSpec{}, fmt.Errorf("kube-rbac-proxy configuration error: %w", err)
	}

	// Validate the image configuration
	err = r.validateImageConfiguration(ctx, kubeRBACProxyImage, "kube-rbac-proxy")
	if err != nil {
		return appsv1.DeploymentSpec{}, fmt.Errorf("invalid kube-rbac-proxy image configuration: %w", err)
	}

	log.FromContext(ctx).Info("Configuring kube-rbac-proxy sidecar",
		"evalHub", instance.Name,
		"namespace", instance.Namespace,
		"proxyImage", kubeRBACProxyImage)

	// kube-rbac-proxy sidecar container
	proxyContainer := corev1.Container{
		Name:  "kube-rbac-proxy",
		Image: kubeRBACProxyImage,
		Args: []string{
			"--secure-listen-address=0.0.0.0:8443",
			"--upstream=http://127.0.0.1:8000",
			"--tls-cert-file=/etc/tls/private/tls.crt",
			"--tls-private-key-file=/etc/tls/private/tls.key",
			"--config-file=/etc/kube-rbac-proxy/config.yaml",
			"--logtostderr=true",
			"--v=0",
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "https",
				ContainerPort: kubeRBACProxyPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: &[]bool{false}[0],
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			RunAsNonRoot: &[]bool{true}[0],
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "kube-rbac-proxy-config",
				MountPath: "/etc/kube-rbac-proxy",
				ReadOnly:  true,
			},
			{
				Name:      instance.Name + "-tls",
				MountPath: "/etc/tls/private",
				ReadOnly:  true,
			},
		},
	}

	// Pod template with both containers and required volumes
	podTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: generateServiceAccountName(instance),
			Containers:         []corev1.Container{container, proxyContainer},
			SecurityContext:    defaultPodSecurityContext,
			RestartPolicy:      corev1.RestartPolicyAlways,
			Volumes: []corev1.Volume{
				{
					Name: "evalhub-config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: instance.Name + "-config",
							},
						},
					},
				},
				{
					Name: "kube-rbac-proxy-config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: instance.Name + "-proxy-config",
							},
						},
					},
				},
				{
					Name: instance.Name + "-tls",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: instance.Name + "-tls",
						},
					},
				},
			},
		},
	}

	// Deployment spec
	replicas := instance.Spec.GetReplicas()
	return appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: labels,
		},
		Template: podTemplate,
		Strategy: appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
			RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxUnavailable: &intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "25%",
				},
				MaxSurge: &intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "25%",
				},
			},
		},
	}, nil
}

// getEvalHubImage retrieves the EvalHub image from ConfigMap with fallback to default
func (r *EvalHubReconciler) getEvalHubImage(ctx context.Context) (string, error) {
	// Get the namespace where the operator is deployed (where the ConfigMap should be)
	namespace := r.Namespace
	if namespace == "" {
		// Fallback to default namespace if not set
		namespace = "trustyai-service-operator-system"
	}

	return utils.GetImageFromConfigMapWithFallback(ctx, r.Client, configMapEvalHubImageKey, configMapName, namespace, defaultEvalHubImage)
}

// mergeEnvVars merges default environment variables with CR-specified ones,
// with CR values taking precedence over defaults when names conflict
func mergeEnvVars(defaults, overrides []corev1.EnvVar) []corev1.EnvVar {
	// Build map of environment variables starting with defaults
	envMap := make(map[string]corev1.EnvVar)
	for _, env := range defaults {
		envMap[env.Name] = env
	}

	// Overlay CR-specified values (they win over defaults)
	for _, env := range overrides {
		envMap[env.Name] = env
	}

	// Reconstruct slice preserving order: CR values first, then remaining defaults
	var result []corev1.EnvVar

	// Add CR values in their original order
	crNames := make(map[string]bool)
	for _, env := range overrides {
		result = append(result, envMap[env.Name])
		crNames[env.Name] = true
	}

	// Add remaining defaults that weren't overridden
	for _, env := range defaults {
		if !crNames[env.Name] {
			result = append(result, envMap[env.Name])
		}
	}

	return result
}
