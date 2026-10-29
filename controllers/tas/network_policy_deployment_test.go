package tas

import (
	"context"
	"testing"

	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTASDeploymentPathsRetainNetworkPolicyIdentity(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			scheme, owner := tasNetworkPolicyFixture(t)
			owner.Spec.Storage.Format = STORAGE_DATABASE
			owner.Spec.Storage.DatabaseConfigurations = "db"
			objects := []client.Object{owner, createConfigMap(operatorNamespace, testKubeRBACProxyImage, testTrustAIServiceImage)}
			if existing {
				deployment, _, _ := tasNetworkPolicyWorkloadFixture(owner)
				deployment.Spec.Template.Spec.Containers = []corev1.Container{{Name: "trustyai-service", Image: testTrustAIServiceImage}}
				objects = append(objects, deployment)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			r := &TrustyAIServiceReconciler{Client: c, Scheme: scheme, Namespace: operatorNamespace, EventRecorder: record.NewFakeRecorder(10)}
			if err := r.ensureDeployment(ctx, owner, CustomCertificatesBundle{}, false); err != nil {
				t.Fatal(err)
			}
			deployment := &appsv1.Deployment{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(owner), deployment); err != nil {
				t.Fatal(err)
			}
			identity, err := tasNetworkPolicyIdentity(owner)
			if err != nil {
				t.Fatal(err)
			}
			want, err := identity.Labels()
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range want {
				if deployment.Spec.Template.Labels[key] != value {
					t.Errorf("template identity %s = %q, want %q", key, deployment.Spec.Template.Labels[key], value)
				}
				if _, found := deployment.Spec.Selector.MatchLabels[key]; found {
					t.Errorf("reserved identity %s added to immutable selector", key)
				}
			}
			if deployment.Spec.Template.Labels[utils.NetworkPolicyComponentLabel] != "tas" {
				t.Fatal("Deployment is outside the TAS policy selector")
			}
		})
	}
}
