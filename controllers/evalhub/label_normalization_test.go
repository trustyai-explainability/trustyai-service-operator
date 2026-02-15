package evalhub

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNormalizeDNS1123LabelValue(t *testing.T) {
	out := normalizeDNS1123LabelValue("This_Is.Not DNS1123!!!")
	require.LessOrEqual(t, len(out), 63)
	require.Regexp(t, dns1123LabelRe, out)

	long := strings.Repeat("a", 80) + "-suffix"
	out2 := normalizeDNS1123LabelValue(long)
	require.LessOrEqual(t, len(out2), 63)
	require.Regexp(t, dns1123LabelRe, out2)
	require.Equal(t, out2, normalizeDNS1123LabelValue(long), "normalization should be stable")
}

func TestAuthReviewerCRB_AppNameLabelIsNormalizedWhenBindingNameTooLong(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	ctx := context.Background()

	// Make the generated binding name exceed 63 chars:
	// <name>-<namespace>-auth-reviewer-crb
	instanceName := strings.Repeat("a", 30)
	ns := strings.Repeat("b", 30)

	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: ns,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(evalHub).
		Build()

	reconciler := &EvalHubReconciler{
		Client:        fakeClient,
		Scheme:        scheme,
		Namespace:     ns,
		EventRecorder: record.NewFakeRecorder(10),
	}

	require.NoError(t, reconciler.createServiceAccount(ctx, evalHub))

	crbName := generateAuthReviewerClusterRoleBindingName(evalHub)
	require.Greater(t, len(crbName), 63, "test must exercise the >63 char case")

	crb := &rbacv1.ClusterRoleBinding{}
	require.NoError(t, fakeClient.Get(ctx, types.NamespacedName{Name: crbName}, crb))

	lbl := crb.Labels["app.kubernetes.io/name"]
	require.LessOrEqual(t, len(lbl), 63)
	require.Regexp(t, dns1123LabelRe, lbl)
	require.Equal(t, generateAuthReviewerClusterRoleBindingAppNameLabelValue(evalHub), lbl)
}
