package trustyaimodule

const (
	// FinalizerName is the finalizer added to TrustyAI module resources
	FinalizerName = "modules.platform.opendatahub.io/finalizer"

	// DefaultRequeueInterval is the interval in seconds for periodic health checks
	DefaultRequeueInterval = 60

	// Condition types
	ConditionTypeReady                 = "Ready"
	ConditionTypeProvisioningSucceeded = "ProvisioningSucceeded"
	ConditionTypeDegraded              = "Degraded"
	ConditionTypeDependenciesMet       = "DependenciesMet"

	// Phases
	PhaseReady    = "Ready"
	PhaseNotReady = "Not Ready"

	// Reasons
	ReasonModuleUnmanaged = "ModuleUnmanaged"

	// Event reasons
	EventReasonRemoved       = "Removed"
	EventReasonUnmanaged     = "Unmanaged"
	EventReasonStatusUpdated = "StatusUpdated"

	// ConfigMap names
	DSCConfigMapName = "trustyai-dsc-config"

	// ConfigMap keys
	LMEvalPermitCodeExecutionKey = "eval.lmeval.permitCodeExecution"
	LMEvalPermitOnlineKey        = "eval.lmeval.permitOnline"

	// SSAAdoptionAnnotationKey marks whether SSA adoption of in-tree resources is complete
	SSAAdoptionAnnotationKey = "trustyai.opendatahub.io/ssa-adoption-completed"

	// AdoptedFromAnnotationKey marks resources that were adopted from in-tree component
	AdoptedFromAnnotationKey = "trustyai.opendatahub.io/adopted-from"

	// FieldManagerModule is the field manager name for SSA operations
	FieldManagerModule = "trustyai-module-operator"

	// InTreeManagedByLabel is the label used to identify resources managed by in-tree component
	InTreeManagedByLabel = "opendatahub.io/trustyai-component"
)
