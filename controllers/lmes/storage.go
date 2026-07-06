package lmes

import (
	"context"

	"github.com/go-logr/logr"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// generateManagedPVCName creates a name for a PVC to be attached to a specific LMEvalJob
func generateManagedPVCName(job *lmesv1alpha1.LMEvalJob) string {
	return job.Name + "-pvc"
}

// generateMagedPVCResource creates a PersistentVolumeClaim for an LMEvalJob
func generateMagedPVCResource(job *lmesv1alpha1.LMEvalJob) *corev1.PersistentVolumeClaim {
	pvcName := generateManagedPVCName(job)
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: job.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(job.Spec.Outputs.PersistentVolumeClaimManaged.Size),
				},
			},
		},
	}
	return &pvc
}

// createPVC creates a PVC for an LMEvalJob
func (r *LMEvalJobReconciler) createPVC(ctx context.Context, job *lmesv1alpha1.LMEvalJob, pvc *corev1.PersistentVolumeClaim) error {
	if err := ctrl.SetControllerReference(job, pvc, r.Scheme); err != nil {
		return err
	}

	return r.Create(ctx, pvc)
}

// handleManagedPVC handles a managed PVC for the LMEvalJob
func (r *LMEvalJobReconciler) handleManagedPVC(ctx context.Context, log logr.Logger, job *lmesv1alpha1.LMEvalJob) error {
	pvcName := generateManagedPVCName(job)

	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: job.Namespace}, pvc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("PVC for LMEvalJob " + job.Name + "not found. Creating.")
			managedPVC := generateMagedPVCResource(job)
			creationErr := r.createPVC(ctx, job, managedPVC)
			if creationErr != nil {
				return creationErr
			}
			log.Info("Created PVC " + pvcName + ".")
			return nil
		}
		return err
	}
	// PVC found
	return nil

}

// handleManagedPVC handles an already existing PVC for the LMEvalJob
func (r *LMEvalJobReconciler) handleExistingPVC(ctx context.Context, log logr.Logger, job *lmesv1alpha1.LMEvalJob) error {
	pvc := &corev1.PersistentVolumeClaim{}
	pvcName := *job.Spec.Outputs.PersistentVolumeClaimName
	err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: job.Namespace}, pvc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Error(err, "PVC named "+pvcName+" for LMEvalJob "+job.Name+"not found.")
			return err
		}
		return err
	}
	// PVC found
	return nil
}
