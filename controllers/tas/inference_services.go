package tas

import (
	"context"
	goerrors "errors"
	"fmt"
	"strings"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DEPLOYMENT_MODE_MODELMESH  = "ModelMesh"
	DEPLOYMENT_MODE_RAW        = "RawDeployment"
	DEPLOYMENT_MODE_STANDARD   = "Standard"
	DEPLOYMENT_MODE_SERVERLESS = "Serverless"
	DEPLOYMENT_MODE_KNATIVE    = "Knative"

	// The annotations that select a write path, the deployment-mode values and
	// the logger mode are declared in serving_fields.go with the rest of the
	// serving API surface.
)

// GetDeploymentsByLabel returns a list of Deployments that match a label key-value pair
func (r *TrustyAIServiceReconciler) GetDeploymentsByLabel(ctx context.Context, namespace string, labelKey string, labelValue string) ([]appsv1.Deployment, error) {
	// Prepare a DeploymentList object
	deployments := &appsv1.DeploymentList{}

	// Define the selector based on the provided label key-value pair
	selector := labels.Set{labelKey: labelValue}.AsSelector()

	// Fetch the Deployments that match the selector
	if err := r.List(ctx, deployments, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		log.FromContext(ctx).Error(err, "Could not list Deployments by label.")
		return nil, err
	}

	return deployments.Items, nil
}

func (r *TrustyAIServiceReconciler) patchEnvVarsForDeployments(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, deployments []appsv1.Deployment, envVarName string, url string, remove bool) (bool, error) {
	// Create volume and volume mount for this intance's TLS secrets
	certVolumes := TLSCertVolumes{}
	certVolumes.createFor(instance)

	// Loop over the Deployments
	for _, deployment := range deployments {

		// Check if all Pods are ready
		if deployment.Status.ReadyReplicas != *deployment.Spec.Replicas {
			// Not all replicas are ready, will retry later
			log.FromContext(ctx).Info("Not all replicas are ready for deployment " + deployment.Name + ". Waiting.")
			return false, nil
		}

		// Removal runs from the finalizer, as the instance's TLS Secret is about
		// to disappear with the instance. Publishing the serving-certificate
		// volume, mount and cert path here pointed ModelMesh Pods at a Secret that
		// no longer exists, so Pods created after the uninstall could not start.
		// Removal therefore only takes back the material this operator added.
		var changed bool
		if remove {
			sinkChanged := removePayloadProcessorEnvVar(&deployment.Spec.Template.Spec, envVarName, url)
			tlsChanged := removeTLSCertMaterial(&deployment.Spec.Template.Spec, instance.Name)
			changed = sinkChanged || tlsChanged
		} else {
			sinkChanged := addPayloadProcessorEnvVar(&deployment.Spec.Template.Spec, envVarName, url)
			tlsChanged := addTLSCertMaterial(&deployment.Spec.Template.Spec, certVolumes)
			changed = sinkChanged || tlsChanged
		}

		// One write per Deployment. The sink edit and the TLS material the same
		// pass adds used to be two updates, so a partially applied pass could
		// publish the mount without the env var that explains it.
		if !changed {
			continue
		}
		if err := r.Update(ctx, &deployment); err != nil {
			log.FromContext(ctx).Error(err, "Could not update Deployment", "Deployment", deployment.Name)
			return false, err
		}
		r.eventModelMeshConfigured(instance)
		log.FromContext(ctx).Info("Updated Deployment " + deployment.Name + ", env var " + envVarName + " to " + url)
	}

	return true, nil
}

// envVarIndex returns the position of an environment variable in a container, or
// -1 when the container does not carry it.
func envVarIndex(container *corev1.Container, name string) int {
	for i := range container.Env {
		if container.Env[i].Name == name {
			return i
		}
	}
	return -1
}

// addPayloadProcessorEnvVar adds this operator's sink to every container that
// does not carry it, and appends it to a container whose value already lists
// other sinks. The value is space separated, so a sink this operator does not
// own is never rewritten.
func addPayloadProcessorEnvVar(spec *corev1.PodSpec, envVarName, url string) bool {
	changed := false
	for i := range spec.Containers {
		container := &spec.Containers[i]
		index := envVarIndex(container, envVarName)
		if index < 0 {
			container.Env = append(container.Env, corev1.EnvVar{Name: envVarName, Value: url})
			changed = true
			continue
		}
		current := container.Env[index].Value
		updated := generateEnvVarValue(current, url, false)
		if updated == current {
			continue
		}
		container.Env[index].Value = updated
		changed = true
	}
	return changed
}

// removePayloadProcessorEnvVar takes this operator's sink back out. A container
// that never carried the variable has nothing to remove, which is the case the
// old nil dereference panicked on.
func removePayloadProcessorEnvVar(spec *corev1.PodSpec, envVarName, url string) bool {
	changed := false
	for i := range spec.Containers {
		container := &spec.Containers[i]
		index := envVarIndex(container, envVarName)
		if index < 0 {
			continue
		}
		current := container.Env[index].Value
		updated := generateEnvVarValue(current, url, true)
		if updated == current {
			continue
		}
		container.Env[index].Value = updated
		changed = true
	}
	return changed
}

// addTLSCertMaterial mounts the instance's serving Secret on a ModelMesh
// Deployment and points the ModelMesh certificate path at it.
func addTLSCertMaterial(spec *corev1.PodSpec, certVolumes TLSCertVolumes) bool {
	changed := false

	volumeExists := false
	for _, vol := range spec.Volumes {
		if vol.Name == certVolumes.volume.Name {
			volumeExists = true
			break
		}
	}
	if !volumeExists {
		spec.Volumes = append(spec.Volumes, certVolumes.volume)
		changed = true
	}

	for i := range spec.Containers {
		container := &spec.Containers[i]
		mountExists := false
		for _, mount := range container.VolumeMounts {
			if mount.Name == certVolumes.volumeMount.Name {
				mountExists = true
				break
			}
		}
		if !mountExists {
			container.VolumeMounts = append(container.VolumeMounts, certVolumes.volumeMount)
			changed = true
		}

		if container.Name != mmContainerName {
			continue
		}
		if envVarIndex(container, tlsKeyCertPathName) < 0 {
			container.Env = append(container.Env, corev1.EnvVar{Name: tlsKeyCertPathName, Value: tlsMountPath + "/tls.crt"})
			changed = true
		}
	}

	return changed
}

// removeTLSCertMaterial takes back the volume, mounts and certificate path that
// addTLSCertMaterial adds, so an uninstall does not leave ModelMesh mounting a
// Secret that is deleted with the instance. Every removal is matched on what
// this operator wrote (Secret name, mount path, certificate path), so a mount a
// user happened to name the same way is left alone.
func removeTLSCertMaterial(spec *corev1.PodSpec, instanceName string) bool {
	changed := false
	volumeName := instanceName + "-internal"

	volumes := make([]corev1.Volume, 0, len(spec.Volumes))
	for _, volume := range spec.Volumes {
		if volume.Name == volumeName && volume.Secret != nil && volume.Secret.SecretName == volumeName {
			changed = true
			continue
		}
		volumes = append(volumes, volume)
	}
	spec.Volumes = volumes

	for i := range spec.Containers {
		container := &spec.Containers[i]

		mounts := make([]corev1.VolumeMount, 0, len(container.VolumeMounts))
		for _, mount := range container.VolumeMounts {
			if mount.Name == volumeName && mount.MountPath == tlsMountPath {
				changed = true
				continue
			}
			mounts = append(mounts, mount)
		}
		container.VolumeMounts = mounts

		if container.Name != mmContainerName {
			continue
		}
		env := make([]corev1.EnvVar, 0, len(container.Env))
		for _, variable := range container.Env {
			if variable.Name == tlsKeyCertPathName && variable.Value == tlsMountPath+"/tls.crt" {
				changed = true
				continue
			}
			env = append(env, variable)
		}
		container.Env = env
	}

	return changed
}

func (r *TrustyAIServiceReconciler) patchEnvVarsByLabelForDeployments(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, namespace string, labelKey string, labelValue string, envVarName string, crName string, remove bool) (bool, error) {
	// Get all Deployments for the label
	deployments, err := r.GetDeploymentsByLabel(ctx, namespace, labelKey, labelValue)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not get Deployments by label.")
		return false, err
	}

	// Build the payload processor endpoint
	url := utils.GenerateTLSServiceURL(crName, namespace) + "/consumer/kserve/v2"

	// Patch environment variables for the Deployments
	if shouldContinue, err := r.patchEnvVarsForDeployments(ctx, instance, deployments, envVarName, url, remove); err != nil {
		log.FromContext(ctx).Error(err, "Could not patch environment variables for Deployments.")
		return shouldContinue, err
	}

	return true, nil
}

// generateEnvVarValue Generates the final value of the MM_PAYLOAD_PROCESSOR environment variable based on the remove flag and current value
func generateEnvVarValue(currentValue, newValue string, remove bool) string {
	// Split the current value into parts
	parts := strings.Split(currentValue, " ")

	// Check if the new value already exists in the list
	exists := false
	for _, part := range parts {
		if part == newValue {
			exists = true
			break
		}
	}

	// If we are removing, and the new value exists, remove it
	if remove && exists {
		newParts := []string{}
		for _, part := range parts {
			if part != newValue {
				newParts = append(newParts, part)
			}
		}
		return strings.Join(newParts, " ")
	}

	// If we are not removing, and the new value doesn't exist, add it
	if !remove && !exists {
		return currentValue + " " + newValue
	}

	return currentValue
}

func (r *TrustyAIServiceReconciler) handleInferenceServices(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, report inferenceServicesReport, namespace string, labelKey, labelValue, envVarName, crName string, remove bool) (bool, error) {
	// The report is derived once per pass by the caller and shared with the status
	// pass, so the writer and the condition cannot disagree inside one reconcile.
	if report.err != nil {
		// Serving is an optional dependency: a failed read must not stop the
		// required work. The error is carried rather than dropped, so the status
		// pass still returns it and the reconcile is retried.
		log.FromContext(ctx).Error(report.err, "Could not read the serving state, skipping InferenceService handling")
		return true, nil
	}

	contract := report.contract
	if contract.state != servingStateUsable {
		log.FromContext(ctx).Info("Serving API not usable, skipping InferenceService handling. Will retry on next reconcile.",
			"state", contract.state, "reason", contract.servingReason(), "message", contract.unusableMessage())
		return true, nil
	}

	inferenceServices, err := r.listServing(ctx, contract, namespace)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not list InferenceService objects.")
		return false, err
	}

	kServeServerlessEnabled, err := r.getKServeServerlessConfig(ctx)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not read KServeServerless configuration. Defaulting to disabled")
		kServeServerlessEnabled = false
	}

	for i := range inferenceServices.Items {
		infService := &inferenceServices.Items[i]
		deploymentMode := infService.GetAnnotations()[deploymentModeAnnotation]

		var patchErr error
		switch deploymentMode {
		case DEPLOYMENT_MODE_MODELMESH:
			// ModelMesh carries the sink in an environment variable on its own
			// Deployment rather than in a logger field, so this branch patches
			// Deployments and never touches the InferenceService.
			var shouldContinue bool
			shouldContinue, err = r.patchEnvVarsByLabelForDeployments(ctx, instance, namespace, labelKey, labelValue, envVarName, crName, remove)
			if err != nil {
				log.FromContext(ctx).Error(err, "Could not patch environment variables for ModelMesh deployments")
				return shouldContinue, err
			}
			continue

		case DEPLOYMENT_MODE_RAW, DEPLOYMENT_MODE_STANDARD:
			// KServe v0.17+ renamed RawDeployment to Standard.
			patchErr = r.patchServingLogger(ctx, instance, contract, infService, crName, remove, true)

		case DEPLOYMENT_MODE_SERVERLESS, DEPLOYMENT_MODE_KNATIVE:
			// KServe v0.17+ renamed Serverless to Knative, and payload capture is
			// only configured for these modes when serverless is enabled here.
			if kServeServerlessEnabled {
				patchErr = r.patchServingLogger(ctx, instance, contract, infService, crName, remove, false)
			}

		default:
			log.FromContext(ctx).Info("Unknown deployment mode, skipping InferenceService",
				"deploymentMode", deploymentMode, "inferenceService", infService.GetName())
			continue
		}

		if patchErr == nil {
			continue
		}

		var rejected *servingObjectRejectedError
		if goerrors.As(patchErr, &rejected) {
			log.FromContext(ctx).Error(patchErr, "Serving object rejected the payload-capture update; reporting it as state")
			r.eventServingObjectRejected(instance, rejected)
			continue
		}
		log.FromContext(ctx).Error(patchErr, "Could not configure payload capture on InferenceService", "inferenceService", infService.GetName())
		return false, patchErr
	}
	return true, nil
}

// servingLoggerURL is the payload-processor endpoint written into
// spec.predictor.logger: the HTTPS URL for Raw/Standard deployments, whose
// predictor sidecar can present the service certificate, and the plain URL for
// Serverless/Knative, where it does not.
func servingLoggerURL(crName, namespace string, useHTTPS bool) string {
	if useHTTPS {
		return utils.GenerateHTTPSKServeLoggerURL(crName, namespace)
	}
	return utils.GenerateKServeLoggerURL(crName, namespace)
}

// servingLoggerURLOf returns the URL an object's logger points at, or "" when it
// carries no logger or an unreadable one.
func servingLoggerURLOf(logger map[string]interface{}) string {
	url, _, err := unstructured.NestedString(logger, "url")
	if err != nil {
		return ""
	}
	return url
}

// servingObjectRejectedError reports a per-object rejection. A validation or
// authorization rejection is a state for that object, not a transient error:
// retrying every 30 seconds cannot fix it, so it is reported and the remaining
// objects are still handled.
type servingObjectRejectedError struct {
	namespace string
	name      string
	err       error
}

func (e *servingObjectRejectedError) Error() string {
	return fmt.Sprintf("the serving object %s/%s rejected the payload-capture update: %v", e.namespace, e.name, e.err)
}

func (e *servingObjectRejectedError) Unwrap() error { return e.err }

// isServingObjectRejection distinguishes a rejection the API server will repeat
// from an error that may clear on its own.
func isServingObjectRejection(err error) bool {
	return errors.IsInvalid(err) || errors.IsForbidden(err)
}

// servingResource returns the namespaced handle for the adopted serving version
// on the dynamic client. The resource comes from the CRD, so nothing here needs a
// scheme entry, an informer, or a RESTMapper lookup.
func (r *TrustyAIServiceReconciler) servingResource(contract servingContract, namespace string) (dynamic.ResourceInterface, error) {
	if r.Dynamic == nil {
		return nil, fmt.Errorf("no dynamic client is configured, cannot reach serving resources")
	}
	return r.Dynamic.Resource(contract.gvr).Namespace(namespace), nil
}

// listServing reads the InferenceServices of the adopted version in one
// namespace. It is a plain read on the dynamic client: no cached List, because
// serving is not watched and a cached List would build a cluster-wide informer
// on first use and answer empty while it syncs.
func (r *TrustyAIServiceReconciler) listServing(ctx context.Context, contract servingContract, namespace string) (*unstructured.UnstructuredList, error) {
	resource, err := r.servingResource(contract, namespace)
	if err != nil {
		return nil, err
	}
	return resource.List(ctx, metav1.ListOptions{})
}

// patchServingLogger makes one InferenceService's payload-capture configuration
// match the desired state, and ensures the service-mesh objects it asks for.
//
// Three properties are load-bearing. It is idempotent: when the logger already
// points where it should, or is already absent on removal, it writes nothing,
// without which every reconcile would write every InferenceService every 30
// seconds. It is a read-modify-write with conflict retry, because
// odh-model-controller and the serving controllers write the same objects. And
// it clears the logger rather than setting an empty struct, so removal leaves the
// field absent instead of present and empty.
func (r *TrustyAIServiceReconciler) patchServingLogger(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, contract servingContract, infService *unstructured.Unstructured, crName string, remove bool, useHTTPS bool) error {
	namespace := infService.GetNamespace()
	name := infService.GetName()
	url := servingLoggerURL(crName, namespace, useHTTPS)

	resource, err := r.servingResource(contract, namespace)
	if err != nil {
		return err
	}

	current, err := resource.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if isServingObjectRejection(err) {
			return &servingObjectRejectedError{namespace: namespace, name: name, err: err}
		}
		return fmt.Errorf("reading InferenceService %s/%s: %w", namespace, name, err)
	}
	if !servingLoggerNeedsUpdate(current, url, remove) {
		return nil
	}

	// The service-mesh objects are ensured before the write, so a failure there
	// leaves the InferenceService untouched rather than half configured.
	if err := r.ensureServingMeshObjects(ctx, instance, current); err != nil {
		return err
	}

	configured := false
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := resource.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		// Another writer may have reached the desired state while this pass was
		// retrying; re-deciding keeps the write idempotent under contention.
		if !servingLoggerNeedsUpdate(current, url, remove) {
			configured = false
			return nil
		}

		updated := current.DeepCopy()
		if remove {
			// Only the logger this operator wrote is removed: an object pointing at
			// another sink is not ours to reconfigure. The three annotations are
			// deleted whenever they are present, because they record our sink and
			// are stale as soon as payload capture is off, whoever cleared it.
			if logger, found, loggerErr := unstructured.NestedMap(updated.Object, predictorLoggerFieldPathParts...); loggerErr == nil && found && servingLoggerIsOurs(logger, url) {
				unstructured.RemoveNestedField(updated.Object, predictorLoggerFieldPathParts...)
			}
			annotations := updated.GetAnnotations()
			for _, key := range servingLoggerAnnotationKeys {
				delete(annotations, key)
			}
			if len(annotations) == 0 {
				annotations = nil
			}
			updated.SetAnnotations(annotations)
		} else if err := unstructured.SetNestedMap(updated.Object, map[string]interface{}{
			"mode": loggerModeAll,
			"url":  url,
		}, predictorLoggerFieldPathParts...); err != nil {
			return err
		}

		if _, err := resource.Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
			return err
		}
		configured = true
		return nil
	})
	if err != nil {
		if isServingObjectRejection(err) {
			return &servingObjectRejectedError{namespace: namespace, name: name, err: err}
		}
		return fmt.Errorf("updating InferenceService %s/%s: %w", namespace, name, err)
	}

	if configured {
		r.eventKServeConfigured(instance)
	}
	return nil
}

// servingLoggerIsOurs reports whether the logger points at the sink this
// operator writes, and so is the operator's to remove. A logger with no sink at
// all points nowhere else and is cleared too.
func servingLoggerIsOurs(logger map[string]interface{}, url string) bool {
	if logger == nil {
		return false
	}
	if len(logger) == 0 {
		return true
	}
	return servingLoggerURLOf(logger) == url
}

// servingLoggerIsDesired reports whether the logger already carries the whole
// desired state. The mode is compared as well as the sink: comparing only the
// URL let an object with the right URL and a different mode persist, so the
// operator reported the state it wrote while the object held another.
func servingLoggerIsDesired(logger map[string]interface{}, url string) bool {
	if logger == nil {
		return false
	}
	mode, _, err := unstructured.NestedString(logger, "mode")
	if err != nil || mode != loggerModeAll {
		return false
	}
	return servingLoggerURLOf(logger) == url
}

// servingHasLoggerAnnotations reports whether the annotations this operator's
// writes leave behind are still present.
func servingHasLoggerAnnotations(infService *unstructured.Unstructured) bool {
	annotations := infService.GetAnnotations()
	for _, key := range servingLoggerAnnotationKeys {
		if _, present := annotations[key]; present {
			return true
		}
	}
	return false
}

// servingLoggerNeedsUpdate reports whether the object differs from the desired
// state. Absent-on-removal and already-desired-on-set are both "nothing to do".
func servingLoggerNeedsUpdate(infService *unstructured.Unstructured, url string, remove bool) bool {
	logger, _, err := unstructured.NestedMap(infService.Object, predictorLoggerFieldPathParts...)
	if err != nil {
		// An unreadable logger is not something this operator wrote; overwrite it
		// rather than leaving payload capture silently off.
		return true
	}
	if remove {
		// Removal has two independent pieces of state to clear: the logger, when it
		// is ours, and the annotations, whenever they are present. Gating both on
		// the URL left the annotations behind whenever the logger had already been
		// cleared or pointed at another sink.
		return servingLoggerIsOurs(logger, url) || servingHasLoggerAnnotations(infService)
	}
	return !servingLoggerIsDesired(logger, url)
}

// ensureServingMeshObjects keeps the original branch: only an object that asks
// for the Istio sidecar gets the DestinationRule and VirtualService, each guarded
// by its own CRD. A missing CRD is logged and does not fail the reconcile,
// because the logger write is still correct without them.
func (r *TrustyAIServiceReconciler) ensureServingMeshObjects(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService, infService *unstructured.Unstructured) error {
	if inject := infService.GetAnnotations()[istioInjectAnnotation]; inject != "true" {
		return nil
	}

	destinationRulePresent, err := r.isDestinationRuleCRDPresent(ctx)
	if err != nil {
		return fmt.Errorf("verifying the DestinationRule CRD is present: %w", err)
	}
	if destinationRulePresent {
		if err := r.ensureDestinationRule(ctx, instance); err != nil {
			return fmt.Errorf("failed to ensure DestinationRule: %w", err)
		}
	} else {
		log.FromContext(ctx).Error(fmt.Errorf("the DestinationRule CRD is not present in this cluster"),
			"InferenceService has service mesh annotation but DestinationRule CRD not found", "inferenceService", infService.GetName())
	}

	virtualServicePresent, err := r.isVirtualServiceCRDPresent(ctx)
	if err != nil {
		return fmt.Errorf("verifying the VirtualService CRD is present: %w", err)
	}
	if virtualServicePresent {
		if err := r.ensureVirtualService(ctx, instance); err != nil {
			return fmt.Errorf("failed to ensure VirtualService: %w", err)
		}
	} else {
		log.FromContext(ctx).Error(fmt.Errorf("the VirtualService CRD is not present in this cluster"),
			"InferenceService has service mesh annotation but VirtualService CRD not found", "inferenceService", infService.GetName())
	}

	return nil
}

// inferenceServicesReport is the serving state as reported through the
// InferenceServicesPresent condition, with the reason that distinguishes the
// three ways serving can be unusable.
type inferenceServicesReport struct {
	contract  servingContract
	namespace string
	// present is true only when the serving API is usable and the namespace
	// holds at least one InferenceService.
	present bool
	// err is a transient failure to read the serving state on this pass. It is
	// carried rather than returned so the status of the required components is
	// still written: serving is informational, and a CRD in Terminating or an API
	// blip must not freeze Ready at its previous value.
	err error
}

// reason is the condition reason for this state. Serving usable but idle has its
// own reason, so "no InferenceService yet" is distinguishable from "no serving".
func (rep inferenceServicesReport) reason() string {
	if rep.contract.state != servingStateUsable {
		return rep.contract.servingReason()
	}
	if rep.present {
		return StatusReasonInferenceServicesFound
	}
	return StatusReasonNoInferenceServices
}

// message names the group and version that was adopted whenever one was, which
// is what makes version adoption observable from outside the process.
func (rep inferenceServicesReport) message() string {
	if rep.contract.state != servingStateUsable {
		return rep.contract.unusableMessage()
	}
	if rep.present {
		return fmt.Sprintf("InferenceServices found (serving API %s)", rep.contract.groupVersion())
	}
	return fmt.Sprintf("no InferenceService in namespace %s (serving API %s); payload capture activates when one is created",
		rep.namespace, rep.contract.groupVersion())
}

// inferenceServicesReport resolves the serving contract and, when serving is
// usable, whether the namespace holds an InferenceService.
//
// A CRD read failure or a list failure while the CRD is present is transient and
// carried in the report for the caller to requeue on; both are distinct from an
// absent API, which is a state.
func (r *TrustyAIServiceReconciler) inferenceServicesReport(ctx context.Context, namespace string) inferenceServicesReport {
	report := inferenceServicesReport{namespace: namespace}

	contract, err := r.servingContractOf(ctx)
	if err != nil {
		report.err = err
		return report
	}
	report.contract = contract
	if contract.state != servingStateUsable {
		return report
	}

	infServices, err := r.listServing(ctx, contract, namespace)
	if err != nil {
		// Keep the contract: what was read is still what this pass adopted, and
		// the condition is left untouched rather than reported as absent.
		report.err = fmt.Errorf("could not list InferenceService objects: %w", err)
		return report
	}

	report.present = len(infServices.Items) > 0
	return report
}
