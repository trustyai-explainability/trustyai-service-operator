package main

import (
	"context"
	"encoding/json"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"

	"github.com/opendatahub-io/odh-platform-utilities/pkg/deploy"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// Inspect the apply payload, rather than relying on fake-client SSA support.
func TestDeployerPreservesControllerResources(t *testing.T) {
	for _, managed := range []string{"", "true", "false"} {
		t.Run("managed="+managed, func(t *testing.T) {
			desired := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "apps/v1", "kind": "Deployment",
				"metadata": map[string]any{"name": "controller", "namespace": "test"},
				"spec": map[string]any{"replicas": int64(1), "template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{
					"name": "manager", "image": "desired:latest", "resources": map[string]any{"limits": map[string]any{"cpu": "900m", "memory": "700Mi"}},
				}}}}},
			}}
			live := desired.DeepCopy()
			live.SetAnnotations(map[string]string{"opendatahub.io/managed": managed})
			_ = unstructured.SetNestedField(live.Object, int64(0), "spec", "replicas")
			containers, _, _ := unstructured.NestedSlice(live.Object, "spec", "template", "spec", "containers")
			container := containers[0].(map[string]any)
			container["resources"] = map[string]any{"limits": map[string]any{"cpu": "1001m", "memory": "4001Mi"}}
			container["image"] = "invalid:latest"
			_ = unstructured.SetNestedSlice(live.Object, containers, "spec", "template", "spec", "containers")
			var applied *unstructured.Unstructured
			cli := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(live).WithInterceptorFuncs(interceptor.Funcs{
				Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
					data, err := json.Marshal(obj)
					if err != nil {
						return err
					}
					applied = &unstructured.Unstructured{}
					return json.Unmarshal(data, applied)
				},
			}).Build()
			err := newDeployer().Deploy(context.Background(), deploy.DeployInput{Client: cli, Resources: []unstructured.Unstructured{*desired}})
			if err != nil {
				t.Fatal(err)
			}
			if managed == "false" {
				if applied != nil {
					t.Fatal("unmanaged Deployment must not be applied")
				}
				return
			}
			if applied == nil {
				t.Fatal("Deployment was not applied")
			}
			wantCPU, wantMemory, wantReplicas := "1001m", "4001Mi", int64(0)
			if managed == "true" {
				wantCPU, wantMemory, wantReplicas = "900m", "700Mi", 1
			}
			replicas, _, _ := unstructured.NestedInt64(applied.Object, "spec", "replicas")
			if replicas != wantReplicas {
				t.Errorf("replicas = %d, want %d", replicas, wantReplicas)
			}
			got, _, _ := unstructured.NestedSlice(applied.Object, "spec", "template", "spec", "containers")
			c := got[0].(map[string]any)
			limits := c["resources"].(map[string]any)["limits"].(map[string]any)
			if limits["cpu"] != wantCPU || limits["memory"] != wantMemory {
				t.Errorf("limits = %v, want cpu=%s memory=%s", limits, wantCPU, wantMemory)
			}
			if c["image"] != "desired:latest" {
				t.Errorf("image drift was preserved: %v", c["image"])
			}
			if applied.GroupVersionKind() != appsv1.SchemeGroupVersion.WithKind("Deployment") {
				t.Fatal("unexpected applied kind")
			}
		})
	}
}
