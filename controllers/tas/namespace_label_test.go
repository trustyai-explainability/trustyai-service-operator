package tas

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
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

func TestReconcileRetainsFinalizerWhenNamespaceLabelCleanupFails(t *testing.T) {
	ctx := context.Background()
	scheme := clientgoscheme(t)
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))

	cleanupErr := errors.New("namespace patch failed")
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
	deletingService := &trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleting-service",
			Namespace:         testTrustyAINamespace,
			DeletionTimestamp: &metav1.Time{Time: time.Unix(1, 0)},
			Finalizers:        []string{finalizerName},
		},
	}
	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace).Build()
	reconcilerClient := &namespacePatchErrorClient{
		Client:          baseClient,
		err:             cleanupErr,
		deletingService: deletingService,
	}
	reconciler := &TrustyAIServiceReconciler{Client: reconcilerClient, Scheme: scheme}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      deletingService.Name,
			Namespace: deletingService.Namespace,
		},
	})

	assert.ErrorIs(t, err, cleanupErr)
	assert.True(t, result.Requeue)
	assert.Zero(t, reconcilerClient.updateCalls)
	assert.Contains(t, deletingService.Finalizers, finalizerName)
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

type namespacePatchErrorClient struct {
	client.Client
	err             error
	deletingService *trustyaiopendatahubiov1.TrustyAIService
	updateCalls     int
}

func (c *namespacePatchErrorClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if service, ok := obj.(*trustyaiopendatahubiov1.TrustyAIService); ok &&
		key == client.ObjectKeyFromObject(c.deletingService) {
		c.deletingService.DeepCopyInto(service)
		return nil
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *namespacePatchErrorClient) Update(
	ctx context.Context,
	obj client.Object,
	opts ...client.UpdateOption,
) error {
	if _, ok := obj.(*trustyaiopendatahubiov1.TrustyAIService); ok {
		c.updateCalls++
	}
	return c.Client.Update(ctx, obj, opts...)
}

func (c *namespacePatchErrorClient) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	if _, ok := obj.(*corev1.Namespace); ok {
		return c.err
	}
	return c.Client.Patch(ctx, obj, patch, opts...)
}
