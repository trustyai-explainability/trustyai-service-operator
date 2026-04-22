package evalhub

import (
	"context"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func networkPolicyName(instance *evalhubv1alpha1.EvalHub) string {
	return instance.Name + "-allow-ingress"
}

func (r *EvalHubReconciler) buildNetworkPolicy(instance *evalhubv1alpha1.EvalHub) *networkingv1.NetworkPolicy {
	apiPort := intstr.FromInt(containerPort)

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      networkPolicyName(instance),
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app":       "eval-hub",
				"component": "api",
				"instance":  instance.Name,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":       "eval-hub",
					"component": "api",
					"instance":  instance.Name,
				},
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Port:     &apiPort,
							Protocol: protocolPtr(corev1.ProtocolTCP),
						},
					},
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"evalhub.trustyai.opendatahub.io/tenant": "",
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"openshift.io/cluster-monitoring": "true",
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"network.openshift.io/policy-group": "ingress",
								},
							},
						},
						{
							PodSelector: &metav1.LabelSelector{},
						},
					},
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
		},
	}
}

func protocolPtr(p corev1.Protocol) *corev1.Protocol {
	return &p
}

func (r *EvalHubReconciler) reconcileNetworkPolicy(ctx context.Context, instance *evalhubv1alpha1.EvalHub) error {
	log := log.FromContext(ctx)
	log.Info("Reconciling NetworkPolicy", "name", networkPolicyName(instance))

	desired := r.buildNetworkPolicy(instance)
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	found := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating NetworkPolicy", "name", desired.Name)
			return r.Create(ctx, desired)
		}
		return err
	}

	found.Spec = desired.Spec
	log.Info("Updating NetworkPolicy", "name", found.Name)
	return r.Update(ctx, found)
}
