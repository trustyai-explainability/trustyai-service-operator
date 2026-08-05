package trustyaimodule

import "github.com/opendatahub-io/odh-platform-utilities/pkg/controller/precondition"

const (
	// FinalizerName is the finalizer added to TrustyAI module resources
	FinalizerName = "modules.platform.opendatahub.io/finalizer"

	// DefaultRequeueInterval is the interval in seconds for periodic health checks
	DefaultRequeueInterval = 60

	// ConditionTypeDependenciesAvailable is the condition type for dependency gate checks.
	// Alias of precondition.ConditionTypeDependenciesAvailable for local use.
	ConditionTypeDependenciesAvailable = precondition.ConditionTypeDependenciesAvailable

	// Event reasons
	EventReasonRemoved       = "Removed"
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
