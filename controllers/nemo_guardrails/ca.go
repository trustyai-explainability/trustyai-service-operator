package nemo_guardrails

import (
	"context"
	"github.com/go-logr/logr"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

// information about writing the new collated certificate
const (
	caBundleInitContainerName  = "ca-bundle-initializer"
	caBundleTransferVolumeName = "ca-bundle-transfer"
	caBundleTransferDirName    = "/tmp/ca-bundle-transfer/"
	caBundleFileName           = "ca-certificates.crt"
	caBundleMountPath          = "/etc/ssl/certs/"
)

// information about certificate sources to be consumed
const (
	odhTrustedCABundle          = "odh-trusted-ca-bundle"
	odhTrustedCASourceDir       = "/tmp/odh-trusted-ca-bundle/"
	odhTrustedCABundleKey       = "ca-bundle.crt"
	openshiftServingCABundleKey = "service-ca.crt"
	openshiftServingCASourceDir = "/tmp/openshift-serving-ca-bundle/"
	userCASourceDir             = "/tmp/user-ca-bundle/"
)

func GetCABundleVolumeName(configmap corev1.ConfigMap) string {
	return configmap.Name + "-vol"
}

// LoadCAConfigs grabs the default CA certificates from odh-trusted-ca, the Openshift serving ca, and user-specified configmaps. Any configmaps that are not found are skipped
func (r *NemoGuardrailsReconciler) LoadCAConfigs(ctx context.Context, logger logr.Logger, nemoGuardrails nemoguardrailsv1alpha1.NemoGuardrails) (utils.CABundleInitContainerConfig, []corev1.ConfigMap, *nemoguardrailsv1alpha1.CAStatus) {
	// === CA handling =====
	caBundleInitContainerConfig := utils.CABundleInitContainerConfig{
		CABundleInitName:           caBundleInitContainerName,
		CABundleSources:            make([]utils.CABundleSourceVolume, 0),
		CABundleTransferVolumeName: caBundleTransferVolumeName,
		CABundleTransferDir:        caBundleTransferDirName,
		CABundleTransferFileName:   caBundleFileName,
	}

	var configMapsToMount []corev1.ConfigMap
	caStatus := &nemoguardrailsv1alpha1.CAStatus{}

	odhTrustedConfigMap, err := utils.GetConfigMapByName(ctx, r.Client, odhTrustedCABundle, nemoGuardrails.Namespace)
	if err != nil {
		logger.Info("Could not find or load ODH trusted CA bundle configmap, so will not be mounted.")
		caStatus.ODHTrustedCAFound = false
		caStatus.ODHTrustedCAError = err.Error()
	} else {
		configMapsToMount = append(configMapsToMount, *odhTrustedConfigMap)
		caBundleInitContainerConfig.CABundleSources = append(caBundleInitContainerConfig.CABundleSources, utils.CABundleSourceVolume{
			CABundleSourceVolumeName: GetCABundleVolumeName(*odhTrustedConfigMap),
			CABundleSourceDir:        odhTrustedCASourceDir,
			CABundleFileNames:        []string{odhTrustedCABundleKey},
		})
		caStatus.ODHTrustedCAFound = true
	}

	openshiftServingConfigMap, err := utils.GetConfigMapByName(ctx, r.Client, nemoGuardrails.Name+"-ca-bundle", nemoGuardrails.Namespace)
	if err != nil {
		logger.Info("Could not find or load OpenShift serving CA bundle configmap, so will not be mounted.")
		caStatus.OpenshiftServingCAFound = false
		caStatus.OpenshiftServingCAError = err.Error()
	} else {
		configMapsToMount = append(configMapsToMount, *openshiftServingConfigMap)
		caBundleInitContainerConfig.CABundleSources = append(caBundleInitContainerConfig.CABundleSources, utils.CABundleSourceVolume{
			CABundleSourceVolumeName: GetCABundleVolumeName(*openshiftServingConfigMap),
			CABundleSourceDir:        openshiftServingCASourceDir,
			CABundleFileNames:        []string{openshiftServingCABundleKey},
		})
		caStatus.OpenshiftServingCAFound = true
	}

	if nemoGuardrails.Spec.CABundleConfig != nil {
		userCAConfigMap, err := utils.GetConfigMapByName(ctx, r.Client, nemoGuardrails.Spec.CABundleConfig.ConfigMapName, nemoGuardrails.Spec.CABundleConfig.ConfigMapNamespace)
		if err != nil {
			logger.Info("Could not find or load the user-specified CA bundle configmap, so will not be mounted.")
			caStatus.UserCAFound = false
			caStatus.UserCAError = err.Error()
		} else {
			configMapsToMount = append(configMapsToMount, *userCAConfigMap)
			caBundleInitContainerConfig.CABundleSources = append(caBundleInitContainerConfig.CABundleSources, utils.CABundleSourceVolume{
				CABundleSourceVolumeName: GetCABundleVolumeName(*userCAConfigMap),
				CABundleSourceDir:        userCASourceDir,
				CABundleFileNames:        nemoGuardrails.Spec.CABundleConfig.ConfigMapKeys,
			})
			caStatus.UserCAFound = true
		}
	}

	return caBundleInitContainerConfig, configMapsToMount, caStatus
}

// AddCAToDeployment modifies the NemoDeployment to contain the CA bundle init container and all necessary volumes
func (r *NemoGuardrailsReconciler) AddCAToDeployment(logger logr.Logger, deployment *appsv1.Deployment, caBundleInitContainerConfig utils.CABundleInitContainerConfig, nemoGuardrailsImage string, configMapsToMount []corev1.ConfigMap) error {
	caBundleInitContainerConfig.CABundleInitImage = nemoGuardrailsImage

	// mount CA configmap volumes
	for _, cm := range configMapsToMount {
		utils.MountConfigMapToDeployment(&cm, GetCABundleVolumeName(cm), deployment)
	}

	// create CA initcontainer
	initContainer, err := utils.CreateCABundleInitContainer(caBundleInitContainerConfig)
	if err != nil {
		logger.Error(err, "Failed to create ca bundle init container")
		return err
	}

	// Add the volume to the deployment spec
	volume := corev1.Volume{
		Name: caBundleInitContainerConfig.CABundleTransferVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	// mount transfer volume to main container
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, volume)
	for idx, _ := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[idx].VolumeMounts = append(
			deployment.Spec.Template.Spec.Containers[idx].VolumeMounts,
			corev1.VolumeMount{
				Name:      caBundleInitContainerConfig.CABundleTransferVolumeName,
				MountPath: caBundleMountPath,
				ReadOnly:  true,
			},
		)
	}
	// Set Python CA env var
	deployment.Spec.Template.Spec.Containers[0].Env = append(
		deployment.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{
			Name:  "SSL_CERT_FILE",
			Value: caBundleMountPath + caBundleFileName,
		},
	)
	deployment.Spec.Template.Spec.InitContainers = []corev1.Container{initContainer}
	return nil
}

// updateCAStatusWithRetry updates the CA status with retry on conflict
func (r *NemoGuardrailsReconciler) updateCAStatusWithRetry(ctx context.Context, namespacedName types.NamespacedName, caStatus *nemoguardrailsv1alpha1.CAStatus) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		instance := &nemoguardrailsv1alpha1.NemoGuardrails{}
		if err := r.Get(ctx, namespacedName, instance); err != nil {
			return err
		}
		instance.Status.CA = caStatus
		return r.Status().Update(ctx, instance)
	})
}
