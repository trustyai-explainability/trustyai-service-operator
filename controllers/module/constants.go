package module

const (
	// ServiceName is the name used for service registration
	ServiceName = "MODULE"

	// FinalizerName is the finalizer added to TrustyAI module resources
	FinalizerName = "modules.platform.opendatahub.io/finalizer"

	// DefaultRequeueInterval is the interval for periodic health checks
	DefaultRequeueInterval = 60 // seconds

	// Condition types
	ConditionTypeReady                 = "Ready"
	ConditionTypeProvisioningSucceeded = "ProvisioningSucceeded"
	ConditionTypeDegraded              = "Degraded"

	// Phases
	PhaseReady    = "Ready"
	PhaseNotReady = "Not Ready"

	// SSA Migration
	// SSAAdoptionAnnotationKey marks whether SSA adoption of in-tree resources is complete
	SSAAdoptionAnnotationKey = "trustyai.opendatahub.io/ssa-adoption-completed"

	// FieldManagerModule is the field manager name for SSA operations
	FieldManagerModule = "trustyai-module-operator"

	// InTreeManagedByLabel is the label used to identify resources managed by in-tree component
	InTreeManagedByLabel = "opendatahub.io/trustyai-component"
)
