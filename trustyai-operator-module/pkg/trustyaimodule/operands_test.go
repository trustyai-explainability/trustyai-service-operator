package trustyaimodule

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var trustyAIServiceGVK = schema.GroupVersionKind{
	Group:   "trustyai.opendatahub.io",
	Version: "v1",
	Kind:    "TrustyAIService",
}

func newTrustyAIService(namespace, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "trustyai.opendatahub.io/v1",
		"kind":       "TrustyAIService",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]interface{}{
			"metrics": map[string]interface{}{
				"schedule": "0 0 * * *",
			},
			"storage": map[string]interface{}{
				"format": "PVC",
			},
		},
	}}
	obj.SetGroupVersionKind(trustyAIServiceGVK)
	return obj
}

func markOperandReady(obj *unstructured.Unstructured) *unstructured.Unstructured {
	ready := obj.DeepCopy()
	_ = unstructured.SetNestedSlice(ready.Object, []interface{}{
		map[string]interface{}{
			"type":   "Ready",
			"status": "True",
			"reason": "Available",
		},
	}, "status", "conditions")
	return ready
}

func createHealthyTrustyAIService(ctx context.Context, c client.Client, namespace, name string) error {
	operand := newTrustyAIService(namespace, name)
	if err := c.Create(ctx, operand); err != nil {
		return err
	}
	return c.Status().Update(ctx, markOperandReady(operand))
}
