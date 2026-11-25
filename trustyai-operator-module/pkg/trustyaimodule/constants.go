package trustyaimodule

import "github.com/opendatahub-io/odh-platform-utilities/pkg/controller/precondition"

const (
	// FinalizerName is the finalizer added to TrustyAI module resources
	FinalizerName = "modules.platform.opendatahub.io/finalizer"

	// DefaultRequeueInterval is the interval in seconds for periodic health checks
	DefaultRequeueInterval = 60

	// ConditionTypeDependenciesAvailable is the condition type for required dependency gate checks.
	// Alias of precondition.ConditionTypeDependenciesAvailable for local use.
	ConditionTypeDependenciesAvailable = precondition.ConditionTypeDependenciesAvailable

	// ConditionTypeKServeAvailable reports whether the optional KServe
	// InferenceService API is installed. It is an observation, not a gate: the API
	// is an integration TrustyAI works without, so an absent one is reported with
	// ConditionReasonServingAPINotInstalled and never as a failed precondition.
	// Kept separate from ConditionTypeDependenciesAvailable, which carries the
	// required Prometheus dependency.
	ConditionTypeKServeAvailable = "KServeAvailable"

	// Reasons the optional serving API reports. Only ConditionReasonServingAPIUnreadable
	// describes a problem; the other two are the normal states of a supported install.
	ConditionReasonServingAPIAvailable    = "Available"
	ConditionReasonServingAPINotInstalled = "NotInstalled"
	ConditionReasonServingAPIUnreadable   = "ReadFailed"

	// Event reasons
	EventReasonRemoved       = "Removed"
	EventReasonStatusUpdated = "StatusUpdated"

	// ConfigMap names
	DSCConfigMapName = "trustyai-dsc-config"

	// PlatformConfigMapName is the platform-managed ConfigMap the platform
	// operator creates/maintains for this module (odh-<modulename>-config).
	// See the platform version handshake doc.
	PlatformConfigMapName = "odh-trustyai-config"

	// ConfigMap keys
	LMEvalPermitCodeExecutionKey = "eval.lmeval.permitCodeExecution"
	LMEvalPermitOnlineKey        = "eval.lmeval.permitOnline"

	// PlatformVersionKey is the data key in PlatformConfigMapName holding the
	// platform operator's current version.
	PlatformVersionKey = "platformVersion"

	// SSAAdoptionAnnotationKey marks whether SSA adoption of in-tree resources is complete
	SSAAdoptionAnnotationKey = "trustyai.opendatahub.io/ssa-adoption-completed"

	// AdoptedFromAnnotationKey marks resources that were adopted from in-tree component
	AdoptedFromAnnotationKey = "trustyai.opendatahub.io/adopted-from"

	// FieldManagerModule is the field manager name for SSA operations
	FieldManagerModule = "trustyai-module-operator"

	// InTreeManagedByLabel is the label used to identify resources managed by in-tree component
	InTreeManagedByLabel = "opendatahub.io/trustyai-component"

	// OperatorDeploymentName is the name of the trustyai-service-operator
	// Deployment rendered from the manifests template.
	OperatorDeploymentName = "trustyai-service-operator-controller-manager"

	// ManagerContainerName is the name of the trustyai-service-operator
	// container within OperatorDeploymentName.
	ManagerContainerName = "manager"
)
