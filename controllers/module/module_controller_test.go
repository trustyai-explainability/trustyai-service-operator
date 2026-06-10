package module

import (
	"context"
	"testing"

	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = modulev1alpha1.AddToScheme(s)
	return s
}

func newModuleCR() *modulev1alpha1.TrustyAI {
	return &modulev1alpha1.TrustyAI{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "default",
			Generation: 1,
		},
		Spec: modulev1alpha1.TrustyAISpec{
			ManagementState: "Managed",
		},
	}
}

func newReconciler(objs ...runtime.Object) *Reconciler {
	scheme := newScheme()
	cb := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&modulev1alpha1.TrustyAI{})
	for _, obj := range objs {
		cb = cb.WithRuntimeObjects(obj)
	}
	return &Reconciler{
		Client:   cb.Build(),
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}
}

func TestReconcileNotFound(t *testing.T) {
	r := newReconciler()
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for missing CR")
	}
}

func TestReconcileAddsFinalizerOnFirstPass(t *testing.T) {
	cr := newModuleCR()
	r := newReconciler(cr)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after adding finalizer")
	}

	updated := &modulev1alpha1.TrustyAI{}
	_ = r.Get(context.Background(), types.NamespacedName{Name: "default"}, updated)
	hasFinalizer := false
	for _, f := range updated.Finalizers {
		if f == finalizerName {
			hasFinalizer = true
		}
	}
	if !hasFinalizer {
		t.Error("expected finalizer to be added")
	}
}

func TestReconcileSetsStatusConditions(t *testing.T) {
	cr := newModuleCR()
	cr.Finalizers = []string{finalizerName}
	r := newReconciler(cr)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &modulev1alpha1.TrustyAI{}
	_ = r.Get(context.Background(), types.NamespacedName{Name: "default"}, updated)

	readyCond := meta.FindStatusCondition(updated.Status.Conditions, ConditionReady)
	if readyCond == nil {
		t.Fatal("expected Ready condition to be set")
	}
	if readyCond.Status != metav1.ConditionTrue {
		t.Errorf("Ready = %s, want True", readyCond.Status)
	}

	provCond := meta.FindStatusCondition(updated.Status.Conditions, ConditionProvisioningSucceeded)
	if provCond == nil {
		t.Fatal("expected ProvisioningSucceeded condition to be set")
	}
	if provCond.Status != metav1.ConditionTrue {
		t.Errorf("ProvisioningSucceeded = %s, want True", provCond.Status)
	}

	degradedCond := meta.FindStatusCondition(updated.Status.Conditions, ConditionDegraded)
	if degradedCond == nil {
		t.Fatal("expected Degraded condition to be set")
	}
	if degradedCond.Status != metav1.ConditionFalse {
		t.Errorf("Degraded = %s, want False", degradedCond.Status)
	}

	if updated.Status.Phase != PhaseReady {
		t.Errorf("Phase = %s, want %s", updated.Status.Phase, PhaseReady)
	}

	if updated.Status.ObservedGeneration != 1 {
		t.Errorf("ObservedGeneration = %d, want 1", updated.Status.ObservedGeneration)
	}
}

func TestReconcileSetsReleases(t *testing.T) {
	cr := newModuleCR()
	cr.Finalizers = []string{finalizerName}
	r := newReconciler(cr)

	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "default"},
	})

	updated := &modulev1alpha1.TrustyAI{}
	_ = r.Get(context.Background(), types.NamespacedName{Name: "default"}, updated)

	if len(updated.Status.Releases) == 0 {
		t.Fatal("expected at least one release entry")
	}
	if updated.Status.Releases[0].Name != "trustyai-service-operator" {
		t.Errorf("release name = %s, want trustyai-service-operator", updated.Status.Releases[0].Name)
	}
}

type mockHealthChecker struct {
	name    string
	healthy bool
	reason  string
}

func (m *mockHealthChecker) Name() string                               { return m.name }
func (m *mockHealthChecker) IsHealthy(_ context.Context) (bool, string) { return m.healthy, m.reason }

func TestReconcileWithDegradedService(t *testing.T) {
	cr := newModuleCR()
	cr.Finalizers = []string{finalizerName}
	r := newReconciler(cr)
	r.HealthCheckers = []ServiceHealthChecker{
		&mockHealthChecker{name: "TAS", healthy: true},
		&mockHealthChecker{name: "LMES", healthy: false, reason: "controller not running"},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &modulev1alpha1.TrustyAI{}
	_ = r.Get(context.Background(), types.NamespacedName{Name: "default"}, updated)

	readyCond := meta.FindStatusCondition(updated.Status.Conditions, ConditionReady)
	if readyCond == nil || readyCond.Status != metav1.ConditionFalse {
		t.Errorf("Ready should be False when a service is degraded")
	}

	degradedCond := meta.FindStatusCondition(updated.Status.Conditions, ConditionDegraded)
	if degradedCond == nil || degradedCond.Status != metav1.ConditionTrue {
		t.Errorf("Degraded should be True when a service is unhealthy")
	}

	if updated.Status.Phase != PhaseNotReady {
		t.Errorf("Phase = %s, want %s", updated.Status.Phase, PhaseNotReady)
	}
}

func TestReconcileAllHealthy(t *testing.T) {
	cr := newModuleCR()
	cr.Finalizers = []string{finalizerName}
	r := newReconciler(cr)
	r.HealthCheckers = []ServiceHealthChecker{
		&mockHealthChecker{name: "TAS", healthy: true},
		&mockHealthChecker{name: "LMES", healthy: true},
		&mockHealthChecker{name: "EVALHUB", healthy: true},
	}

	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "default"},
	})

	updated := &modulev1alpha1.TrustyAI{}
	_ = r.Get(context.Background(), types.NamespacedName{Name: "default"}, updated)

	readyCond := meta.FindStatusCondition(updated.Status.Conditions, ConditionReady)
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		t.Error("Ready should be True when all services are healthy")
	}
	if updated.Status.Phase != PhaseReady {
		t.Errorf("Phase = %s, want %s", updated.Status.Phase, PhaseReady)
	}
}

func TestHandleDeletionRemovesFinalizer(t *testing.T) {
	cr := newModuleCR()
	cr.Finalizers = []string{finalizerName}
	now := metav1.Now()
	cr.DeletionTimestamp = &now
	// fake client requires a deletion grace period for objects with a deletion timestamp
	cr.DeletionGracePeriodSeconds = new(int64)

	r := newReconciler(cr)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &modulev1alpha1.TrustyAI{}
	_ = r.Get(context.Background(), types.NamespacedName{Name: "default"}, updated)
	for _, f := range updated.Finalizers {
		if f == finalizerName {
			t.Error("finalizer should be removed during deletion")
		}
	}
}

func TestObservedGenerationTracksSpecChanges(t *testing.T) {
	cr := newModuleCR()
	cr.Finalizers = []string{finalizerName}
	cr.Generation = 5
	r := newReconciler(cr)

	_, _ = r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "default"},
	})

	updated := &modulev1alpha1.TrustyAI{}
	_ = r.Get(context.Background(), types.NamespacedName{Name: "default"}, updated)

	if updated.Status.ObservedGeneration != 5 {
		t.Errorf("ObservedGeneration = %d, want 5", updated.Status.ObservedGeneration)
	}
}
