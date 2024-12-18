package guardrails

import (
	"context"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	routev1 "github.com/openshift/api/route/v1"
	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DEPLOYMENT_MODE_RAW        = "RawDeployment"
	DEPLOYMENT_MODE_SERVERLESS = "Serverless"
)

func (r *GuardrailsReconciler) ensureInferenceServices(ctx context.Context, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator, namespace string) (bool, error) {
	// check if the infere
	var inferenceservices kservev1beta1.InferenceServiceList
	if err := r.List(ctx, &inferenceservices, client.InNamespace(namespace)); err != nil {
		return false, err
	}
	for _, isvc := range inferenceservices.Items {
		annotations := isvc.GetAnnotations()
		if val, ok := annotations["serving.kserve.io/deploymentMode"]; ok {
			if val == DEPLOYMENT_MODE_SERVERLESS {
				log.FromContext(ctx, "Guardrails Orchestrator does not support KServe Serveless")
			} else if val == DEPLOYMENT_MODE_RAW {
				// create external route
				route := r.patch4KServeRaw(ctx, *orchestrator, isvc.Name)
				if route != nil {
					log.FromContext(ctx, "Failed to patch route for KServe RawDeployment Inference Service")
				}
			}
		}
	}
	return true, nil
}

func (r *GuardrailsReconciler) patch4KServeRaw(ctx context.Context, orchestrator guardrailsv1alpha1.GuardrailsOrchestrator, inferenceServiceName string) *routev1.Route {
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      inferenceServiceName,
			Namespace: orchestrator.Namespace,
		},
		Spec: routev1.RouteSpec{
			Host: "",
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: inferenceServiceName + "-predictor",
				// Weight: 100,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("h2c"),
			},
			WildcardPolicy: routev1.WildcardPolicyNone,
		},
	}
	return route
}
