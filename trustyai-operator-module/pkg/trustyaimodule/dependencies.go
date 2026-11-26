package trustyaimodule

import (
	"context"
	stderrors "errors"
	"fmt"

	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/action"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/conditions"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/controller/precondition"
	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// servingAPIGVK is the optional InferenceService API. TrustyAI integrates with it
// when it is there and works without it, so nothing about it is a precondition.
var servingAPIGVK = schema.GroupVersionKind{
	Group:   "serving.kserve.io",
	Version: "v1beta1",
	Kind:    "InferenceService",
}

// modulePreConditions defines the set of platform dependencies that must be
// satisfied before the TrustyAI operator can deploy its workloads.
//
// Prometheus monitoring is required (Error severity — blocks Ready and stops
// reconciliation). The optional InferenceService API is deliberately not here: a
// precondition reports PreConditionFailed when it is unmet, which would describe a
// supported configuration (TrustyAI without KServe) as a broken requirement. It is
// reported by reportServingAPI as an ordinary observation instead.
var modulePreConditions = []precondition.PreCondition{
	precondition.Custom(
		checkPrometheus,
		precondition.WithConditionType(ConditionTypeDependenciesAvailable),
		precondition.WithStopReconciliation(),
	),
}

// reportServingAPI records whether the optional InferenceService API is installed.
//
// The API is an integration, not a dependency. Reporting it through a precondition
// made an absent KServe look like a fault — `KServeAvailable: False`,
// `reason: PreConditionFailed` — which reads as an unmet requirement and keeps an
// unsupported-but-healthy install in a failed-looking state on every reconcile.
// Here the same fact is stated as what it is:
//
//   - installed:  `True`/`Available`, the integration is active;
//   - absent:     `False`/`NotInstalled`, Info severity, nothing is wrong;
//   - unreadable: `Unknown`/`ReadFailed` with the API server's error, which is the
//     only one of the three worth investigating.
//
// None of the three stops reconciliation or touches Ready: a serving API the
// platform does not install cannot gate a stack that does not need it. An
// unreadable API is logged and requeued on the normal reconcile interval rather
// than returned as an error, for the same reason.
func reportServingAPI(
	ctx context.Context,
	c client.Client,
	module *platformv1alpha1.TrustyAI,
	condMgr *conditions.Manager,
) {
	if c == nil || condMgr == nil {
		return
	}
	logger := log.FromContext(ctx)
	generation := conditions.WithObservedGeneration(module.Generation)
	severity := conditions.WithSeverity(common.ConditionSeverityInfo)

	if _, err := c.RESTMapper().RESTMapping(servingAPIGVK.GroupKind(), servingAPIGVK.Version); err != nil {
		if meta.IsNoMatchError(err) {
			condMgr.MarkFalse(ConditionTypeKServeAvailable,
				conditions.WithReason(ConditionReasonServingAPINotInstalled),
				conditions.WithMessage(
					"%s.%s is not installed; the serving integration stays inactive",
					servingAPIGVK.Kind, servingAPIGVK.Group,
				),
				generation,
				severity,
			)

			return
		}

		logger.Error(err, "Could not read the InferenceService API, reporting it as unknown")
		condMgr.MarkUnknown(ConditionTypeKServeAvailable,
			conditions.WithReason(ConditionReasonServingAPIUnreadable),
			conditions.WithMessage(
				"could not read %s.%s: %v", servingAPIGVK.Kind, servingAPIGVK.Group, err,
			),
			generation,
			severity,
		)

		return
	}

	condMgr.MarkTrue(ConditionTypeKServeAvailable,
		conditions.WithReason(ConditionReasonServingAPIAvailable),
		conditions.WithMessage(
			"%s.%s is available; the serving integration is active",
			servingAPIGVK.Kind, servingAPIGVK.Group,
		),
		generation,
		severity,
	)
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
