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

// eventServingObjectRejected reports a served object that refused the
// payload-capture update. It is a warning rather than a reconcile error because
// a validation or authorization rejection repeats on every pass: it is a state
// for that object, not a transient failure of the operator.
func (r *TrustyAIServiceReconciler) eventServingObjectRejected(instance *trustyaiopendatahubiov1.TrustyAIService, rejected *servingObjectRejectedError) {
	r.EventRecorder.Event(instance, corev1.EventTypeWarning, EventReasonInferenceServiceRejected, rejected.Error())
}

func (r *TrustyAIServiceReconciler) eventPVCCreated(instance *trustyaiopendatahubiov1.TrustyAIService) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonPVCCreated, "PVC created")
}

func (r *TrustyAIServiceReconciler) eventDeploymentCreated(instance *trustyaiopendatahubiov1.TrustyAIService, message string) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonDeploymentCreated, message)
}

func (r *TrustyAIServiceReconciler) eventLocalServiceMonitorCreated(instance *trustyaiopendatahubiov1.TrustyAIService) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonServiceMonitorCreated, "Local ServiceMonitor created")
}

func (r *TrustyAIServiceReconciler) eventMetricsReaderSACreated(instance *trustyaiopendatahubiov1.TrustyAIService) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonMetricsReaderSACreated, "Metrics reader ServiceAccount created")
}

func (r *TrustyAIServiceReconciler) eventMetricsCABundleConfigMapCreated(instance *trustyaiopendatahubiov1.TrustyAIService) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonMetricsCABundleConfigMapCreated, "Metrics CA bundle ConfigMap created")
}

func (r *TrustyAIServiceReconciler) eventMetricsRoleCreated(instance *trustyaiopendatahubiov1.TrustyAIService) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonMetricsRoleCreated, "Metrics reader Role created")
}

func (r *TrustyAIServiceReconciler) eventMetricsRoleBindingCreated(instance *trustyaiopendatahubiov1.TrustyAIService) {
	r.EventRecorder.Event(instance, corev1.EventTypeNormal, EventReasonMetricsRoleBindingCreated, "Metrics reader RoleBinding created")
}
