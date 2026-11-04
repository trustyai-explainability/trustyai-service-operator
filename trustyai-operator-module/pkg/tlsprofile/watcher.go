package tlsprofile

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var watcherLog = ctrl.Log.WithName("tls-profile-watcher")

type SecurityProfileWatcher struct {
	client.Client
	profile          *configv1.TLSSecurityProfile
	adherence        string
	readProfileState func(context.Context) (profileState, error)
	onPolicyChange   func()
}

func NewSecurityProfileWatcher(
	c client.Client,
	initialProfile *configv1.TLSSecurityProfile,
	initialAdherence string,
	onPolicyChange func(),
) *SecurityProfileWatcher {
	return &SecurityProfileWatcher{
		Client:         c,
		profile:        initialProfile,
		adherence:      initialAdherence,
		onPolicyChange: onPolicyChange,
	}
}

func (w *SecurityProfileWatcher) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	if w.readProfileState == nil {
		return reconcile.Result{RequeueAfter: profileRetryInterval}, errors.New("TLS profile watcher has no reader")
	}
	state, err := w.readProfileState(ctx)
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) || isAPIUnavailable(err) {
			watcherLog.Error(err, "cannot read TLS policy, restarting to fail closed")
			if w.onPolicyChange != nil {
				w.onPolicyChange()
			}
			return reconcile.Result{}, nil
		}
		watcherLog.Info("TLS policy read failed, retrying", "error", err)
		return reconcile.Result{RequeueAfter: profileRetryInterval}, nil
	}
	if !reflect.DeepEqual(w.profile, state.profile) || w.adherence != state.adherence {
		w.profile = state.profile
		w.adherence = state.adherence
		if w.onPolicyChange != nil {
			w.onPolicyChange()
		}
	}
	return reconcile.Result{}, nil
}

func (w *SecurityProfileWatcher) NeedLeaderElection() bool {
	return false
}

func (w *SecurityProfileWatcher) SetupWithManager(mgr ctrl.Manager) error {
	apiClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("creating dynamic client for TLS profile watcher: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("creating discovery client for TLS profile watcher: %w", err)
	}
	w.readProfileState = func(ctx context.Context) (profileState, error) {
		return readProfileState(ctx, apiClient, discoveryClient)
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("tls-profile-watcher").
		WithOptions(controller.Options{NeedLeaderElection: boolPointer(false)}).
		For(&configv1.APIServer{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc:  func(e event.CreateEvent) bool { return e.Object.GetName() == apiServerName },
			UpdateFunc:  func(e event.UpdateEvent) bool { return e.ObjectNew.GetName() == apiServerName },
			DeleteFunc:  func(event.DeleteEvent) bool { return false },
			GenericFunc: func(event.GenericEvent) bool { return false },
		})).Complete(w)
}

func boolPointer(value bool) *bool {
	return &value
}
