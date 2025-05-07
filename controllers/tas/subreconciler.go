package tas

import (
	"context"
	"time"

	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func Requeue() (reconcile.Result, error) { return ctrl.Result{Requeue: true}, nil }

func RequeueWithError(e error) (reconcile.Result, error) { return ctrl.Result{Requeue: true}, e }

func RequeueWithErrorMessage(ctx context.Context, e error, message string) (reconcile.Result, error) {
	log.FromContext(ctx).Error(e, message)
	return ctrl.Result{Requeue: true}, e
}

func RequeueWithDelay(dur time.Duration) (reconcile.Result, error) {
	return ctrl.Result{Requeue: true, RequeueAfter: dur}, nil
}

func RequeueWithDelayMessage(ctx context.Context, dur time.Duration, message string) (reconcile.Result, error) {
	log.FromContext(ctx).Info(message)
	return ctrl.Result{Requeue: true, RequeueAfter: dur}, nil
}

func RequeueWithDelayAndError(dur time.Duration, e error) (reconcile.Result, error) {
	return ctrl.Result{Requeue: true, RequeueAfter: dur}, e
}
func DoNotRequeue() (reconcile.Result, error) { return ctrl.Result{Requeue: false}, nil }

type SubReconciler = func(context.Context, ctrl.Request, *trustyaiopendatahubiov1alpha1.TrustyAIService) (*ctrl.Result, error)
