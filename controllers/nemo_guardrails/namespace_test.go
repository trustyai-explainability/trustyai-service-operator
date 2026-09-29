package nemo_guardrails

import (
	"context"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreateDeploymentUsesSpecNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	cr := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nemo-guardrails",
			Namespace: "test-ns",
			UID:       "cr-uid",
		},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
			Namespace: "models",
			NemoConfigs: []nemoguardrailsv1alpha1.NemoConfig{{
				Name:       "nemo-config",
				ConfigMaps: []string{"nemo-config"},
			}},
		},
	}
	operatorConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: constants.ConfigMap, Namespace: "operator-ns"},
		Data:       map[string]string{nemoGuardrailsImageKey: "quay.io/trustyai/nemo-guardrails:test"},
	}
	userConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-config", Namespace: "test-ns"},
		Data:       map[string]string{"config.yaml": "models: []"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(operatorConfig, userConfig).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme, Namespace: "operator-ns"}

	deployment, err := reconciler.createDeployment(context.Background(), cr, utils.CABundleInitContainerConfig{}, nil)
	require.NoError(t, err)
	require.Equal(t, "models", deployment.Namespace)
	require.Empty(t, deployment.OwnerReferences)
	require.Equal(t, "cr-uid", deployment.Labels[utils.OwnerUIDLabel])

	copied := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "nemo-config", Namespace: "models"}, copied))
	require.Equal(t, "models: []", copied.Data["config.yaml"])

	source := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(userConfig), source))
	require.Equal(t, "true", source.Labels["nemo-guardrails-config"])
}

func TestEnsureConfigMapDoesNotReplaceUnownedConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cr := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails", Namespace: "control-plane", UID: "cr-uid"},
	}
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-config", Namespace: "models"},
		Data:       map[string]string{"config.yaml": "keep: me"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	_, err := reconciler.ensureConfigMapInNamespace(context.Background(), cr, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-config", Namespace: "control-plane"},
		Data:       map[string]string{"config.yaml": "overwrite"},
	}, "models")
	require.Error(t, err)

	got := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existing), got))
	require.Equal(t, "keep: me", got.Data["config.yaml"])
	require.Empty(t, got.Labels)
}

func TestValidateWorkloadNamespaceRequiresOptIn(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	allowed := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "models",
		Labels: map[string]string{WorkloadNamespaceLabel: WorkloadNamespaceLabelValue},
	}}
	denied := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(allowed, denied).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	require.NoError(t, reconciler.validateWorkloadNamespace(context.Background(), "models"))
	require.Error(t, reconciler.validateWorkloadNamespace(context.Background(), "other"))
	require.Error(t, reconciler.validateWorkloadNamespace(context.Background(), "missing"))
}

func TestReleasePreviousWorkloadKeepsOldNamespaceOnCollision(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, routev1.AddToScheme(scheme))
	expose := true
	cr := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nemo-guardrails", Namespace: "control-plane", UID: "cr-uid",
			Annotations: map[string]string{"security.opendatahub.io/enable-auth": "true"},
		},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{Namespace: "models", ExposeRoute: &expose},
		Status: nemoguardrailsv1alpha1.NemoGuardrailStatus{
			WorkloadNamespace: "old",
		},
	}
	current := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "nemo-guardrails", Namespace: "old",
		Labels: map[string]string{utils.OwnerUIDLabel: string(cr.UID)},
	}}
	collision := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails", Namespace: "models"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
	}
	unownedAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails-serviceaccount", Namespace: "models"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current, collision, unownedAccount).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	require.Error(t, reconciler.releasePreviousWorkload(context.Background(), cr, "models"))
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(current), &appsv1.Deployment{}))
	got := &appsv1.Deployment{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(collision), got))
	require.Equal(t, int32(3), *got.Spec.Replicas)
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(unownedAccount), &corev1.ServiceAccount{}))
}

func TestReleasePreviousWorkloadKeepsOldNamespaceOnConfigMapCollision(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, routev1.AddToScheme(scheme))
	defaultName := nemoGuardrailsDefaultConfigPrefix + "-rails"
	cr := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails", Namespace: "control-plane", UID: "cr-uid"},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
			Namespace: "models",
			NemoConfigs: []nemoguardrailsv1alpha1.NemoConfig{{
				Name:       "nemo-config",
				ConfigMaps: []string{"nemo-config", defaultName},
			}},
			CABundleConfig: &common.CABundleConfig{ConfigMapName: "user-ca", ConfigMapNamespace: "control-plane"},
		},
		Status: nemoguardrailsv1alpha1.NemoGuardrailStatus{WorkloadNamespace: "old"},
	}
	current := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "nemo-guardrails", Namespace: "old",
		Labels: map[string]string{utils.OwnerUIDLabel: string(cr.UID)},
	}}
	userCopy := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-config", Namespace: "models"},
		Data:       map[string]string{"config.yaml": "keep: me"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current, userCopy, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "user-ca", Namespace: "control-plane"},
		Data:       map[string]string{"ca.crt": "source"},
	}).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme, Namespace: "operator-ns"}

	require.Error(t, reconciler.releasePreviousWorkload(context.Background(), cr, "models"))
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(current), &appsv1.Deployment{}))
	got := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(userCopy), got))
	require.Equal(t, "keep: me", got.Data["config.yaml"])
	require.Empty(t, got.Labels)
}

func TestCopiedConfigMapNamesSkipInPlaceConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	defaultName := nemoGuardrailsDefaultConfigPrefix + "-rails"
	cr := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails", Namespace: "models", UID: "cr-uid"},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{
			NemoConfigs: []nemoguardrailsv1alpha1.NemoConfig{{
				Name:       "local",
				ConfigMaps: []string{"local-config", defaultName},
			}},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: defaultName, Namespace: "operator-ns"},
	}).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme, Namespace: "operator-ns"}

	names, err := reconciler.copiedConfigMapNames(context.Background(), cr, "models")
	require.NoError(t, err)
	require.Equal(t, []string{"nemo-guardrails-" + defaultName}, names)
}

func TestRejectUnownedServiceAccount(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, routev1.AddToScheme(scheme))
	cr := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nemo-guardrails", Namespace: "control-plane", UID: "cr-uid",
			Annotations: map[string]string{"security.opendatahub.io/enable-auth": "true"},
		},
		Spec: nemoguardrailsv1alpha1.NemoGuardrailsSpec{Namespace: "models"},
	}
	account := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails-serviceaccount", Namespace: "models"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(account).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	require.Error(t, reconciler.rejectUnownedWorkload(context.Background(), cr, "models"))
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(account), &corev1.ServiceAccount{}))
}

func TestRequireOwnedOrAbsentLeavesUnownedDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, routev1.AddToScheme(scheme))
	cr := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails", Namespace: "control-plane", UID: "cr-uid"},
	}
	existing := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails", Namespace: "models"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
	}
	owned := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "nemo-guardrails", Namespace: "models",
		Labels: map[string]string{utils.OwnerUIDLabel: string(cr.UID)},
	}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing, owned).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	err := reconciler.requireOwnedOrAbsent(context.Background(), cr, "Deployment", &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails", Namespace: "models"}})
	require.Error(t, err)
	require.NoError(t, reconciler.requireOwnedOrAbsent(context.Background(), cr, "Service", &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails", Namespace: "models"}}))
	require.NoError(t, reconciler.requireOwnedOrAbsent(context.Background(), cr, "Route", &routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "missing", Namespace: "models"}}))

	got := &appsv1.Deployment{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existing), got))
	require.Equal(t, int32(3), *got.Spec.Replicas)
}

func int32Ptr(v int32) *int32 { return &v }

func TestDeleteOwnedInNamespaceLeavesOtherObjects(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, routev1.AddToScheme(scheme))
	cr := &nemoguardrailsv1alpha1.NemoGuardrails{
		ObjectMeta: metav1.ObjectMeta{Name: "nemo-guardrails", Namespace: "control-plane", UID: "cr-uid"},
	}
	legacy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "nemo-guardrails", Namespace: "old",
		OwnerReferences: []metav1.OwnerReference{{UID: cr.UID}},
	}}
	labeled := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "copied", Namespace: "old",
		Labels: map[string]string{utils.OwnerUIDLabel: string(cr.UID)},
	}}
	other := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "someone-else", Namespace: "old"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacy, labeled, other).Build()
	reconciler := &NemoGuardrailsReconciler{Client: c, Scheme: scheme}

	require.NoError(t, reconciler.deleteOwnedInNamespace(context.Background(), cr, "old"))

	require.Error(t, c.Get(context.Background(), client.ObjectKeyFromObject(legacy), &appsv1.Deployment{}))
	require.Error(t, c.Get(context.Background(), client.ObjectKeyFromObject(labeled), &corev1.ConfigMap{}))
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(other), &appsv1.Deployment{}))
}
