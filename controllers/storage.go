package controllers

import (
	"context"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func generatePVCName(instance *trustyaiopendatahubiov1alpha1.TrustyAIService) string {
	return instance.Name + "-pvc"
}

func (r *TrustyAIServiceReconciler) ensurePVC(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	pvcName := generatePVCName(instance)

	pvc := &corev1.PersistentVolumeClaim{}

	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: instance.Namespace}, pvc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("PVC not found. Creating.")
			// The PVC doesn't exist, so we need to create it
			return r.createPVC(ctx, instance)
		}
		return err
	}

	return nil
}

func (r *TrustyAIServiceReconciler) createPVC(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	pvcName := generatePVCName(instance)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
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
		},
	}

	if err := ctrl.SetControllerReference(instance, pvc, r.Scheme); err != nil {
		return err
	}

	return r.Create(ctx, pvc)
}
