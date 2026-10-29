package trustyaimodule

import (
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/odh-platform-utilities/pkg/deploy"
)

// NewModuleDeployer builds the Deployer used by the TrustyAI module operator.
//
// Merge is scoped to OperatorDeploymentName only. The odh/rhoai overlays
// currently render a single Deployment (trustyai-service-operator-controller-manager);
// scoping avoids accidentally preserving resources/replicas on any future
// Deployments added to the manifests template that should stay fully
// operator-managed.
//
// When opendatahub.io/managed is not "true" (absent or any other value), user
// changes to that Deployment's container resources and replicas are preserved
// across reconcile. That matches the platform allowlist contract (ODS-2664 /
// RHOAIENG-12811 / RHOAIENG-101065). Other fields (image, args, env, probes,
// volumes) continue to come from the rendered manifest.
//
// Setting opendatahub.io/managed=true disables the merge and reapplies the
// manifest values (used to restore defaults after a customization).
//
// Not in scope of this Deployer:
//   - TAS / LMES / EvalHub / NeMo Guardrails operand Deployments (owned by the
//     service operator controllers, not the module Deployer)
//   - trustyai-operator-module-controller-manager (owned by the ODH platform
//     operator, which has its own allowlist path)
//
// ClusterRole/ClusterRoleBinding are cluster-scoped; Kubernetes rejects a
// namespace-scoped owner (the TrustyAI CR) on a cluster-scoped resource, so
// they cannot use owner-reference-based ownership/GC. They are cleaned up
// explicitly in handleDeletion instead.
func NewModuleDeployer() *deploy.Deployer {
	return deploy.NewDeployer(
		deploy.WithFieldOwner(FieldManagerModule),
		deploy.WithApplyOrder(),
		deploy.WithMergeStrategy(
			appsv1.SchemeGroupVersion.WithKind("Deployment"),
			mergeOperatorDeployment,
		),
		deploy.WithExcludeFromOwnership(
			rbacv1.SchemeGroupVersion.WithKind("ClusterRole"),
			rbacv1.SchemeGroupVersion.WithKind("ClusterRoleBinding"),
		),
	)
}

// mergeOperatorDeployment preserves resources/replicas only for the TrustyAI
// service-operator Deployment. All other Deployments are left as rendered.
func mergeOperatorDeployment(existing, desired *unstructured.Unstructured) error {
	if desired == nil || desired.GetName() != OperatorDeploymentName {
		return nil
	}
	return deploy.MergeDeployments(existing, desired)
}
