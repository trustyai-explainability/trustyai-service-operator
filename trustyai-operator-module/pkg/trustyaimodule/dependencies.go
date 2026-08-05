package trustyaimodule

import (
	"context"
	stderrors "errors"
	"fmt"

	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/action"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/precondition"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// modulePreConditions defines the set of platform dependencies that must be
// satisfied before the TrustyAI operator can deploy its workloads.
//
// KServe InferenceService is optional (Info severity — does not block Ready).
// Prometheus monitoring is required (Error severity — blocks Ready and stops reconciliation).
var modulePreConditions = []precondition.PreCondition{
	precondition.MonitorCRD(
		schema.GroupVersionKind{
			Group:   "serving.kserve.io",
			Version: "v1beta1",
			Kind:    "InferenceService",
		},
		precondition.WithConditionType(ConditionTypeKServeAvailable),
		precondition.WithSeverity(common.ConditionSeverityInfo),
	),
	precondition.Custom(
		checkPrometheus,
		precondition.WithConditionType(ConditionTypeDependenciesAvailable),
		precondition.WithStopReconciliation(),
	),
}

// checkPrometheus verifies that at least one Prometheus instance exists in
// the cluster. This is required because TrustyAI relies on Prometheus for
// metrics collection.
func checkPrometheus(ctx context.Context, rr *action.ReconciliationRequest) (precondition.CheckResult, error) {
	gvk := schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "Prometheus",
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	if err := rr.Client.List(ctx, list); err != nil {
		if errors.IsNotFound(err) || isNoMatchError(err) {
			return precondition.CheckResult{
				Pass:    false,
				Message: "Monitoring is not installed (Prometheus CRD not found)",
			}, nil
		}
		return precondition.CheckResult{}, fmt.Errorf("failed to list Prometheus: %w", err)
	}

	if len(list.Items) == 0 {
		return precondition.CheckResult{
			Pass:    false,
			Message: "Prometheus CRD found but no Prometheus instance exists",
		}, nil
	}

	return precondition.CheckResult{Pass: true}, nil
}

func isNoMatchError(err error) bool {
	if err == nil {
		return false
	}
	var noKindMatchErr *meta.NoKindMatchError
	if stderrors.As(err, &noKindMatchErr) {
		return true
	}
	return errors.IsNotFound(err)
}
