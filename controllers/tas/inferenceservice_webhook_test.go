package tas

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestInferenceServiceLoggerWebhook(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kservev1beta1.AddToScheme(scheme))
	require.NoError(t, trustyaiopendatahubiov1.AddToScheme(scheme))

	service := &trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "trustyai-service", Namespace: "models"},
	}

	tests := []struct {
		name              string
		objects           []client.Object
		deploymentMode    string
		existingLoggerURL string
		wantPatch         bool
		wantLoggerURL     string
	}{
		{
			name:           "injects logger for raw deployment with a same namespace service",
			objects:        []client.Object{service},
			deploymentMode: DEPLOYMENT_MODE_RAW,
			wantPatch:      true,
			wantLoggerURL:  "https://trustyai-service.models.svc.cluster.local",
		},
		{
			name:           "injects logger for standard deployment with a same namespace service",
			objects:        []client.Object{service},
			deploymentMode: DEPLOYMENT_MODE_STANDARD,
			wantPatch:      true,
			wantLoggerURL:  "https://trustyai-service.models.svc.cluster.local",
		},
		{
			name:           "does not inject without a same namespace service",
			deploymentMode: DEPLOYMENT_MODE_RAW,
		},
		{
			name: "does not inject with a service in another namespace",
			objects: []client.Object{
				&trustyaiopendatahubiov1.TrustyAIService{
					ObjectMeta: metav1.ObjectMeta{Name: "trustyai-service", Namespace: "other-models"},
				},
			},
			deploymentMode: DEPLOYMENT_MODE_RAW,
		},
		{
			name: "does not inject when multiple same namespace services exist",
			objects: []client.Object{
				service,
				&trustyaiopendatahubiov1.TrustyAIService{ObjectMeta: metav1.ObjectMeta{Name: "second-trustyai-service", Namespace: "models"}},
			},
			deploymentMode: DEPLOYMENT_MODE_RAW,
		},
		{
			name:              "preserves an existing logger",
			objects:           []client.Object{service},
			deploymentMode:    DEPLOYMENT_MODE_RAW,
			existingLoggerURL: "https://customer-logger.models.svc",
		},
		{
			name:           "does not inject unsupported deployment mode",
			objects:        []client.Object{service},
			deploymentMode: DEPLOYMENT_MODE_SERVERLESS,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inferenceService := testInferenceService(test.deploymentMode, test.existingLoggerURL)
			rawObject, err := json.Marshal(inferenceService)
			require.NoError(t, err)

			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(test.objects...).Build()
			handler := NewInferenceServiceLoggerWebhook(reader, scheme)
			response := handler.Handle(context.Background(), admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					UID:       types.UID("test-uid"),
					Operation: admissionv1.Create,
					Namespace: "models",
					Object:    runtime.RawExtension{Raw: rawObject},
				},
			})

			require.True(t, response.Allowed)
			if !test.wantPatch {
				assert.Empty(t, response.Patch)
				return
			}

			patches := decodeJSONPatch(t, response.Patch)
			require.Len(t, patches, 1)
			patch := patches[0]
			assert.Equal(t, "add", patch.Operation)
			assert.Equal(t, "/spec/predictor/logger", patch.Path)
			logger := patch.Value
			assert.Equal(t, "all", logger["mode"])
			assert.Equal(t, test.wantLoggerURL, logger["url"])
		})
	}
}

func TestInferenceServiceLoggerWebhookAllowsNonCreateRequests(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kservev1beta1.AddToScheme(scheme))
	require.NoError(t, trustyaiopendatahubiov1.AddToScheme(scheme))

	inferenceService, err := json.Marshal(testInferenceService(DEPLOYMENT_MODE_RAW, ""))
	require.NoError(t, err)

	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "trustyai-service", Namespace: "models"},
	}).Build()
	handler := NewInferenceServiceLoggerWebhook(reader, scheme)
	response := handler.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Namespace: "models",
			Object:    runtime.RawExtension{Raw: inferenceService},
		},
	})

	assert.True(t, response.Allowed)
	assert.Empty(t, response.Patch)
}

func TestInferenceServiceLoggerWebhookFailsOpenWhenTrustyAIServiceListFails(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kservev1beta1.AddToScheme(scheme))
	require.NoError(t, trustyaiopendatahubiov1.AddToScheme(scheme))

	inferenceService, err := json.Marshal(testInferenceService(DEPLOYMENT_MODE_RAW, ""))
	require.NoError(t, err)

	handler := NewInferenceServiceLoggerWebhook(errorReader{}, scheme)
	response := handler.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Namespace: "models",
			Object:    runtime.RawExtension{Raw: inferenceService},
		},
	})

	assert.True(t, response.Allowed)
	assert.Empty(t, response.Patch)
}

func TestInferenceServiceLoggerWebhookFailsOpenWhenInferenceServiceCannotBeDecoded(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kservev1beta1.AddToScheme(scheme))
	require.NoError(t, trustyaiopendatahubiov1.AddToScheme(scheme))

	handler := NewInferenceServiceLoggerWebhook(fake.NewClientBuilder().WithScheme(scheme).Build(), scheme)
	response := handler.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Namespace: "models",
			Object:    runtime.RawExtension{Raw: []byte(`{"apiVersion":"serving.kserve.io/v1beta1"`)},
		},
	})

	assert.True(t, response.Allowed)
	assert.Empty(t, response.Patch)
}

func TestInferenceServiceLoggerWebhookSkipsRequestWithoutNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kservev1beta1.AddToScheme(scheme))
	require.NoError(t, trustyaiopendatahubiov1.AddToScheme(scheme))

	inferenceService := testInferenceService(DEPLOYMENT_MODE_RAW, "")
	inferenceService.Namespace = ""
	rawObject, err := json.Marshal(inferenceService)
	require.NoError(t, err)

	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "trustyai-service", Namespace: "models"},
	}).Build()
	handler := NewInferenceServiceLoggerWebhook(reader, scheme)
	response := handler.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: rawObject},
		},
	})

	assert.True(t, response.Allowed)
	assert.Empty(t, response.Patch)
}

func TestInferenceServiceLoggerWebhookSkipsDeletingTrustyAIService(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kservev1beta1.AddToScheme(scheme))
	require.NoError(t, trustyaiopendatahubiov1.AddToScheme(scheme))

	inferenceService, err := json.Marshal(testInferenceService(DEPLOYMENT_MODE_RAW, ""))
	require.NoError(t, err)
	deletingService := &trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "trustyai-service",
			Namespace:         "models",
			DeletionTimestamp: &metav1.Time{Time: time.Unix(1, 0)},
			Finalizers:        []string{"trustyai.opendatahub.io/finalizer"},
		},
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deletingService).Build()
	handler := NewInferenceServiceLoggerWebhook(reader, scheme)
	response := handler.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Namespace: "models",
			Object:    runtime.RawExtension{Raw: inferenceService},
		},
	})

	assert.True(t, response.Allowed)
	assert.Empty(t, response.Patch)
}

func TestInferenceServiceLoggerWebhookSkipsNamespaceMismatch(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kservev1beta1.AddToScheme(scheme))
	require.NoError(t, trustyaiopendatahubiov1.AddToScheme(scheme))

	inferenceService := testInferenceService(DEPLOYMENT_MODE_RAW, "")
	inferenceService.Namespace = "other-models"
	rawObject, err := json.Marshal(inferenceService)
	require.NoError(t, err)
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "trustyai-service", Namespace: "models"},
	}).Build()
	handler := NewInferenceServiceLoggerWebhook(reader, scheme)
	response := handler.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Namespace: "models",
			Object:    runtime.RawExtension{Raw: rawObject},
		},
	})

	assert.True(t, response.Allowed)
	assert.Empty(t, response.Patch)
}

func TestInferenceServiceLoggerWebhookReturnsOnlyLoggerPatch(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kservev1beta1.AddToScheme(scheme))
	require.NoError(t, trustyaiopendatahubiov1.AddToScheme(scheme))

	object := map[string]interface{}{
		"apiVersion": "serving.kserve.io/v1beta1",
		"kind":       "InferenceService",
		"metadata": map[string]interface{}{
			"name":      "model",
			"namespace": "models",
			"annotations": map[string]string{
				"serving.kserve.io/deploymentMode": DEPLOYMENT_MODE_RAW,
			},
		},
		"spec": map[string]interface{}{
			"predictor": map[string]interface{}{
				"model": map[string]interface{}{
					"modelFormat": map[string]string{"name": "sklearn"},
				},
				"futureField": map[string]string{"preserve": "me"},
			},
		},
	}
	rawObject, err := json.Marshal(object)
	require.NoError(t, err)
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "trustyai-service", Namespace: "models"},
	}).Build()
	handler := NewInferenceServiceLoggerWebhook(reader, scheme)
	response := handler.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Namespace: "models",
			Object:    runtime.RawExtension{Raw: rawObject},
		},
	})

	assert.True(t, response.Allowed)
	patches := decodeJSONPatch(t, response.Patch)
	require.Len(t, patches, 1)
	assert.Equal(t, "/spec/predictor/logger", patches[0].Path)
}

type jsonPatch struct {
	Operation string                 `json:"op"`
	Path      string                 `json:"path"`
	Value     map[string]interface{} `json:"value"`
}

func decodeJSONPatch(t *testing.T, raw []byte) []jsonPatch {
	t.Helper()
	var patches []jsonPatch
	require.NoError(t, json.Unmarshal(raw, &patches))
	return patches
}

type errorReader struct {
	client.Reader
}

func (errorReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("list failed")
}

func testInferenceService(deploymentMode, loggerURL string) *kservev1beta1.InferenceService {
	inferenceService := &kservev1beta1.InferenceService{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "serving.kserve.io/v1beta1",
			Kind:       "InferenceService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "model",
			Namespace: "models",
			Annotations: map[string]string{
				"serving.kserve.io/deploymentMode": deploymentMode,
			},
		},
		Spec: kservev1beta1.InferenceServiceSpec{
			Predictor: kservev1beta1.PredictorSpec{
				Model: &kservev1beta1.ModelSpec{
					ModelFormat: kservev1beta1.ModelFormat{Name: "sklearn"},
				},
			},
		},
	}
	if loggerURL != "" {
		inferenceService.Spec.Predictor.Logger = &kservev1beta1.LoggerSpec{
			Mode: kservev1beta1.LogAll,
			URL:  &loggerURL,
		}
	}
	return inferenceService
}
