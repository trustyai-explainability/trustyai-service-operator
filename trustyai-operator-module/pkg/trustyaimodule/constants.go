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

	// ConditionTypeKServeAvailable is the condition type for the optional KServe InferenceService CRD check.
	// Kept separate from ConditionTypeDependenciesAvailable so the informational KServe result cannot
	// overwrite or hide the required Prometheus dependency result during RunAll aggregation.
	ConditionTypeKServeAvailable = "KServeAvailable"

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
	OperatorDeploymentName = "trustyai-service-operator"

	// ManagerContainerName is the name of the trustyai-service-operator
	// container within OperatorDeploymentName.
	ManagerContainerName = "manager"
)

// clusterRoleNames and clusterRoleBindingNames list the ClusterRole and
// ClusterRoleBinding names rendered from
// config/manifests-template/base/rbac. These are excluded from
// owner-reference-based ownership (Kubernetes rejects a namespace-scoped
// owner on a cluster-scoped resource), so they must be deleted explicitly
// during finalizer cleanup instead of relying on garbage collection.
var clusterRoleNames = []string{
	"tls-profile-reader",
	"tas-manager-role",
	"lmes-manager-role",
	"evalhub-manager-role",
	"gorch-manager-role",
	"nemo-guardrails-manager-role",
}

var clusterRoleBindingNames = []string{
	"tls-profile-reader-binding",
	"tas-manager-rolebinding",
	"lmes-manager-rolebinding",
	"evalhub-manager-rolebinding",
	"gorch-manager-rolebinding",
	"nemo-guardrails-manager-rolebinding",
}
