package tapm

import (
	"context"
	trustyaiv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tapm/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TrustyAIPipelineManifestReconciler reconciles a TrustyAIPipelineManifest object
type TrustyAIPipelineManifestReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Namespace string
	Recorder  record.EventRecorder
}

const (
	pipelineManifestConfigMap = "trustyai-pipeline-manifest"
	ServiceName               = "TAPM"
	finalizer                 = "trustyai.opendatahub.io/tapm-finalizer"
)

// List of image keys to use in the pipeline manifest
var pipelineImageKeys = []string{
	"pipeline-ragas-image",
	"pipeline-garak-image",
}

// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=trustyaipipelinemanifests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=trustyaipipelinemanifests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=trustyaipipelinemanifests/finalizers,verbs=update

// The registered function to set up GORCH controller
func ControllerSetUp(mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error {
	return (&TrustyAIPipelineManifestReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Namespace: ns,
		Recorder:  recorder,
	}).SetupWithManager(mgr)
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TrustyAIPipelineManifest object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *TrustyAIPipelineManifestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// fetch instance of NemoGuardrails CR
	tapm := &trustyaiv1alpha1.TrustyAIPipelineManifest{}
	err := r.Get(context.TODO(), req.NamespacedName, tapm)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("TrustyAIPipelineManifest resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get TrustyAIPipelineManifest.")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if tapm.GetDeletionTimestamp() != nil {
		// Delete the ConfigMap if it exists
		cm := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: pipelineManifestConfigMap, Namespace: tapm.GetNamespace()}, cm)
		if err == nil {
			if delErr := r.Delete(ctx, cm); delErr != nil && !errors.IsNotFound(delErr) {
				logger.Error(delErr, "Failed to delete ConfigMap during finalization", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
				return ctrl.Result{}, delErr
			}
		}
		// Remove the finalizer and update
		if containsString(tapm.GetFinalizers(), finalizer) {
			tapm.SetFinalizers(removeString(tapm.GetFinalizers(), finalizer))
			if err := r.Update(ctx, tapm); err != nil {
				logger.Error(err, "Failed to remove finalizer from TrustyAIPipelineManifest")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !containsString(tapm.GetFinalizers(), finalizer) {
		tapm.SetFinalizers(append(tapm.GetFinalizers(), finalizer))
		if err := r.Update(ctx, tapm); err != nil {
			logger.Error(err, "Failed to add finalizer to TrustyAIPipelineManifest")
			return ctrl.Result{}, err
		}
	}

	// load corresponding images
	var imageMap = make(map[string]string)
	for _, key := range pipelineImageKeys {
		image, err := utils.GetImageFromConfigMap(ctx, r.Client, key, constants.ConfigMap, r.Namespace)
		if err != nil {
			logger.Error(err, "Failed to get image for", "key", key)
			return ctrl.Result{}, err
		}
		imageMap[key] = image
	}

	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: pipelineManifestConfigMap, Namespace: tapm.GetNamespace()}, existingConfigMap)
	if err != nil && errors.IsNotFound(err) {
		// Define a new configmap
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pipelineManifestConfigMap,
				Namespace: tapm.GetNamespace(),
			},
			Data: imageMap,
		}

		log.FromContext(ctx).Info("Creating a new TrustyAIPipelineManifest ConfigMap", "ConfigMap.Namespace", tapm.Namespace, "ConfigMap.Name", cm.Name)
		err = r.Create(ctx, cm)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to create new ConfigMap", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
			return ctrl.Result{}, err
		}

		err = controllerutil.SetControllerReference(tapm, cm, r.Scheme)
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to set controller reference on ConfigMap", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get ConfigMap")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// Helper functions for finalizer management
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	var result []string
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrustyAIPipelineManifestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&trustyaiv1alpha1.TrustyAIPipelineManifest{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
