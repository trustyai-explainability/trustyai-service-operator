package tas

import (
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	corev1 "k8s.io/api/core/v1"
)

func (r *TrustyAIServiceReconciler) eventModelMeshConfigured(instance *trustyaiopendatahubiov1.TrustyAIService) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonInferenceServiceConfigured, "ModelMesh InferenceService configured")
}

func (r *TrustyAIServiceReconciler) eventKServeConfigured(instance *trustyaiopendatahubiov1.TrustyAIService) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonInferenceServiceConfigured, "KServe InferenceService configured")
}

func (r *TrustyAIServiceReconciler) eventPVCCreated(instance *trustyaiopendatahubiov1.TrustyAIService) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonPVCCreated, "PVC created")
}

func (r *TrustyAIServiceReconciler) eventLocalServiceMonitorCreated(instance *trustyaiopendatahubiov1.TrustyAIService) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonServiceMonitorCreated, "Local ServiceMonitor created")
}
