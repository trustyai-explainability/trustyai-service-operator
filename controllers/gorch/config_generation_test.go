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

func setupTestObjects(ns string, labelFunc func(i int) string, tlsFunc func(i int) bool, generationTLS bool) ([]client.Object, []client.Object, []client.Object, []client.Object) {
	var isvcs []client.Object
	var srs []client.Object
	var svcs []client.Object
	var secrets []client.Object
	for i := 1; i <= 5; i++ {
		runtimeName := fmt.Sprintf("my-serving-runtime-%d", i)
		isvcName := fmt.Sprintf("my-inference-service-%d", i)
		label := labelFunc(i)

		var scheme string
		if tlsFunc(i) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      isvcName + "-predictor-serving-cert",
					Namespace: ns,
				},
			}
			secrets = append(secrets, secret)
			scheme = "https"
		} else {
			scheme = "http"
		}

		var url string
		var servicePort int
		var srPort int
		if tlsFunc(i) {
			servicePort = 8440 + i
			srPort = 8000 + i
			url = fmt.Sprintf("my-inference-service-%d.test-ns.svc.cluster.local:%d", i, servicePort)
		} else {
			servicePort = 80 + i
			srPort = 8000 + i
			url = fmt.Sprintf("my-inference-service-%d.test-ns.svc.cluster.local", i)
		}
		isvc := &kservev1beta1.InferenceService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      isvcName,
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
					Scheme: scheme,
					Host:   url,
				},
			},
		}

		// Add a Service for this InferenceService with the name isvc.Name + "-predictor"
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      isvcName + "-predictor",
				Namespace: ns,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name: scheme,
						Port: int32(servicePort),
					},
				},
				Selector: map[string]string{
					"app": isvcName,
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
									ContainerPort: int32(srPort), // increment port by i
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
		svcs = append(svcs, svc)
	}
	generationRuntimeName := "my-generation-runtime"
	generationISVCName := "my-generation-service"
	var generationISVCServicePort int
	generationSRPort := 8000

	var generationScheme string
	if generationTLS {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generationISVCName + "-predictor-serving-cert",
				Namespace: ns,
			},
		}
		generationISVCServicePort = 443
		secrets = append(secrets, secret)
		generationScheme = "https"
	} else {
		generationISVCServicePort = 80
		generationScheme = "http"
	}
	genSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generationISVCName + "-predictor",
			Namespace: ns,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: generationScheme,
					Port: int32(generationISVCServicePort),
				},
			},
			Selector: map[string]string{
				"app": generationISVCName,
			},
		},
	}

	genIsvc := &kservev1beta1.InferenceService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generationISVCName,
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
				Scheme: generationScheme,
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
								ContainerPort: int32(generationSRPort), // increment port by i
							},
						},
					},
				},
			},
		},
		Status: v1alpha1.ServingRuntimeStatus{},
	}
	isvcs = append(isvcs, genIsvc)
	srs = append(srs, genSr)
	svcs = append(svcs, genSvc)
	return isvcs, svcs, srs, secrets
}

func setupTestReconcilerAndOrchestrator(ns, orchestratorName string, autoConfig gorchv1alpha1.AutoConfig, isvcs, svcs []client.Object, srs []client.Object, secrets []client.Object, builtInDetectors bool, gateway bool) (*GuardrailsOrchestratorReconciler, *gorchv1alpha1.GuardrailsOrchestrator) {
	s := runtime.NewScheme()
	_ = kservev1beta1.AddToScheme(s)
	_ = gorchv1alpha1.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(isvcs...).WithObjects(svcs...).WithObjects(srs...).WithObjects(secrets...).Build()
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

	isvcs, svcs, srs, secrets := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" }, func(i int) bool { return false }, false)
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, svcs, srs, secrets, false, false)

	cm, _, _, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)
	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := `chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 80
detectors:
  my-inference-service-1:
    type: text_contents
    service:
      hostname: "my-inference-service-1.test-ns.svc.cluster.local"
      port: 81
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 82
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 83
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 84
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 85
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
passthrough_headers:
  - Authorization
  - Content-Type
`
	assert.Equal(t, expectedData, cm.Data["config.yaml"])
}

func TestGenerateOrchestratorConfigMapFullURL(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, svcs, srs, secrets := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" }, func(i int) bool { return false }, false)
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "http://my-generation-service:8123",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, svcs, srs, secrets, false, false)

	cm, _, _, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)
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
      port: 81
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 82
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 83
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 84
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 85
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
passthrough_headers:
  - Authorization
  - Content-Type
`
	assert.Equal(t, expectedData, cm.Data["config.yaml"])
}

func TestGenerateOrchestratorConfigMapDetectorLabels(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, srs, svcs, secrets := setupTestObjects(ns, func(i int) string {
		if i < 3 {
			return "trustyai/guardrails/groupA"
		}
		return "trustyai/guardrails/groupB"
	}, func(i int) bool { return false }, false)
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
		DetectorServiceLabelToMatch: "trustyai/guardrails/groupB",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, srs, svcs, secrets, false, false)

	cm, _, _, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)
	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := `chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 80
detectors:
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 83
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 84
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 85
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
passthrough_headers:
  - Authorization
  - Content-Type
`
	assert.Equal(t, expectedData, cm.Data["config.yaml"])
}

func TestGenerateOrchestratorConfigMapBuiltInDetectors(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, svcs, srs, secrets := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" }, func(i int) bool { return false }, false)
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, svcs, srs, secrets, true, false)

	cm, _, _, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)
	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := fmt.Sprintf(`chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 80
detectors:
  my-inference-service-1:
    type: text_contents
    service:
      hostname: "my-inference-service-1.test-ns.svc.cluster.local"
      port: 81
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 82
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 83
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 84
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 85
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  %s:
    type: text_contents
    service:
      hostname: 127.0.0.1
      port: 8080
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
passthrough_headers:
  - Authorization
  - Content-Type
`, builtInDetectorName)
	assert.Equal(t, expectedData, cm.Data["config.yaml"])
}

func TestGenerateOrchestratorConfigMapBuiltInDetectorsAndGateway(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, svcs, srs, secrets := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" }, func(i int) bool { return false }, false)
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, svcs, srs, secrets, true, true)

	cm, _, detectorServices, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)
	cmGateway, err := reconciler.defineGatewayConfigMap(orchestrator, detectorServices)

	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := fmt.Sprintf(`chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 80
detectors:
  my-inference-service-1:
    type: text_contents
    service:
      hostname: "my-inference-service-1.test-ns.svc.cluster.local"
      port: 81
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 82
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 83
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 84
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 85
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  %s:
    type: text_contents
    service:
      hostname: 127.0.0.1
      port: 8080
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
passthrough_headers:
  - Authorization
  - Content-Type
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

	deployment, err := templates.ParseResource[*appsv1.Deployment](templatePath, deploymentConfig, reflect.TypeOf(&appsv1.Deployment{}))
	assert.NoError(t, err)
	assert.NotNil(t, deployment)
	assert.Equal(t, "test-orchestrator", deployment.Name)
	assert.Equal(t, "test-ns", deployment.Namespace)
}

func TestHTTPSInferenceServiceTLSMounting(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, svcs, srs, secrets := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" }, func(i int) bool { return i%2 == 0 }, false)
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, svcs, srs, secrets, true, true)

	cm, _, detectorServices, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)

	if err != nil {
		fmt.Println(err, "Failed to define orchestrator configmap")

	}

	_, err = reconciler.defineGatewayConfigMap(orchestrator, detectorServices)

	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)
	expectedData := fmt.Sprintf(`chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 80
detectors:
  my-inference-service-1:
    type: text_contents
    service:
      hostname: "my-inference-service-1.test-ns.svc.cluster.local"
      port: 81
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 8442
      tls: my-inference-service-2-tls
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 83
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 8444
      tls: my-inference-service-4-tls
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 85
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  %s:
    type: text_contents
    service:
      hostname: 127.0.0.1
      port: 8080
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
tls:
  my-inference-service-2-tls:
    cert_path: "/etc/tls/my-inference-service-2-predictor-serving-cert/tls.crt"
    key_path: "/etc/tls/my-inference-service-2-predictor-serving-cert/tls.key"
    client_ca_cert_path: "/etc/tls/ca/service-ca.crt"
  my-inference-service-4-tls:
    cert_path: "/etc/tls/my-inference-service-4-predictor-serving-cert/tls.crt"
    key_path: "/etc/tls/my-inference-service-4-predictor-serving-cert/tls.key"
    client_ca_cert_path: "/etc/tls/ca/service-ca.crt"
passthrough_headers:
  - Authorization
  - Content-Type
`, builtInDetectorName)
	assert.Equal(t, expectedData, cm.Data["config.yaml"])
}

func TestHTTPSInferenceServiceTLSGenerationTLS(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, svcs, srs, secrets := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" }, func(i int) bool { return i%2 == 0 }, true)
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, svcs, srs, secrets, true, true)

	cm, _, detectorServices, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)

	if err != nil {
		fmt.Println(err, "Failed to define orchestrator configmap")

	}

	_, err = reconciler.defineGatewayConfigMap(orchestrator, detectorServices)

	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := fmt.Sprintf(`chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 443
    tls: my-generation-service-tls
detectors:
  my-inference-service-1:
    type: text_contents
    service:
      hostname: "my-inference-service-1.test-ns.svc.cluster.local"
      port: 81
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 8442
      tls: my-inference-service-2-tls
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 83
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 8444
      tls: my-inference-service-4-tls
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 85
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  %s:
    type: text_contents
    service:
      hostname: 127.0.0.1
      port: 8080
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
tls:
  my-generation-service-tls:
    cert_path: "/etc/tls/my-generation-service-predictor-serving-cert/tls.crt"
    key_path: "/etc/tls/my-generation-service-predictor-serving-cert/tls.key"
    client_ca_cert_path: "/etc/tls/ca/service-ca.crt"
  my-inference-service-2-tls:
    cert_path: "/etc/tls/my-inference-service-2-predictor-serving-cert/tls.crt"
    key_path: "/etc/tls/my-inference-service-2-predictor-serving-cert/tls.key"
    client_ca_cert_path: "/etc/tls/ca/service-ca.crt"
  my-inference-service-4-tls:
    cert_path: "/etc/tls/my-inference-service-4-predictor-serving-cert/tls.crt"
    key_path: "/etc/tls/my-inference-service-4-predictor-serving-cert/tls.key"
    client_ca_cert_path: "/etc/tls/ca/service-ca.crt"
passthrough_headers:
  - Authorization
  - Content-Type
`, builtInDetectorName)
	assert.Equal(t, expectedData, cm.Data["config.yaml"])

	// Simulate mounting the secret in a deployment
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "main"},
					},
				},
			},
		},
	}
	for _, secret := range secrets {
		MountSecret(deployment, secret.GetName())
		found := false
		for _, v := range deployment.Spec.Template.Spec.Volumes {
			if v.Name == secret.GetName()+"-vol" && v.VolumeSource.Secret != nil && v.VolumeSource.Secret.SecretName == secret.GetName() {
				found = true
				break
			}
		}
		assert.True(t, found, "TLS secret volume should be mounted for HTTPS inference service")
	}
}

func TestHTTPSInferenceServiceTLSGenerationTLSOauth(t *testing.T) {
	ctx := context.Background()
	ns := "test-ns"
	orchestratorName := "test-orch"

	isvcs, svcs, srs, secrets := setupTestObjects(ns, func(i int) string { return "trustyai/guardrails" }, func(i int) bool { return i%2 == 0 }, true)
	autoConfig := gorchv1alpha1.AutoConfig{
		InferenceServiceToGuardrail: "my-generation-service",
	}
	reconciler, orchestrator := setupTestReconcilerAndOrchestrator(ns, orchestratorName, autoConfig, isvcs, svcs, srs, secrets, true, true)
	orchestrator.Annotations = make(map[string]string)
	orchestrator.Annotations["security.opendatahub.io/enable-auth"] = "true"

	cm, _, detectorServices, _, err := reconciler.defineOrchestratorConfigMap(ctx, orchestrator)

	if err != nil {
		fmt.Println(err, "Failed to define orchestrator configmap")

	}

	_, err = reconciler.defineGatewayConfigMap(orchestrator, detectorServices)

	assert.NoError(t, err)
	assert.NotNil(t, cm)
	assert.Equal(t, orchestratorName+"-auto-config", cm.Name)
	assert.Equal(t, ns, cm.Namespace)

	expectedData := fmt.Sprintf(`chat_generation:
  service:
    hostname: my-generation-service.test-ns.svc.cluster.local
    port: 443
    tls: my-generation-service-tls
detectors:
  my-inference-service-1:
    type: text_contents
    service:
      hostname: "my-inference-service-1.test-ns.svc.cluster.local"
      port: 81
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-2:
    type: text_contents
    service:
      hostname: "my-inference-service-2.test-ns.svc.cluster.local"
      port: 8442
      tls: my-inference-service-2-tls
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-3:
    type: text_contents
    service:
      hostname: "my-inference-service-3.test-ns.svc.cluster.local"
      port: 83
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-4:
    type: text_contents
    service:
      hostname: "my-inference-service-4.test-ns.svc.cluster.local"
      port: 8444
      tls: my-inference-service-4-tls
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  my-inference-service-5:
    type: text_contents
    service:
      hostname: "my-inference-service-5.test-ns.svc.cluster.local"
      port: 85
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
  %s:
    type: text_contents
    service:
      hostname: 127.0.0.1
      port: 8080
    chunker_id: whole_doc_chunker
    default_threshold: 0.5
tls:
  my-generation-service-tls:
    cert_path: "/etc/tls/my-generation-service-predictor-serving-cert/tls.crt"
    key_path: "/etc/tls/my-generation-service-predictor-serving-cert/tls.key"
    client_ca_cert_path: "/etc/tls/ca/service-ca.crt"
  my-inference-service-2-tls:
    cert_path: "/etc/tls/my-inference-service-2-predictor-serving-cert/tls.crt"
    key_path: "/etc/tls/my-inference-service-2-predictor-serving-cert/tls.key"
    client_ca_cert_path: "/etc/tls/ca/service-ca.crt"
  my-inference-service-4-tls:
    cert_path: "/etc/tls/my-inference-service-4-predictor-serving-cert/tls.crt"
    key_path: "/etc/tls/my-inference-service-4-predictor-serving-cert/tls.key"
    client_ca_cert_path: "/etc/tls/ca/service-ca.crt"
passthrough_headers:
  - Authorization
  - Content-Type
  - x-forwarded-access-token
rewrite_forwarded_access_header: true
`, builtInDetectorName)
	assert.Equal(t, expectedData, cm.Data["config.yaml"])

	// Simulate mounting the secret in a deployment
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "main"},
					},
				},
			},
		},
	}
	for _, secret := range secrets {
		MountSecret(deployment, secret.GetName())
		found := false
		for _, v := range deployment.Spec.Template.Spec.Volumes {
			if v.Name == secret.GetName()+"-vol" && v.VolumeSource.Secret != nil && v.VolumeSource.Secret.SecretName == secret.GetName() {
				found = true
				break
			}
		}
		assert.True(t, found, "TLS secret volume should be mounted for HTTPS inference service")
	}
}
