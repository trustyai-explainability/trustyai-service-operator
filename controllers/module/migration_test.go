package module

import (
	"context"
	"testing"

	modulev1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/module/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newMigrationReconciler(objs ...runtime.Object) *Reconciler {
	scheme := runtime.NewScheme()
	_ = modulev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	cb := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&modulev1alpha1.TrustyAI{})
	for _, obj := range objs {
		cb = cb.WithRuntimeObjects(obj)
	}
	return &Reconciler{
		Client:   cb.Build(),
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
	}
}

func TestIsMigrationCompleted(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{"nil annotations", nil, false},
		{"empty annotations", map[string]string{}, false},
		{"other annotation", map[string]string{"foo": "bar"}, false},
		{"migration false", map[string]string{migrationAnnotation: "false"}, false},
		{"migration true", map[string]string{migrationAnnotation: "true"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &modulev1alpha1.TrustyAI{
				ObjectMeta: metav1.ObjectMeta{Annotations: tt.annotations},
			}
			if got := isMigrationCompleted(cr); got != tt.want {
				t.Errorf("isMigrationCompleted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunMigrationSkipsWhenAlreadyCompleted(t *testing.T) {
	cr := newModuleCR()
	cr.Finalizers = []string{finalizerName}
	cr.Annotations = map[string]string{migrationAnnotation: "true"}
	r := newMigrationReconciler(cr)

	err := r.runMigration(context.Background(), cr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMigrationFreshInstall(t *testing.T) {
	t.Setenv("APPLICATIONS_NAMESPACE", "opendatahub")

	cr := newModuleCR()
	cr.Finalizers = []string{finalizerName}
	r := newMigrationReconciler(cr)

	err := r.runMigration(context.Background(), cr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &modulev1alpha1.TrustyAI{}
	_ = r.Get(context.Background(), types.NamespacedName{Name: "default"}, updated)
	if updated.Annotations[migrationAnnotation] != "true" {
		t.Error("expected migration annotation to be set")
	}
}

func TestRunMigrationAdoptsConfigMap(t *testing.T) {
	t.Setenv("APPLICATIONS_NAMESPACE", "opendatahub")

	cr := newModuleCR()
	cr.Finalizers = []string{finalizerName}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.ConfigMap,
			Namespace: "opendatahub",
		},
		Data: map[string]string{
			"trustyaiServiceImage": "quay.io/old/image:v1",
			"kube-rbac-proxy":      "quay.io/old/rbac:v1",
		},
	}

	r := newMigrationReconciler(cr, cm)

	err := r.runMigration(context.Background(), cr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify ConfigMap still exists with same data
	updatedCM := &corev1.ConfigMap{}
	err = r.Get(context.Background(), types.NamespacedName{
		Name: constants.ConfigMap, Namespace: "opendatahub",
	}, updatedCM)
	if err != nil {
		t.Fatalf("ConfigMap should still exist: %v", err)
	}
	if updatedCM.Data["trustyaiServiceImage"] != "quay.io/old/image:v1" {
		t.Errorf("ConfigMap data should be preserved, got %v", updatedCM.Data)
	}

	// Verify migration annotation
	updated := &modulev1alpha1.TrustyAI{}
	_ = r.Get(context.Background(), types.NamespacedName{Name: "default"}, updated)
	if updated.Annotations[migrationAnnotation] != "true" {
		t.Error("expected migration annotation to be set")
	}
}

func TestRunMigrationIsIdempotent(t *testing.T) {
	t.Setenv("APPLICATIONS_NAMESPACE", "opendatahub")

	cr := newModuleCR()
	cr.Finalizers = []string{finalizerName}
	r := newMigrationReconciler(cr)

	if err := r.runMigration(context.Background(), cr); err != nil {
		t.Fatalf("first run: %v", err)
	}

	updated := &modulev1alpha1.TrustyAI{}
	_ = r.Get(context.Background(), types.NamespacedName{Name: "default"}, updated)

	if err := r.runMigration(context.Background(), updated); err != nil {
		t.Fatalf("second run: %v", err)
	}
}

func TestAdoptConfigMapNotFound(t *testing.T) {
	r := newMigrationReconciler()

	adopted, err := r.adoptConfigMap(context.Background(), "opendatahub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adopted {
		t.Error("should not report adoption when ConfigMap does not exist")
	}
}

func TestAdoptConfigMapExists(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.ConfigMap,
			Namespace: "opendatahub",
		},
		Data: map[string]string{
			"trustyaiServiceImage": "quay.io/original:v1",
		},
	}

	r := newMigrationReconciler(cm)

	adopted, err := r.adoptConfigMap(context.Background(), "opendatahub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !adopted {
		t.Error("should report adoption when ConfigMap exists")
	}
}
