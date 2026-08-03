package trustyaimodule

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
)

// TrustyAIModuleReconciler reconciles TrustyAI module objects.
type TrustyAIModuleReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	ManifestsTemplatePath string
}

// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=trustyais/finalizers,verbs=update

func (r *TrustyAIModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling TrustyAI module", "name", req.Name)
	return ctrl.Result{}, nil
}

func (r *TrustyAIModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.TrustyAI{}).
		Complete(r)
}
