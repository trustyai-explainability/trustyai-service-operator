package tas

import (
	"context"
	"encoding/json"
	"fmt"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// InferenceServiceLoggerWebhookPath is the admission endpoint used to inject a
// TrustyAI logger into a newly created KServe InferenceService.
const InferenceServiceLoggerWebhookPath = "/mutate-serving-kserve-io-v1beta1-inferenceservice"

type inferenceServiceLoggerWebhook struct {
	reader  client.Reader
	decoder admission.Decoder
}

// NewInferenceServiceLoggerWebhook creates the TAS logger-injection admission handler.
func NewInferenceServiceLoggerWebhook(reader client.Reader, scheme *runtime.Scheme) admission.Handler {
	return &inferenceServiceLoggerWebhook{
		reader:  reader,
		decoder: admission.NewDecoder(scheme),
	}
}

func (h *inferenceServiceLoggerWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation != admissionv1.Create {
		return admission.Allowed("logger injection applies to InferenceService CREATE requests only")
	}

	inferenceService := &kservev1beta1.InferenceService{}
	if err := h.decoder.Decode(req, inferenceService); err != nil {
		return h.allowOnError(ctx, "decode InferenceService", err)
	}

	if inferenceService.Spec.Predictor.Logger != nil {
		return admission.Allowed("InferenceService already specifies a logger")
	}

	annotations := inferenceService.GetAnnotations()
	deploymentMode := annotations["serving.kserve.io/deploymentMode"]
	if deploymentMode != DEPLOYMENT_MODE_RAW && deploymentMode != DEPLOYMENT_MODE_STANDARD {
		return admission.Allowed("InferenceService deployment mode is not supported for create-time logger injection")
	}

	namespace := inferenceService.Namespace
	if namespace == "" {
		namespace = req.Namespace
	}
	if namespace == "" {
		return admission.Allowed("create-time logger injection requires a namespaced InferenceService")
	}
	if req.Namespace != "" && inferenceService.Namespace != "" && req.Namespace != inferenceService.Namespace {
		return admission.Allowed("admission request and InferenceService namespaces do not match")
	}

	var services trustyaiopendatahubiov1.TrustyAIServiceList
	if err := h.reader.List(ctx, &services, client.InNamespace(namespace)); err != nil {
		return h.allowOnError(ctx, fmt.Sprintf("list TrustyAIService objects in %q", namespace), err)
	}
	activeServices := make([]trustyaiopendatahubiov1.TrustyAIService, 0, len(services.Items))
	for _, service := range services.Items {
		if service.DeletionTimestamp.IsZero() {
			activeServices = append(activeServices, service)
		}
	}
	if len(activeServices) != 1 {
		return admission.Allowed("create-time logger injection requires exactly one TrustyAIService in the namespace")
	}

	loggerURL := utils.GenerateHTTPSKServeLoggerURL(activeServices[0].Name, namespace)
	logger := kservev1beta1.LoggerSpec{
		Mode: kservev1beta1.LogAll,
		URL:  &loggerURL,
	}

	loggerJSON, err := json.Marshal(logger)
	if err != nil {
		return h.allowOnError(ctx, "marshal injected logger", err)
	}

	patch := fmt.Sprintf(`[{"op":"add","path":"/spec/predictor/logger","value":%s}]`, loggerJSON)
	patchType := admissionv1.PatchTypeJSONPatch
	return admission.Response{
		AdmissionResponse: admissionv1.AdmissionResponse{
			Allowed:   true,
			Patch:     []byte(patch),
			PatchType: &patchType,
		},
	}
}

func (h *inferenceServiceLoggerWebhook) allowOnError(ctx context.Context, message string, err error) admission.Response {
	log.FromContext(ctx).Error(err, "Skipping create-time logger injection")
	return admission.Allowed(fmt.Sprintf("%s: %v", message, err))
}
