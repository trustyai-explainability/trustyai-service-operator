package tlsprofile

import (
	"context"
	"fmt"
	"reflect"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const retryInterval = 5 * time.Second

type ProfileWatcher struct {
	lastState        profileState
	stateKnown       bool
	readProfileState func(context.Context) (profileState, error)
	onRestart        func()
}

func NewProfileWatcher(result Result, onRestart func()) *ProfileWatcher {
	return &ProfileWatcher{
		lastState:  profileState{profile: result.Profile, adherence: result.Adherence},
		stateKnown: result.StateKnown,
		onRestart:  onRestart,
	}
}

func (w *ProfileWatcher) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	if w.readProfileState == nil {
		return reconcile.Result{RequeueAfter: retryInterval}, nil
	}
	state, err := w.readProfileState(ctx)
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			log.Error(err, "permission denied reading TLS profile state, restarting to fail closed")
			w.restart()
			return reconcile.Result{}, nil
		}
		return reconcile.Result{RequeueAfter: retryInterval}, nil
	}
	if !w.stateKnown || !reflect.DeepEqual(w.lastState, state) {
		w.lastState = state
		w.stateKnown = true
		w.restart()
	}
	return reconcile.Result{}, nil
}

func (w *ProfileWatcher) NeedLeaderElection() bool {
	return false
}

func (w *ProfileWatcher) SetupWithManager(manager ctrl.Manager) error {
	apiClient, err := dynamic.NewForConfig(manager.GetConfig())
	if err != nil {
		return fmt.Errorf("creating dynamic client for TLS profile watcher: %w", err)
	}
	w.readProfileState = func(ctx context.Context) (profileState, error) {
		apiServer, err := apiClient.Resource(
			configv1.GroupVersion.WithResource("apiservers"),
		).Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return profileState{}, err
		}
		return decodeProfileState(apiServer.Object)
	}
	return ctrl.NewControllerManagedBy(manager).
		Named("tls-profile-watcher").
		WithOptions(controller.Options{NeedLeaderElection: boolPtr(false)}).
		For(&configv1.APIServer{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc:  func(e event.CreateEvent) bool { return e.Object.GetName() == "cluster" },
			UpdateFunc:  func(e event.UpdateEvent) bool { return e.ObjectNew.GetName() == "cluster" },
			DeleteFunc:  func(event.DeleteEvent) bool { return false },
			GenericFunc: func(event.GenericEvent) bool { return false },
		})).Complete(w)
}

func (w *ProfileWatcher) restart() {
	if w.onRestart != nil {
		w.onRestart()
	}
}

func decodeProfileState(object map[string]interface{}) (profileState, error) {
	state := profileState{adherence: AdherenceNoOpinion}
	profile, found, err := unstructured.NestedMap(object, "spec", "tlsSecurityProfile")
	if err != nil {
		return profileState{}, err
	}
	if found {
		state.profile = &configv1.TLSSecurityProfile{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(profile, state.profile); err != nil {
			return profileState{}, err
		}
	}
	state.adherence, found, err = unstructured.NestedString(object, "spec", "tlsAdherence")
	if err != nil {
		return profileState{}, err
	}
	if !found {
		state.adherence = AdherenceNoOpinion
	}
	return state, nil
}

func boolPtr(value bool) *bool {
	return &value
}
