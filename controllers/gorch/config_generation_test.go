package gorch

import (
	"context"
	"fmt"
	"github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	"github.com/stretchr/testify/assert"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

func TestGenerateOrchestratorConfigMap(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	// Register the kserve InferenceService type with the scheme
	s := runtime.NewScheme()
	_ = kservev1beta1.AddToScheme(s)
	_ = gorchv1alpha1.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)

	// Create five test InferenceServices
	var isvcs []client.Object
	var srs []client.Object
	for i := 1; i <= 5; i++ {
		runtimeName := fmt.Sprintf("my-serving-runtime-%d", i)
		isvc := &kservev1beta1.InferenceService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("my-inference-service-%d", i),
				Namespace: ns,
				Labels:    map[string]string{"trustyai/guardrails": "true"},
			},
			Spec: kservev1beta1.InferenceServiceSpec{
				Predictor: kservev1beta1.PredictorSpec{
					Model: &kservev1beta1.ModelSpec{
						Runtime: &runtimeName,
					},
				},
			},
			Status: kservev1beta1.InferenceServiceStatus{
				URL: &apis.URL{
					Scheme: "https",
					Host:   fmt.Sprintf("my-inference-service-%d.test-ns.svc.cluster.local:844%d", i, i),
				},
			},
		}

		sr := &v1alpha1.ServingRuntime{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("my-serving-runtime-%d", i),
				Namespace: ns,
				Labels:    map[string]string{"trustyai/guardrails": "true"},
			},
			Spec: v1alpha1.ServingRuntimeSpec{
				ServingRuntimePodSpec: v1alpha1.ServingRuntimePodSpec{
					Containers: []corev1.Container{
						{
							Name: "predictor",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: int32(8000 + i), // increment port by i
								},
							},
						},
					},
				},
			},
			Status: v1alpha1.ServingRuntimeStatus{},
		}

		isvcs = append(isvcs, isvc)
		srs = append(srs, sr)
	}
	genIsvc := &kservev1beta1.InferenceService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("my-generation-service"),
			Namespace: ns,
		},
		Status: kservev1beta1.InferenceServiceStatus{
			URL: &apis.URL{
				Scheme: "https",
				Host:   "my-generation-service-%d.test-ns.svc.cluster.local:8080",
			},
		},
	}
	isvcs = append(isvcs, genIsvc)

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(isvcs...).WithObjects(srs...).Build()

	reconciler := &GuardrailsOrchestratorReconciler{
		Client: fakeClient,
		Scheme: s,
	}

	orchestrator := &gorchv1alpha1.GuardrailsOrchestrator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orchestratorName,
			Namespace: ns,
		},
	}

	cm, err := reconciler.GenerateOrchestratorConfigMap(ctx, orchestratorName, ns, orchestrator, "my-generation-service", true)
	assert.NoError(t, err)
	assert.NotNil(t, cm)
	// Check that the configmap contains expected data
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)
}
