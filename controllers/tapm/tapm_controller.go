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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TrustyAIPipelineManifestReconciler reconciles a TrustyAIPipelineManifest object
type TrustyAIPipelineManifestReconciler struct {
	client.Client
	Namespace string
	Scheme    *runtime.Scheme
}

const (
	pipelineManifestConfigMap = "trustyai-pipeline-manifest"
)

// List of image keys to use in the pipeline manifest
var pipelineImageKeys = []string{
	"pipeline-ragas-image",
	"pipeline-garak-image",
}

//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=trustyaipipelinemanifest,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=trustyaipipelinemanifest/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=trustyaipipelinemanifest/finalizers,verbs=update

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
			logger.Info("NemoGuardrails resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get NemoGuardrails.")
		return ctrl.Result{}, err
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

		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to define configmap", "configmap", tapm.GetName(), "namespace", tapm.GetNamespace())
			return ctrl.Result{}, err
		}
		log.FromContext(ctx).Info("Creating a new ConfigMap", "ConfigMap.Namespace", tapm.Namespace, "ConfigMap.Name", cm.Name)
		err = r.Create(ctx, cm)
		if err != nil {
			log.FromContext(ctx).Error(err, "Failed to create new ConfigMap", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.FromContext(ctx).Error(err, "Failed to get ConfigMap")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrustyAIPipelineManifestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&trustyaiv1alpha1.TrustyAIPipelineManifest{}).
		Complete(r)
}
