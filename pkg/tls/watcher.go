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
	"reflect"
	"time"

	configv1 "github.com/openshift/api/config/v1"
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
	lastProfile     *configv1.TLSSecurityProfile
	onProfileChange func()
}

func NewProfileWatcher(c client.Client, initialProfile *configv1.TLSSecurityProfile, onProfileChange func()) *ProfileWatcher {
	return &ProfileWatcher{
		Client:          c,
		lastProfile:     initialProfile,
		onProfileChange: onProfileChange,
	}
}

func (w *ProfileWatcher) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	apiServer := &configv1.APIServer{}
	if err := w.Get(ctx, client.ObjectKey{Name: "cluster"}, apiServer); err != nil {
		watcherLog.Info("TLS profile fetch did not succeed, retrying", "retryAfter", profileRetryInterval, "error", err)
		return reconcile.Result{RequeueAfter: profileRetryInterval}, nil
	}

	currentProfile := apiServer.Spec.TLSSecurityProfile
	if !reflect.DeepEqual(w.lastProfile, currentProfile) {
		watcherLog.Info("TLS security profile changed, triggering restart")
		w.lastProfile = currentProfile
		if w.onProfileChange != nil {
			w.onProfileChange()
		}
	}

	return reconcile.Result{}, nil
}

func (w *ProfileWatcher) NeedLeaderElection() bool {
	return false
}

func (w *ProfileWatcher) SetupWithManager(mgr ctrl.Manager) error {
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
