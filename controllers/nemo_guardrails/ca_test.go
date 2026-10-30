package nemo_guardrails

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestLoadCAConfigsRejectsForeignCABundleNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "user-ca", Namespace: "other-ns"},
		Data:       map[string]string{"ca.crt": "secret"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(foreign).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}
	cr := nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo", Namespace: "models", UID: "cr-uid"},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
			CABundleConfig: &common.CABundleConfig{
				ConfigMapName:      "user-ca",
				ConfigMapNamespace: "other-ns",
				ConfigMapKeys:      []string{"ca.crt"},
			},
		},
	}

	_, mounted, status := reconciler.LoadCAConfigs(context.Background(), logr.Discard(), cr, "redhat-ai-gateway-infra")
	require.False(t, status.UserCAFound)
	require.Contains(t, status.UserCAError, `"other-ns"`)
	require.Contains(t, status.UserCAError, `"models"`)
	require.Empty(t, mounted)

	copyName := copiedObjectName("models", "nemo", "user-ca", "redhat-ai-gateway-infra")
	err := c.Get(context.Background(), types.NamespacedName{Name: copyName, Namespace: "redhat-ai-gateway-infra"}, &corev1.ConfigMap{})
	require.True(t, apierrors.IsNotFound(err))

	names, err := reconciler.copiedConfigMapNames(context.Background(), &cr, "redhat-ai-gateway-infra")
	require.NoError(t, err)
	require.NotContains(t, names, copyName)
}

func TestLoadCAConfigsMountsCABundleFromCRNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "user-ca", Namespace: "models"},
		Data:       map[string]string{"ca.crt": "pem"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}
	cr := nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo", Namespace: "models", UID: "cr-uid"},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
			CABundleConfig: &common.CABundleConfig{ConfigMapName: "user-ca", ConfigMapKeys: []string{"ca.crt"}},
		},
	}

	_, mounted, status := reconciler.LoadCAConfigs(context.Background(), logr.Discard(), cr, "models")
	require.True(t, status.UserCAFound)
	require.Empty(t, status.UserCAError)
	require.Len(t, mounted, 1)
	require.Equal(t, "models", mounted[0].Namespace)
	require.Equal(t, "user-ca", mounted[0].Name)
}

func TestCopiedConfigMapNamesKeepsCRNamespaceCABundle(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cr := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo", Namespace: "models", UID: "cr-uid"},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
			CABundleConfig: &common.CABundleConfig{ConfigMapName: "user-ca", ConfigMapNamespace: "models"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "user-ca", Namespace: "models"},
	}).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	names, err := reconciler.copiedConfigMapNames(context.Background(), cr, "redhat-ai-gateway-infra")
	require.NoError(t, err)
	require.Equal(t, []string{copiedObjectName("models", "nemo", "user-ca", "redhat-ai-gateway-infra")}, names)
}
