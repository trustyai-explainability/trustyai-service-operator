package tas

import (
	"context"
	"fmt"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/images"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	pkgtls "github.com/trustyai-explainability/trustyai-service-operator/pkg/tls"
	"reflect"
	"strconv"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/tas/templates"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultBatchSize       = 5000
	deploymentTemplatePath = "service/deployment.tmpl.yaml"
	caBundleAnnotation     = "config.openshift.io/inject-trusted-cabundle"
	caBundleName           = "odh-trusted-ca-bundle"
)

type CustomCertificatesBundle struct {
	IsDefined     bool
	VolumeName    string
	ConfigMapName string
}

type DeploymentConfig struct {
	Instance                 *trustyaiopendatahubiov1.TrustyAIService
	ServiceImage             string
	KubeRBACProxyImage       string
	Schedule                 string
	VolumeMountName          string
	PVCClaimName             string
	CustomCertificatesBundle CustomCertificatesBundle
	Version                  string
	BatchSize                int
	UseDBTLSCerts            bool
	ProxyTLSArgs             []string
	MigrationEnvVarName      string
}

// createDeploymentObject returns a Deployment for the TrustyAI Service instance
func (r *TrustyAIServiceReconciler) createDeploymentObject(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, serviceImage string, caBunble CustomCertificatesBundle) (*appsv1.Deployment, error) {
	var batchSize int
	// If no batch size is provided, assume the default one
	if instance.Spec.Metrics.BatchSize == nil {
		batchSize = defaultBatchSize
	} else {
		batchSize = *instance.Spec.Metrics.BatchSize
	}

	pvcName := generatePVCName(instance)
	// Get Kube-RBAC-proxy image (env var first, then ConfigMap)
	kubeRBACProxyImage, err := images.ResolveImage(ctx, r.Client, configMapKubeRBACProxyImageKey, constants.ConfigMap, r.Namespace, "")
	if err != nil {
		return nil, fmt.Errorf("resolving kube-rbac-proxy image: %w", err)
	}

	deploymentConfig := DeploymentConfig{
		Instance:                 instance,
		ServiceImage:             serviceImage,
		KubeRBACProxyImage:       kubeRBACProxyImage,
		Schedule:                 strconv.Itoa(batchSize),
		VolumeMountName:          volumeMountName,
		PVCClaimName:             pvcName,
		CustomCertificatesBundle: caBunble,
		Version:                  constants.Version,
		BatchSize:                batchSize,
		ProxyTLSArgs:             pkgtls.CurrentProxyTLSArguments().Args,
		MigrationEnvVarName:      migrationEnvVarName,
	}
	if instance.Spec.Storage.IsStorageDatabase() {
		_, err := utils.GetSecret(ctx, r.Client, instance.Name+"-db-ca", instance.Namespace)
		if err != nil {
			deploymentConfig.UseDBTLSCerts = false
			log.FromContext(ctx).Info("Using insecure database connection. Certificates " + instance.Name + "-db-ca not found")
		} else {
			deploymentConfig.UseDBTLSCerts = true
			log.FromContext(ctx).Info("Using secure database connection with certificates " + instance.Name + "-db-ca")
		}
	} else {
		deploymentConfig.UseDBTLSCerts = false
		log.FromContext(ctx).Info("No need to check database secrets. Using PVC-mode.")
	}

	var deployment *appsv1.Deployment
	deployment, err = templateParser.ParseResource[*appsv1.Deployment](deploymentTemplatePath, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the service's deployment template")
		return nil, err
	}
	if instance.Spec.Storage.IsStorageDatabase() && instance.Spec.Storage.IsDatabaseTuningSet() {
		container := serviceContainer(deployment)
		if container == nil {
			return nil, fmt.Errorf("service container not found in deployment %q", deployment.Name)
		}
		container.Env = append(container.Env, databaseTuningConfigMapEnvVars(instance.Spec.Storage.DatabaseTuning)...)
	}

	// Stamp the reserved policy identity on the template, never the immutable selector.
	if err := ctrl.SetControllerReference(instance, deployment, r.Scheme); err != nil {
		return nil, err
	}
	identity, err := tasNetworkPolicyIdentity(instance)
	if err != nil {
		return nil, err
	}
	if err := utils.LabelOwnedNetworkPolicyDeployment(deployment, r.Scheme, instance, identity); err != nil {
		return nil, fmt.Errorf("stamping NetworkPolicy identity on TAS Deployment: %w", err)
	}
	return deployment, nil
}

// reconcileDeployment returns a Deployment object with the same name/namespace as the cr
func (r *TrustyAIServiceReconciler) createDeployment(ctx context.Context, cr *trustyaiopendatahubiov1.TrustyAIService, imageName string, caBundle CustomCertificatesBundle) error {

	if !cr.Spec.Storage.IsDatabaseConfigurationsSet() {

		pvcName := generatePVCName(cr)

		pvc := &corev1.PersistentVolumeClaim{}
		pvcerr := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: cr.Namespace}, pvc)
		if pvcerr != nil {
			log.FromContext(ctx).Error(pvcerr, "PVC not found")
			return pvcerr
		}
	}

	// We can now create the Deployment.
	deployment, err := r.createDeploymentObject(ctx, cr, imageName, caBundle)
	if err != nil {
		// Error creating the deployment resource object
		return err
	}

	log.FromContext(ctx).Info("Creating Deployment.")
	err = r.Create(ctx, deployment)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error creating Deployment.")
		return err
	}
	r.eventDeploymentCreated(cr, "Deployment created")
	// Created successfully
	return nil

}

// updateDeployment returns a Deployment object with the same name/namespace as the cr
func (r *TrustyAIServiceReconciler) updateDeployment(ctx context.Context, cr *trustyaiopendatahubiov1.TrustyAIService, imageName string, caBundle CustomCertificatesBundle) error {

	if !cr.Spec.Storage.IsDatabaseConfigurationsSet() {

		pvcName := generatePVCName(cr)

		pvc := &corev1.PersistentVolumeClaim{}
		pvcerr := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: cr.Namespace}, pvc)
		if pvcerr != nil {
			log.FromContext(ctx).Error(pvcerr, "PVC not found")
			return pvcerr
		}
	}

	// We can now create the Deployment object.
	deployment, err := r.createDeploymentObject(ctx, cr, imageName, caBundle)
	if err != nil {
		// Error creating the deployment resource object
		return err
	}

	existing := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Deployment not found during update, creating it.")
			if err := r.Create(ctx, deployment); err != nil {
				return err
			}
			r.eventDeploymentCreated(cr, "Deployment created")
			return nil
		}
		return err
	}

	if !metav1.IsControlledBy(existing, cr) {
		err := fmt.Errorf("existing Deployment %s/%s is not controlled by TrustyAIService %s, refusing to modify it", existing.Namespace, existing.Name, cr.Name)
		log.FromContext(ctx).Error(err, "Deployment ownership conflict")
		return err
	}

	if !reflect.DeepEqual(existing.Spec.Selector, deployment.Spec.Selector) {
		// spec.selector is immutable — recover by deleting and recreating the Deployment
		log.FromContext(ctx).Info("Deployment selector changed, deleting and recreating Deployment.", "Deployment", deployment.Name)
		if err := r.Delete(ctx, existing); err != nil {
			log.FromContext(ctx).Error(err, "Error deleting Deployment with stale selector.")
			return err
		}
		if err := r.Create(ctx, deployment); err != nil {
			log.FromContext(ctx).Error(err, "Error recreating Deployment after selector change.")
			return err
		}
		r.eventDeploymentCreated(cr, "Deployment recreated due to immutable selector change")
		return nil
	}

	deployment.ResourceVersion = existing.ResourceVersion
	log.FromContext(ctx).Info("Updating Deployment.")
	err = r.Update(ctx, deployment)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error updating Deployment.")
		return err
	}
	// Created successfully
	return nil

}

func (r *TrustyAIServiceReconciler) ensureDeployment(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, caBundle CustomCertificatesBundle, migration bool) error {

	// Get image from env var or ConfigMap
	// If there's a ConfigMap with custom images, it is only applied when the operator is first deployed
	// Changing (or creating) the ConfigMap after the operator is deployed will not have any effect
	image, err := images.ResolveImage(ctx, r.Client, configMapServiceImageKey, constants.ConfigMap, r.Namespace, "")
	if err != nil {
		return fmt.Errorf("resolving TrustyAI Service image: %w", err)
	}

	deploy := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, deploy)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment does not exist, create it
			log.FromContext(ctx).Info("Could not find Deployment. Creating it.")
			return r.createDeployment(ctx, instance, image, caBundle)
		}

		// Some other error occurred when trying to get the Deployment
		return err
	}
	// Deployment exists, but we are migrating
	if migration {
		log.FromContext(ctx).Info("Found migration annotation. Migrating.")
		return r.updateDeployment(ctx, instance, image, caBundle)
	}

	// Keep migration settings and the ConfigMap tuning reference in sync with the
	// CR, in both directions. ConfigMap data updates are intentionally left for a
	// manual Deployment restart.
	if instance.IsMigration() || deploymentHasMigration(deploy) || instance.Spec.Storage.IsDatabaseTuningSet() || deploymentHasDatabaseTuning(deploy) {
		desired, err := r.createDeploymentObject(ctx, instance, image, caBundle)
		if err != nil {
			return err
		}
		if deploymentMigrationChanged(deploy, desired) || databaseTuningDeploymentChanged(deploy, desired) {
			return r.updateDeployment(ctx, instance, image, caBundle)
		}
	}
	// A manager restart must refresh TLS on existing workloads, not only new ones.
	// Patch only TLS-owned fields so manual rollout annotations and storage/image
	// settings are not overwritten by a routine profile change.
	desired, err := r.createDeploymentObject(ctx, instance, image, caBundle)
	if err != nil {
		return err
	}
	if err := r.ensureTASTLSDeployment(ctx, instance, deploy, desired); err != nil {
		return err
	}
	// The caller reconciles the policy before legacy workloads enter its selector.
	return r.ensureTASDeploymentIdentity(ctx, instance, deploy)
}

// deploymentHasMigration reports whether the Deployment carries migration-only settings.
func deploymentHasMigration(deployment *appsv1.Deployment) bool {
	container := serviceContainer(deployment)
	return container != nil && (container.StartupProbe != nil || containerHasMigrationEnv(container))
}

func containerHasMigrationEnv(container *corev1.Container) bool {
	for _, env := range container.Env {
		if env.Name == migrationEnvVarName {
			return true
		}
	}
	return false
}

// migrationEnvNames are the env vars the template sets from the migration fields.
var migrationEnvNames = map[string]struct{}{
	migrationEnvVarName:                      {},
	"STORAGE_DATA_FILENAME":                  {},
	"STORAGE_DATA_FOLDER":                    {},
	"SERVICE_DATA_FORMAT":                    {},
	"STORAGE_MIGRATION_CONFIG_FROM_FOLDER":   {},
	"STORAGE_MIGRATION_CONFIG_FROM_FILENAME": {},
}

func migrationEnvVars(envVars []corev1.EnvVar) []corev1.EnvVar {
	var migrationEnv []corev1.EnvVar
	for _, env := range envVars {
		if _, ok := migrationEnvNames[env.Name]; ok {
			migrationEnv = append(migrationEnv, env)
		}
	}
	return migrationEnv
}

// storageVolumeMounts returns the container's mounts of the PVC storage volume.
func storageVolumeMounts(container *corev1.Container) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeMountName {
			mounts = append(mounts, mount)
		}
	}
	return mounts
}

// deploymentMigrationChanged compares the migration settings (env, storage mount,
// startup probe and progress deadline) of two Deployments.
// ProgressDeadlineSeconds is only compared when the desired Deployment sets it,
// because the API server defaults it on the live object.
func deploymentMigrationChanged(current, desired *appsv1.Deployment) bool {
	currentContainer := serviceContainer(current)
	desiredContainer := serviceContainer(desired)
	if currentContainer == nil || desiredContainer == nil {
		return currentContainer != desiredContainer
	}
	if !reflect.DeepEqual(migrationEnvVars(currentContainer.Env), migrationEnvVars(desiredContainer.Env)) {
		return true
	}
	if !reflect.DeepEqual(storageVolumeMounts(currentContainer), storageVolumeMounts(desiredContainer)) {
		return true
	}
	if (currentContainer.StartupProbe == nil) != (desiredContainer.StartupProbe == nil) {
		return true
	}
	if desiredContainer.StartupProbe != nil && currentContainer.StartupProbe.FailureThreshold != desiredContainer.StartupProbe.FailureThreshold {
		return true
	}
	if desired.Spec.ProgressDeadlineSeconds != nil {
		return current.Spec.ProgressDeadlineSeconds == nil || *current.Spec.ProgressDeadlineSeconds != *desired.Spec.ProgressDeadlineSeconds
	}
	return false
}

// checkDeploymentReady verifies that a TrustyAI service deployment is ready
func (r *TrustyAIServiceReconciler) checkDeploymentReady(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService) (bool, error) {
	deployment := &appsv1.Deployment{}

	err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
			if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
				podList := &corev1.PodList{}
				listOpts := []client.ListOption{
					client.InNamespace(instance.Namespace),
					client.MatchingLabels(deployment.Spec.Selector.MatchLabels),
				}
				if err := r.List(ctx, podList, listOpts...); err != nil {
					return false, err
				}

				for _, pod := range podList.Items {
					for _, cs := range pod.Status.ContainerStatuses {
						if cs.State.Waiting != nil && cs.State.Waiting.Reason == StateReasonCrashLoopBackOff {
							return false, nil
						}
						if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
							return false, nil
						}
					}
				}

				return true, nil
			}
		}
	}

	return false, nil
}
