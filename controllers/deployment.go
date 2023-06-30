package controllers

import (
	"context"
	"fmt"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"
)

func (r *TrustyAIServiceReconciler) createDeploymentObject(ctx context.Context, cr *trustyaiopendatahubiov1alpha1.TrustyAIService, imageName string, imageTag string) *appsv1.Deployment {
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

	containers := []corev1.Container{
		{
			Name:  containerName,
			Image: fmt.Sprintf("%s:%s", imageName, imageTag),
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
	}

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
					Volumes: []corev1.Volume{
						{
							Name: volumeMountName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
									ReadOnly:  false,
								},
							},
						},
					},
				},
			},
		},
	}
	return deployment
}

// reconcileDeployment returns a Deployment object with the same name/namespace as the cr
func (r *TrustyAIServiceReconciler) createDeployment(ctx context.Context, cr *trustyaiopendatahubiov1alpha1.TrustyAIService, imageName string, imageTag string) error {

	pvcName := generatePVCName(cr)

	pvc := &corev1.PersistentVolumeClaim{}
	pvcerr := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: cr.Namespace}, pvc)
	if pvcerr != nil {
		log.FromContext(ctx).Error(pvcerr, "PVC not found")
		return pvcerr
	}
	if pvcerr == nil {
		// The PVC is ready. We can now create the Deployment.
		deployment := r.createDeploymentObject(ctx, cr, imageName, imageTag)

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
	imageName, imageTag, err := r.getImageAndTagFromConfigMap(ctx)
	if err != nil {
		return err
	}

	deploy := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, deploy)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment does not exist, create it
			log.FromContext(ctx).Info("Could not find Deployment. Creating it.")
			return r.createDeployment(ctx, instance, imageName, imageTag)
		}

		// Some other error occurred when trying to get the Deployment
		return err
	}
	// Deployment is ready and using the PVC
	return nil
}
