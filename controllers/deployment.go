package controllers

import (
	"context"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/templates"
	"reflect"
	"strconv"

	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
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
	Instance                 *trustyaiopendatahubiov1alpha1.TrustyAIService
	ServiceImage             string
	OAuthImage               string
	Schedule                 string
	VolumeMountName          string
	PVCClaimName             string
	CustomCertificatesBundle CustomCertificatesBundle
	Version                  string
	BatchSize                int
}

// createDeploymentObject returns a Deployment for the TrustyAI Service instance
func (r *TrustyAIServiceReconciler) createDeploymentObject(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, serviceImage string, caBunble CustomCertificatesBundle) (*appsv1.Deployment, error) {

	var batchSize int
	// If no batch size is provided, assume the default one
	if instance.Spec.Metrics.BatchSize == nil {
		batchSize = defaultBatchSize
	} else {
		batchSize = *instance.Spec.Metrics.BatchSize
	}

	pvcName := generatePVCName(instance)
	// Get OAuth-proxy image from ConfigMap
	oauthProxyImage, err := r.getImageFromConfigMap(ctx, configMapOAuthProxyImageKey, defaultOAuthProxyImage)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error getting OAuth image from ConfigMap. Using the default image value of "+defaultOAuthProxyImage)
	}

	deploymentConfig := DeploymentConfig{
		Instance:                 instance,
		ServiceImage:             serviceImage,
		OAuthImage:               oauthProxyImage,
		Schedule:                 strconv.Itoa(batchSize),
		VolumeMountName:          volumeMountName,
		PVCClaimName:             pvcName,
		CustomCertificatesBundle: caBunble,
		Version:                  Version,
		BatchSize:                batchSize,
	}

	var deployment *appsv1.Deployment
	deployment, err = templateParser.ParseResource[appsv1.Deployment](deploymentTemplatePath, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the service's deployment template")
		return nil, err
	}

	return deployment, nil
}

// reconcileDeployment returns a Deployment object with the same name/namespace as the cr
func (r *TrustyAIServiceReconciler) createDeployment(ctx context.Context, cr *trustyaiopendatahubiov1alpha1.TrustyAIService, imageName string, caBundle CustomCertificatesBundle) error {

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

	if err := ctrl.SetControllerReference(cr, deployment, r.Scheme); err != nil {
		log.FromContext(ctx).Error(err, "Error setting TrustyAIService as owner of Deployment.")
		return err
	}
	log.FromContext(ctx).Info("Creating Deployment.")
	err = r.Create(ctx, deployment)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error creating Deployment.")
		return err
	}
	// Created successfully
	return nil

}

// updateDeployment returns a Deployment object with the same name/namespace as the cr
func (r *TrustyAIServiceReconciler) updateDeployment(ctx context.Context, cr *trustyaiopendatahubiov1alpha1.TrustyAIService, imageName string, caBundle CustomCertificatesBundle) error {

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

	if err := ctrl.SetControllerReference(cr, deployment, r.Scheme); err != nil {
		log.FromContext(ctx).Error(err, "Error setting TrustyAIService as owner of Deployment.")
		return err
	}
	log.FromContext(ctx).Info("Updating Deployment.")
	err = r.Update(ctx, deployment)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error updating Deployment.")
		return err
	}
	// Created successfully
	return nil

}

func (r *TrustyAIServiceReconciler) ensureDeployment(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, caBundle CustomCertificatesBundle, migration bool) error {

	// Get image and tag from ConfigMap
	// If there's a ConfigMap with custom images, it is only applied when the operator is first deployed
	// Changing (or creating) the ConfigMap after the operator is deployed will not have any effect
	image, err := r.getImageFromConfigMap(ctx, configMapServiceImageKey, defaultImage)
	if err != nil {
		return err
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

	// Deployment is ready and using the PVC
	return nil
}

func (r *TrustyAIServiceReconciler) checkDeploymentReady(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) (bool, error) {
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
				return true, nil
			}
		}
	}

	return false, nil
}
