package tas

import (
	"context"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestReconcileTASNetworkPolicyStopsOnServerlessConfigReadError(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new policy"
		if existing {
			name = "existing serverless policy"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			scheme, owner := tasNetworkPolicyFixture(t)
			configMap := createConfigMap(operatorNamespace, testKubeRBACProxyImage, testTrustAIServiceImage)
			configMap.Data[configMapkServeServerlessKey] = "enabled"
			failRead := false
			writes := 0
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, configMap).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.ConfigMap); ok && failRead {
							return apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, key.Name, nil)
						}
						return c.Get(ctx, key, obj, opts...)
					},
					Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
						writes++
						return c.Create(ctx, obj, opts...)
					},
					Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						writes++
						return c.Update(ctx, obj, opts...)
					},
				}).Build()
			r := &TrustyAIServiceReconciler{Client: c, Scheme: scheme, Namespace: operatorNamespace}
			var before *networkingv1.NetworkPolicy
			if existing {
				if err := r.reconcileNetworkPolicy(ctx, owner); err != nil {
					t.Fatal(err)
				}
				policies := &networkingv1.NetworkPolicyList{}
				if err := c.List(ctx, policies); err != nil {
					t.Fatal(err)
				}
				if len(policies.Items) != 1 {
					t.Fatalf("got %d policies, want one existing policy", len(policies.Items))
				}
				before = policies.Items[0].DeepCopy()
				if !ruleGrantsKServePeerPort(before.Spec.Ingress, 8080) {
					t.Fatal("initial policy does not allow the serverless logger")
				}
			}
			failRead = true
			writes = 0
			err := r.reconcileNetworkPolicy(ctx, owner)
			if err == nil || !strings.Contains(err.Error(), "read KServeServerless configuration for TAS NetworkPolicy") {
				t.Errorf("got error %v, want contextual configuration read error", err)
			}
			if writes != 0 {
				t.Errorf("attempted %d writes after configuration read failed", writes)
			}
			policies := &networkingv1.NetworkPolicyList{}
			if err := c.List(ctx, policies); err != nil {
				t.Fatal(err)
			}
			if !existing {
				if len(policies.Items) != 0 {
					t.Fatal("created a guessed policy after configuration read failed")
				}
			} else if len(policies.Items) != 1 || !reflect.DeepEqual(before, &policies.Items[0]) {
				t.Fatal("configuration read failure changed the existing serverless policy")
			}
		})
	}
}

func TestReconcileTASNetworkPolicyAllowsMissingServerlessConfigMap(t *testing.T) {
	ctx := context.Background()
	scheme, owner := tasNetworkPolicyFixture(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()
	r := &TrustyAIServiceReconciler{Client: c, Scheme: scheme, Namespace: operatorNamespace}
	if enabled, err := r.getKServeServerlessConfig(ctx); err != nil || enabled {
		t.Fatalf("missing ConfigMap: enabled=%v, err=%v; want false, nil", enabled, err)
	}
	if err := r.reconcileNetworkPolicy(ctx, owner); err != nil {
		t.Fatal(err)
	}
	policies := &networkingv1.NetworkPolicyList{}
	if err := c.List(ctx, policies); err != nil {
		t.Fatal(err)
	}
	if len(policies.Items) != 1 {
		t.Fatalf("got %d policies, want one policy", len(policies.Items))
	}
	if ruleGrantsKServePeerPort(policies.Items[0].Spec.Ingress, 8080) {
		t.Fatal("missing ConfigMap unexpectedly enabled the serverless logger rule")
	}
}
