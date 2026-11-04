package tlsprofile

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestWatcherRestartsOnProfileOrAdherenceChange(t *testing.T) {
	for _, test := range []struct {
		name  string
		state profileState
	}{
		{
			name: "profile change",
			state: profileState{
				profile:   &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType},
				adherence: tlsAdherenceStrict,
			},
		},
		{
			name: "adherence change",
			state: profileState{
				profile:   &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType},
				adherence: tlsAdherenceStrict,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			restarted := false
			watcher := NewSecurityProfileWatcher(
				nil,
				&configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType},
				tlsAdherenceNoOpinion,
				func() { restarted = true },
			)
			watcher.readProfileState = func(context.Context) (profileState, error) { return test.state, nil }
			if _, err := watcher.Reconcile(context.Background(), reconcile.Request{}); err != nil {
				t.Fatal(err)
			}
			if !restarted {
				t.Fatal("policy change did not trigger restart")
			}
		})
	}
}

func TestWatcherRetriesTransientAndRestartsOnDeniedRead(t *testing.T) {
	watcher := NewSecurityProfileWatcher(nil, nil, tlsAdherenceNoOpinion, nil)
	watcher.readProfileState = func(context.Context) (profileState, error) {
		return profileState{}, apierrors.NewServiceUnavailable("temporary")
	}
	result, err := watcher.Reconcile(context.Background(), reconcile.Request{})
	if err != nil || result.RequeueAfter != profileRetryInterval {
		t.Fatalf("transient result = %#v, %v", result, err)
	}

	restarted := false
	watcher.onPolicyChange = func() { restarted = true }
	watcher.readProfileState = func(context.Context) (profileState, error) {
		return profileState{}, apierrors.NewForbidden(
			schema.GroupResource{Resource: "apiservers"},
			apiServerName,
			nil,
		)
	}
	result, err = watcher.Reconcile(context.Background(), reconcile.Request{})
	if err != nil || result.RequeueAfter != 0 || !restarted {
		t.Fatalf("denied result = %#v, %v, restarted = %v", result, err, restarted)
	}
}
