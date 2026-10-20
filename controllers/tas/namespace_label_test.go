package tas

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testTrustyAINamespace = "models"

func TestEnsureTrustyAINamespaceLabel(t *testing.T) {
	ctx := context.Background()
	scheme := clientgoscheme(t)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testTrustyAINamespace}}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace).Build()
	apiReader := &trackingReader{Reader: k8sClient}
	reconciler := &TrustyAIServiceReconciler{Client: k8sClient, APIReader: apiReader}

	require.NoError(t, reconciler.ensureTrustyAINamespaceLabel(ctx, testTrustyAINamespace))
	assert.Equal(t, 1, apiReader.getCalls)

	updated := &corev1.Namespace{}
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: testTrustyAINamespace}, updated))
	assert.Equal(t, "true", updated.Labels[trustyAINamespaceLabel])
	assert.Equal(t, "true", updated.Annotations[trustyAINamespaceLabelAnnotation])
}

func TestEnsureTrustyAINamespaceLabelPreservesExistingValue(t *testing.T) {
	ctx := context.Background()
	scheme := clientgoscheme(t)
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testTrustyAINamespace,
			Labels: map[string]string{trustyAINamespaceLabel: "false"},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace).Build()
	reconciler := &TrustyAIServiceReconciler{Client: k8sClient}

	require.NoError(t, reconciler.ensureTrustyAINamespaceLabel(ctx, testTrustyAINamespace))

	updated := &corev1.Namespace{}
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: testTrustyAINamespace}, updated))
	assert.Equal(t, "false", updated.Labels[trustyAINamespaceLabel])
	assert.NotContains(t, updated.Annotations, trustyAINamespaceLabelAnnotation)
}

func TestRemoveTrustyAINamespaceLabelKeepsLabelForAnotherService(t *testing.T) {
	ctx := context.Background()
	scheme := clientgoscheme(t)
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testTrustyAINamespace,
			Labels: map[string]string{
				trustyAINamespaceLabel: "true",
			},
			Annotations: map[string]string{
				trustyAINamespaceLabelAnnotation: "true",
			},
		},
	}
	first := &trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "first", Namespace: testTrustyAINamespace},
	}
	second := &trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "second", Namespace: testTrustyAINamespace},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace, first, second).Build()
	reconciler := &TrustyAIServiceReconciler{Client: k8sClient}

	require.NoError(t, reconciler.removeTrustyAINamespaceLabel(ctx, first))

	updated := &corev1.Namespace{}
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: testTrustyAINamespace}, updated))
	assert.Equal(t, "true", updated.Labels[trustyAINamespaceLabel])
}

func TestRemoveTrustyAINamespaceLabelAfterLastService(t *testing.T) {
	ctx := context.Background()
	scheme := clientgoscheme(t)
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testTrustyAINamespace,
			Labels: map[string]string{trustyAINamespaceLabel: "true"},
			Annotations: map[string]string{
				trustyAINamespaceLabelAnnotation: "true",
			},
		},
	}
	service := &trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "only", Namespace: testTrustyAINamespace},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace, service).Build()
	apiReader := &trackingReader{Reader: k8sClient}
	reconciler := &TrustyAIServiceReconciler{Client: k8sClient, APIReader: apiReader}

	require.NoError(t, reconciler.removeTrustyAINamespaceLabel(ctx, service))
	assert.Equal(t, 1, apiReader.getCalls)

	updated := &corev1.Namespace{}
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: testTrustyAINamespace}, updated))
	assert.NotContains(t, updated.Labels, trustyAINamespaceLabel)
	assert.NotContains(t, updated.Annotations, trustyAINamespaceLabelAnnotation)
}

func TestRemoveTrustyAINamespaceLabelPreservesPreexistingTrueValue(t *testing.T) {
	ctx := context.Background()
	scheme := clientgoscheme(t)
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testTrustyAINamespace,
			Labels: map[string]string{trustyAINamespaceLabel: "true"},
		},
	}
	service := &trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "only", Namespace: testTrustyAINamespace},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace, service).Build()
	reconciler := &TrustyAIServiceReconciler{Client: k8sClient}

	require.NoError(t, reconciler.removeTrustyAINamespaceLabel(ctx, service))

	updated := &corev1.Namespace{}
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: testTrustyAINamespace}, updated))
	assert.Equal(t, "true", updated.Labels[trustyAINamespaceLabel])
	assert.NotContains(t, updated.Annotations, trustyAINamespaceLabelAnnotation)
}

func TestRemoveTrustyAINamespaceLabelPreservesExistingNonOperatorValue(t *testing.T) {
	ctx := context.Background()
	scheme := clientgoscheme(t)
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   testTrustyAINamespace,
			Labels: map[string]string{trustyAINamespaceLabel: "false"},
		},
	}
	service := &trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "only", Namespace: testTrustyAINamespace},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace, service).Build()
	reconciler := &TrustyAIServiceReconciler{Client: k8sClient}

	require.NoError(t, reconciler.removeTrustyAINamespaceLabel(ctx, service))

	updated := &corev1.Namespace{}
	require.NoError(t, k8sClient.Get(ctx, types.NamespacedName{Name: testTrustyAINamespace}, updated))
	assert.Equal(t, "false", updated.Labels[trustyAINamespaceLabel])
}

func clientgoscheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, trustyaiopendatahubiov1.AddToScheme(scheme))
	return scheme
}

type trackingReader struct {
	client.Reader
	getCalls int
}

func (r *trackingReader) Get(
	ctx context.Context,
	key types.NamespacedName,
	obj client.Object,
	opts ...client.GetOption,
) error {
	r.getCalls++
	return r.Reader.Get(ctx, key, obj, opts...)
}
