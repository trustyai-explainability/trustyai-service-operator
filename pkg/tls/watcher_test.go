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
	"errors"
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := configv1.Install(scheme); err != nil {
		t.Fatalf("installing configv1 scheme: %v", err)
	}
	return scheme
}

func newAPIServerWithProfile(profile *configv1.TLSSecurityProfile) *configv1.APIServer {
	return &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSSecurityProfile: profile,
		},
	}
}

func mustReconcile(t *testing.T, w *ProfileWatcher) reconcile.Result {
	t.Helper()
	result, err := w.Reconcile(context.Background(), reconcile.Request{})
	if err != nil {
		t.Fatalf("Reconcile() returned unexpected error: %v", err)
	}
	return result
}

func TestReconcile_ProfileUnchanged(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
	scheme := newTestScheme(t)
	apiServer := newAPIServerWithProfile(profile)
	apiServer.Name = "cluster"

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

	called := false
	w := NewProfileWatcher(c, profile, func() { called = true })

	result := mustReconcile(t, w)
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue, got %v", result.RequeueAfter)
	}
	if called {
		t.Error("onProfileChange must not be called when profile is unchanged")
	}
}

func TestReconcile_DeniedReadRestartsToFailClosed(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "forbidden", err: apierrors.NewForbidden(schema.GroupResource{Resource: "apiservers"}, "cluster", errors.New("denied"))},
		{name: "unauthorized", err: apierrors.NewUnauthorized("denied")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			w := NewProfileWatcher(nil, nil, func() { called = true })
			w.readProfileState = func(context.Context) (profileState, error) {
				return profileState{}, tt.err
			}

			result := mustReconcile(t, w)
			if result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want no retry after denied read", result.RequeueAfter)
			}
			if !called {
				t.Error("restart callback was not called after denied read")
			}
		})
	}
}

func TestReconcile_ProfileChanged(t *testing.T) {
	initial := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
	updated := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	scheme := newTestScheme(t)

	apiServer := newAPIServerWithProfile(updated)
	apiServer.Name = "cluster"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

	called := false
	w := NewProfileWatcher(c, initial, func() { called = true })

	result := mustReconcile(t, w)
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue, got %v", result.RequeueAfter)
	}
	if !called {
		t.Error("onProfileChange must be called when profile changes")
	}
	if w.lastProfile == nil || w.lastProfile.Type != configv1.TLSProfileModernType {
		t.Errorf("lastProfile not updated correctly, got %v", w.lastProfile)
	}
}

func TestReconcile_NilCallback(t *testing.T) {
	updated := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	scheme := newTestScheme(t)

	apiServer := newAPIServerWithProfile(updated)
	apiServer.Name = "cluster"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

	w := NewProfileWatcher(c, nil, nil)
	mustReconcile(t, w)
}

func TestReconcile_APIServerNotFound(t *testing.T) {
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	called := false
	w := NewProfileWatcher(c, nil, func() { called = true })

	result := mustReconcile(t, w)
	if result.RequeueAfter != profileRetryInterval {
		t.Errorf("expected requeue after %v, got %v", profileRetryInterval, result.RequeueAfter)
	}
	if called {
		t.Error("onProfileChange must not be called when API server object is not found")
	}
}

func TestReconcile_InitialNilToProfile(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	scheme := newTestScheme(t)

	apiServer := newAPIServerWithProfile(profile)
	apiServer.Name = "cluster"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

	called := false
	w := NewProfileWatcher(c, nil, func() { called = true })

	mustReconcile(t, w)
	if !called {
		t.Error("onProfileChange must be called when initial profile is nil and API returns a profile")
	}
}

func TestReconcile_ProfileNilledOut(t *testing.T) {
	initial := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	scheme := newTestScheme(t)

	apiServer := newAPIServerWithProfile(nil)
	apiServer.Name = "cluster"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

	called := false
	w := NewProfileWatcher(c, initial, func() { called = true })

	mustReconcile(t, w)
	if !called {
		t.Error("onProfileChange must be called when profile is removed from APIServer")
	}
}

func TestReconcile_CustomProfileChange(t *testing.T) {
	initial := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
				MinTLSVersion: configv1.VersionTLS12,
			},
		},
	}
	updated := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				Ciphers:       []string{"ECDHE-RSA-AES256-GCM-SHA384"},
				MinTLSVersion: configv1.VersionTLS13,
			},
		},
	}
	scheme := newTestScheme(t)

	apiServer := newAPIServerWithProfile(updated)
	apiServer.Name = "cluster"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

	called := false
	w := NewProfileWatcher(c, initial, func() { called = true })

	mustReconcile(t, w)
	if !called {
		t.Error("onProfileChange must be called when custom cipher list changes")
	}
}

func TestReconcile_AdherenceChange(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
	scheme := newTestScheme(t)

	apiServer := newAPIServerWithProfile(profile)
	apiServer.Name = "cluster"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

	called := false
	w := NewProfileWatcher(c, profile, func() { called = true })
	w.readProfileState = func(context.Context) (profileState, error) {
		return profileState{profile: profile, adherence: TLSAdherenceStrictAllComponents}, nil
	}

	mustReconcile(t, w)
	if !called {
		t.Error("onProfileChange must be called when TLS adherence changes")
	}
	if w.lastAdherence != TLSAdherenceStrictAllComponents {
		t.Errorf("watcher did not retain the new adherence: %q", w.lastAdherence)
	}
}

func TestReconcile_TransientFailurePreservesPolicyUntilRecovery(t *testing.T) {
	initial := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	previous := CurrentProxyTLSArguments()
	args, err := ResolveProxyTLSArguments(initial, TLSAdherenceStrictAllComponents, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	SetProxyTLSArguments(args)
	defer SetProxyTLSArguments(previous)

	restarts := 0
	w := NewProfileWatcherWithAdherence(nil, initial, TLSAdherenceStrictAllComponents, func() { restarts++ })
	w.readProfileState = func(context.Context) (profileState, error) {
		return profileState{}, apierrors.NewServiceUnavailable("temporary outage")
	}
	result := mustReconcile(t, w)
	if result.RequeueAfter != profileRetryInterval || restarts != 0 ||
		!reflect.DeepEqual(w.lastProfile, initial) || w.lastAdherence != TLSAdherenceStrictAllComponents ||
		!reflect.DeepEqual(CurrentProxyTLSArguments(), args) {
		t.Fatal("transient runtime failure changed the last-known-good policy or triggered restart")
	}

	// Recovery with the same policy must not restart; a subsequent adherence-only
	// change must restart so bootstrap can resolve the new complete policy.
	w.readProfileState = func(context.Context) (profileState, error) {
		return profileState{profile: initial, adherence: TLSAdherenceStrictAllComponents}, nil
	}
	if result := mustReconcile(t, w); result.RequeueAfter != 0 || restarts != 0 {
		t.Fatal("recovery with unchanged policy should not restart or requeue")
	}
	w.readProfileState = func(context.Context) (profileState, error) {
		return profileState{profile: initial, adherence: TLSAdherenceLegacyAdheringComponentsOnly}, nil
	}
	mustReconcile(t, w)
	if restarts != 1 {
		t.Fatal("adherence change after recovery must restart")
	}
	if !reflect.DeepEqual(CurrentProxyTLSArguments(), args) {
		t.Fatal("watcher must leave publishing to the next successful bootstrap")
	}
}

func TestReconcile_MultipleReconciles(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	scheme := newTestScheme(t)

	apiServer := newAPIServerWithProfile(profile)
	apiServer.Name = "cluster"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiServer).Build()

	callCount := 0
	w := NewProfileWatcher(c, nil, func() { callCount++ })

	mustReconcile(t, w)
	if callCount != 1 {
		t.Errorf("expected 1 callback on first reconcile, got %d", callCount)
	}

	mustReconcile(t, w)
	if callCount != 1 {
		t.Errorf("expected callback count to stay at 1 after unchanged reconcile, got %d", callCount)
	}
}
