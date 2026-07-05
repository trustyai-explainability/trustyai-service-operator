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

	// ConfigMap names
	DSCConfigMapName = "trustyai-dsc-config"

	// ConfigMap keys
	LMEvalPermitCodeExecutionKey = "eval.lmeval.permitCodeExecution"
	LMEvalPermitOnlineKey        = "eval.lmeval.permitOnline"
)
