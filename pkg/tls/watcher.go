/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tls

import (
	"context"
	"fmt"
	"reflect"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	openshifttls "github.com/openshift/controller-runtime-common/pkg/tls"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

const profileRetryInterval = 5 * time.Second

type ProfileWatcher struct {
	client.Client
	lastProfile      *configv1.TLSSecurityProfile
	lastAdherence    string
	readProfileState func(context.Context) (profileState, error)
	onRestart        func()
}

func NewProfileWatcher(c client.Client, initialProfile *configv1.TLSSecurityProfile, onRestart func()) *ProfileWatcher {
	return NewProfileWatcherWithAdherence(c, initialProfile, TLSAdherenceNoOpinion, onRestart)
}

// NewProfileWatcherWithAdherence seeds the watcher with the profile and
// adherence baseline from bootstrap, including Intermediate/NoOpinion after a
// transient read failure. The production reader is installed by SetupWithManager.
func NewProfileWatcherWithAdherence(c client.Client, initialProfile *configv1.TLSSecurityProfile, initialAdherence string, onRestart func()) *ProfileWatcher {
	return &ProfileWatcher{
		Client:        c,
		lastProfile:   initialProfile,
		lastAdherence: initialAdherence,
		readProfileState: func(ctx context.Context) (profileState, error) {
			apiServer := &configv1.APIServer{}
			if err := c.Get(ctx, client.ObjectKey{Name: "cluster"}, apiServer); err != nil {
				return profileState{}, err
			}
			return profileState{profile: apiServer.Spec.TLSSecurityProfile, adherence: initialAdherence}, nil
		},
		onRestart: onRestart,
	}
}

func (w *ProfileWatcher) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	state, err := w.readProfileState(ctx)
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			watcherLog.Error(err, "permission denied reading TLS profile state; restarting to fail closed")
			if w.onRestart != nil {
				w.onRestart()
			}
			return reconcile.Result{}, nil
		}
		watcherLog.Info("TLS profile state fetch did not succeed, retrying", "retryAfter", profileRetryInterval, "error", err)
		return reconcile.Result{RequeueAfter: profileRetryInterval}, nil
	}
	// Compare effective specs: absent and explicit Intermediate are equivalent.
	// Invalid profiles must trigger bootstrap validation, not be silently ignored.
	previous, previousErr := openshifttls.GetTLSProfileSpec(w.lastProfile)
	current, currentErr := openshifttls.GetTLSProfileSpec(state.profile)
	profileChanged := previousErr != nil || currentErr != nil ||
		!reflect.DeepEqual(normalizeTLSProfileSpec(previous), normalizeTLSProfileSpec(current))
	if profileChanged || w.lastAdherence != state.adherence {
		watcherLog.Info("TLS security profile or adherence changed, triggering operand refresh", "adherence", state.adherence)
		w.lastProfile = state.profile
		w.lastAdherence = state.adherence
		if w.onRestart != nil {
			w.onRestart()
		}
	}

	return reconcile.Result{}, nil
}

func (w *ProfileWatcher) NeedLeaderElection() bool {
	return false
}

func (w *ProfileWatcher) SetupWithManager(mgr ctrl.Manager) error {
	apiClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("creating dynamic client for TLS profile state: %w", err)
	}
	w.readProfileState = func(ctx context.Context) (profileState, error) {
		return readTLSProfileState(ctx, apiClient)
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("tls-profile-watcher").
		WithOptions(controller.Options{NeedLeaderElection: boolPtr(false)}).
		For(&configv1.APIServer{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return e.Object.GetName() == "cluster"
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return e.ObjectNew.GetName() == "cluster"
			},
			DeleteFunc: func(_ event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(_ event.GenericEvent) bool {
				return false
			},
		})).
		Complete(w)
}

func boolPtr(b bool) *bool { return &b }
