package controllers

import (
	"context"
	"fmt"
	trustyaiopendatahubiov1alpha1 "github.com/ruivieira/trustyai-service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *TrustyAIServiceReconciler) ensurePV(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) (*corev1.PersistentVolume, error) {
	// Extract the PV name from the instance spec
	pvName := instance.Spec.Storage.PV

	// Create a PV object
	pv := &corev1.PersistentVolume{}

	// Try to get the PV
	err := r.Get(ctx, types.NamespacedName{Name: pvName}, pv)
	if err != nil {
		// If an error occurs while getting the PV, return the error
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("PersistentVolume %s not found", pvName)
		}
		return nil, err
	}

	// If the PV exists, return its reference
	return pv, nil
}

func (r *TrustyAIServiceReconciler) ensurePVC(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, pv *corev1.PersistentVolume) error {
	pvc := &corev1.PersistentVolumeClaim{}

	err := r.Get(ctx, types.NamespacedName{Name: defaultPvcName, Namespace: instance.Namespace}, pvc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("PVC not found. Creating.")
			// The PVC doesn't exist, so we need to create it
			return r.createPVC(ctx, instance, pv)
		}
		return err
	}

	return nil
}

func (r *TrustyAIServiceReconciler) createPVC(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, pv *corev1.PersistentVolume) error {

	// Extract the storage class from the PV
	storageClass := pv.Spec.StorageClassName

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultPvcName,
			Namespace: instance.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(instance.Spec.Storage.Size),
				},
			},
			StorageClassName: &storageClass,
			VolumeMode:       pv.Spec.VolumeMode,
		},
	}

	if err := ctrl.SetControllerReference(instance, pvc, r.Scheme); err != nil {
		return err
	}

	return r.Create(ctx, pvc)
}
