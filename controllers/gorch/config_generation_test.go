package gorch

import (
	"context"
	"fmt"
	"github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	"github.com/stretchr/testify/assert"
	gorchv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/gorch/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/gorch/templates"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

func setupTestObjects(ns string, labelFunc func(i int) string) ([]client.Object, []client.Object) {
	var isvcs []client.Object
	var srs []client.Object
	for i := 1; i <= 5; i++ {
		runtimeName := fmt.Sprintf("my-serving-runtime-%d", i)
		label := labelFunc(i)
		isvc := &kservev1beta1.InferenceService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("my-inference-service-%d", i),
				Namespace: ns,
				Labels:    map[string]string{label: "true"},
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
	generationRuntimeName := "my-generation-runtime"
	genIsvc := &kservev1beta1.InferenceService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-generation-service",
			Namespace: ns,
		},
		Spec: kservev1beta1.InferenceServiceSpec{
			Predictor: kservev1beta1.PredictorSpec{
				Model: &kservev1beta1.ModelSpec{
					Runtime: &generationRuntimeName,
				},
			},
		},
		Status: kservev1beta1.InferenceServiceStatus{
			URL: &apis.URL{
				Scheme: "https",
				Host:   "my-generation-service.test-ns.svc.cluster.local:8080",
			},
		},
	}
	genSr := &v1alpha1.ServingRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generationRuntimeName,
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
								ContainerPort: int32(7000), // increment port by i
							},
						},
					},
				},
			},
		},
		Status: v1alpha1.ServingRuntimeStatus{},
	}
	isvcs = append(isvcs, genIsvc, genSr)
	return isvcs, srs
}

func setupTestReconcilerAndOrchestrator(ns, orchestratorName string, autoConfig gorchv1alpha1.AutoConfig, isvcs, srs []client.Object, builtInDetectors bool, gateway bool) (*GuardrailsOrchestratorReconciler, *gorchv1alpha1.GuardrailsOrchestrator) {
	s := runtime.NewScheme()
	_ = kservev1beta1.AddToScheme(s)
	_ = gorchv1alpha1.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
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
		Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
			AutoConfig:              &autoConfig,
			EnableBuiltInDetectors:  builtInDetectors,
			EnableGuardrailsGateway: gateway,
		},
	}
	return reconciler, orchestrator
}

func TestGenerateOrchestratorConfigMap(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, srs := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" })
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, srs, false, false)

	cm, _, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)
	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := `chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 7000
detectors:
  my-inference-service-1:
    type: text_contents
    service:
      hostname: "my-inference-service-1.test-ns.svc.cluster.local"
      port: 8001
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 8002
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 8003
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 8004
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 8005
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
`
	assert.Equal(t, expectedData, cm.Data["config.yaml"])
}

func TestGenerateOrchestratorConfigMapFullURL(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, srs := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" })
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "http://my-generation-service:8123",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, srs, false, false)

	cm, _, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)
	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := `chat_generation:
  service:
    hostname: my-generation-service
    port: 8123
detectors:
  my-inference-service-1:
    type: text_contents
    service:
      hostname: "my-inference-service-1.test-ns.svc.cluster.local"
      port: 8001
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 8002
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 8003
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 8004
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 8005
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
`
	assert.Equal(t, expectedData, cm.Data["config.yaml"])
}

func TestGenerateOrchestratorConfigMapDetectorLabels(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, srs := setupTestObjects(ns, func(i int) string {
		if i < 3 {
			return "trustyai/guardrails/groupA"
		}
		return "trustyai/guardrails/groupB"
	})
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
		DetectorServiceLabelToMatch: "trustyai/guardrails/groupB",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, srs, false, false)

	cm, _, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)
	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := `chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 7000
detectors:
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 8003
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 8004
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 8005
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
`
	assert.Equal(t, expectedData, cm.Data["config.yaml"])
}

func TestGenerateOrchestratorConfigMapBuiltInDetectors(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, srs := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" })
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, srs, true, false)

	cm, _, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)
	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := fmt.Sprintf(`chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 7000
detectors:
  my-inference-service-1:
    type: text_contents
    service:
      hostname: "my-inference-service-1.test-ns.svc.cluster.local"
      port: 8001
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 8002
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 8003
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 8004
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 8005
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  %s:
    type: text_contents
    service:
      hostname: 127.0.0.1
      port: 8080
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
`, builtInDetectorName)
	assert.Equal(t, expectedData, cm.Data["config.yaml"])
}

func TestGenerateOrchestratorConfigMapBuiltInDetectorsAndGateway(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, srs := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" })
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, srs, true, true)

	cm, _, detectorServices, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)
	cmGateway, err := reconciler.defineGatewayConfigMap(orchestrator, detectorServices)

	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := fmt.Sprintf(`chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 7000
detectors:
  my-inference-service-1:
    type: text_contents
    service:
      hostname: "my-inference-service-1.test-ns.svc.cluster.local"
      port: 8001
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 8002
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 8003
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 8004
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 8005
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  %s:
    type: text_contents
    service:
      hostname: 127.0.0.1
      port: 8080
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
`, builtInDetectorName)
	expectedGatewayData := fmt.Sprintf(`orchestrator:
  host: "localhost"
  port: 8032
detectors:
  - name: my-inference-service-1
    input: true
    output: true
    detector_params: {}
  - name: my-inference-service-2
    input: true
    output: true
    detector_params: {}
  - name: my-inference-service-3
    input: true
    output: true
    detector_params: {}
  - name: my-inference-service-4
    input: true
    output: true
    detector_params: {}
  - name: my-inference-service-5
    input: true
    output: true
    detector_params: {}
  - name: %s
    input: true
    output: true
    detector_params:
      regex:
        - $^
routes:
  - name: all
    detectors:
      - my-inference-service-1
      - my-inference-service-2
      - my-inference-service-3
      - my-inference-service-4
      - my-inference-service-5
      - %s
  - name: passthrough
    detectors:
`, builtInDetectorName, builtInDetectorName)

	assert.Equal(t, expectedData, cm.Data["config.yaml"])
	assert.Equal(t, expectedGatewayData, cmGateway.Data["config.yaml"])
}

func TestDeploymentTemplateRenders(t *testing.T) {
	// Setup a minimal orchestrator and images
	orchestrator := &gorchv1alpha1.GuardrailsOrchestrator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-orchestrator",
			Namespace: "test-ns",
		},
		Spec: gorchv1alpha1.GuardrailsOrchestratorSpec{
			Replicas: 1,
		},
	}
	containerImages := ContainerImages{
		OrchestratorImage:      "orchestrator:latest",
		DetectorImage:          "detector:latest",
		GuardrailsGatewayImage: "gateway:latest",
	}
	deploymentConfig := DeploymentConfig{
		Orchestrator:    orchestrator,
		ContainerImages: containerImages,
	}

	// Use the actual template path
	templatePath := "deployment.tmpl.yaml"

	deployment, err := templates.ParseResource[appsv1.Deployment](templatePath, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	assert.NoError(t, err)
	assert.NotNil(t, deployment)
	assert.Equal(t, "test-orchestrator", deployment.Name)
	assert.Equal(t, "test-ns", deployment.Namespace)
}
