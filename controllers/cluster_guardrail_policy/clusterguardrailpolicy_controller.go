/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster_guardrail_policy

import (
	"context"
	"fmt"
	"reflect"
	"time"

	clusterguardrailpolicyv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/cluster_guardrail_policy/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ClusterGuardrailPolicyReconciler reconciles a ClusterGuardrailPolicy object
type ClusterGuardrailPolicyReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Namespace string
	Recorder  record.EventRecorder
}

// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=clusterguardrailpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=clusterguardrailpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=clusterguardrailpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=nemoguardrails,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=nemoguardrails/status,verbs=get
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *ClusterGuardrailPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// ====== Fetch ClusterGuardrailPolicy CR ===========================================================================
	instance := &clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ClusterGuardrailPolicy resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ClusterGuardrailPolicy")
		return ctrl.Result{}, err
	}

	// ====== Finalizer handling ========================================================================================
	if instance.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(instance, finalizerName) {
			if err := r.cleanupManagedResources(ctx, instance); err != nil {
				logger.Error(err, "Failed to clean up managed resources")
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(instance, finalizerName)
			if err := r.Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(instance, finalizerName) {
		controllerutil.AddFinalizer(instance, finalizerName)
		if err := r.Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	// ====== Validate target Gateway ===================================================================================
	gatewayFound, err := r.validateGateway(ctx, instance)
	if err != nil {
		logger.Error(err, "Failed to validate Gateway")
		r.updateCondition(ctx, instance, "Accepted", corev1.ConditionFalse, "GatewayError", err.Error())
		instance.Status.Phase = "Error"
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if !gatewayFound {
		msg := fmt.Sprintf("Gateway %s/%s not found", instance.Spec.TargetNamespace, instance.Spec.TargetRef.Name)
		logger.Info(msg)
		r.updateCondition(ctx, instance, "Accepted", corev1.ConditionFalse, "GatewayNotFound", msg)
		instance.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, instance); err != nil {
			logger.Error(err, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	r.updateCondition(ctx, instance, "Accepted", corev1.ConditionTrue, "GatewayFound", "Target Gateway found")

	// ====== Resolve effective guardrails ================================================================================
	effectiveGuardrails := resolveEffectiveGuardrails(&instance.Spec)

	// ====== Reconcile NemoGuardrails CR ================================================================================
	if effectiveGuardrails != nil && effectiveGuardrails.NemoGuardrails != nil {
		nemoReady, err := r.reconcileNemoGuardrails(ctx, instance, effectiveGuardrails)
		if err != nil {
			logger.Error(err, "Failed to reconcile NemoGuardrails")
			r.updateCondition(ctx, instance, "NemoGuardrailsReady", corev1.ConditionFalse, "ReconcileError", err.Error())
			instance.Status.Phase = "Error"
			if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		if nemoReady {
			r.updateCondition(ctx, instance, "NemoGuardrailsReady", corev1.ConditionTrue, "Ready", "NemoGuardrails CR is ready")
			instance.Status.Phase = "Ready"
		} else {
			r.updateCondition(ctx, instance, "NemoGuardrailsReady", corev1.ConditionFalse, "Progressing", "NemoGuardrails CR is being deployed")
			instance.Status.Phase = "Progressing"
		}
	} else {
		instance.Status.Phase = "Ready"
	}

	// ====== Reconcile IPP Plugin ConfigMaps ===========================================================================
	if effectiveGuardrails != nil && (effectiveGuardrails.InputGuard != nil || effectiveGuardrails.OutputGuard != nil) {
		instance.Spec.Guardrails = effectiveGuardrails
		if err := r.reconcileIPPConfig(ctx, instance); err != nil {
			logger.Error(err, "Failed to reconcile IPP config")
			r.updateCondition(ctx, instance, "IPPConfigReady", corev1.ConditionFalse, "ReconcileError", err.Error())
			instance.Status.Phase = "Error"
			if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		r.updateCondition(ctx, instance, "IPPConfigReady", corev1.ConditionTrue, "Ready", "IPP plugin ConfigMaps are ready")
	}

	// ====== Update status =============================================================================================
	if err := r.Status().Update(ctx, instance); err != nil {
		logger.Error(err, "Failed to update ClusterGuardrailPolicy status")
		return ctrl.Result{}, err
	}

	logger.Info("RECONCILE DONE")
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// validateGateway checks if the target Gateway exists.
func (r *ClusterGuardrailPolicyReconciler) validateGateway(ctx context.Context, instance *clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy) (bool, error) {
	gw := &unstructured.Unstructured{}
	gw.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "Gateway",
	})
	err := r.Get(ctx, types.NamespacedName{
		Name:      string(instance.Spec.TargetRef.Name),
		Namespace: instance.Spec.TargetNamespace,
	}, gw)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// reconcileNemoGuardrails creates or updates the managed NemoGuardrails CR.
func (r *ClusterGuardrailPolicyReconciler) reconcileNemoGuardrails(ctx context.Context, instance *clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy, effectiveGuardrails *clusterguardrailpolicyv1alpha1.GuardrailRules) (bool, error) {
	logger := log.FromContext(ctx)
	nemoName := fmt.Sprintf("%s-nemo", instance.Name)
	nemoNamespace := instance.Spec.TargetNamespace
	nemoConfig := effectiveGuardrails.NemoGuardrails

	existing := &nemoguardrailsv1alpha1.NemoGuardrails{}
	err := r.Get(ctx, types.NamespacedName{Name: nemoName, Namespace: nemoNamespace}, existing)

	desiredSpec := nemoguardrailsv1alpha1.NemoGuardrailsSpec{
		NemoConfigs: r.toNemoConfigs(nemoConfig.NemoConfigs),
		Replicas:    nemoConfig.Replicas,
		Env:         nemoConfig.Env,
	}

	if err != nil && errors.IsNotFound(err) {
		nemo := &nemoguardrailsv1alpha1.NemoGuardrails{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nemoName,
				Namespace: nemoNamespace,
				Labels: map[string]string{
					managedByLabel: instance.Name,
				},
				Annotations: map[string]string{
					managedByAnnotation: instance.Name,
				},
			},
			Spec: desiredSpec,
		}
		logger.Info("Creating NemoGuardrails CR", "name", nemoName, "namespace", nemoNamespace)
		if err := r.Create(ctx, nemo); err != nil {
			return false, err
		}

		instance.Status.NemoGuardrailsRef = &clusterguardrailpolicyv1alpha1.ManagedResourceRef{
			Name:      nemoName,
			Namespace: nemoNamespace,
			Ready:     false,
		}
		return false, nil
	} else if err != nil {
		return false, err
	}

	needsUpdate := false
	if !reflect.DeepEqual(existing.Spec, desiredSpec) {
		existing.Spec = desiredSpec
		needsUpdate = true
	}
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	if existing.Labels[managedByLabel] != instance.Name {
		existing.Labels[managedByLabel] = instance.Name
		needsUpdate = true
	}
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	if existing.Annotations[managedByAnnotation] != instance.Name {
		existing.Annotations[managedByAnnotation] = instance.Name
		needsUpdate = true
	}

	if needsUpdate {
		if err := r.Update(ctx, existing); err != nil {
			return false, err
		}
	}

	isReady := existing.Status.Phase == "Ready"
	instance.Status.NemoGuardrailsRef = &clusterguardrailpolicyv1alpha1.ManagedResourceRef{
		Name:      nemoName,
		Namespace: nemoNamespace,
		Ready:     isReady,
	}
	return isReady, nil
}

// toNemoConfigs converts the policy NemoConfigRef to the NemoGuardrails NemoConfig type.
func (r *ClusterGuardrailPolicyReconciler) toNemoConfigs(configs []clusterguardrailpolicyv1alpha1.NemoConfigRef) []nemoguardrailsv1alpha1.NemoConfig {
	result := make([]nemoguardrailsv1alpha1.NemoConfig, len(configs))
	for i, c := range configs {
		result[i] = nemoguardrailsv1alpha1.NemoConfig{
			Name:       c.Name,
			ConfigMaps: c.ConfigMaps,
			Default:    c.Default,
		}
	}
	return result
}

// cleanupManagedResources deletes NemoGuardrails CRs and IPP ConfigMaps managed by this policy.
func (r *ClusterGuardrailPolicyReconciler) cleanupManagedResources(ctx context.Context, instance *clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy) error {
	logger := log.FromContext(ctx)

	nemoList := &nemoguardrailsv1alpha1.NemoGuardrailsList{}
	if err := r.List(ctx, nemoList, client.MatchingLabels{managedByLabel: instance.Name}); err != nil {
		return err
	}
	for i := range nemoList.Items {
		logger.Info("Deleting managed NemoGuardrails CR", "name", nemoList.Items[i].Name, "namespace", nemoList.Items[i].Namespace)
		if err := r.Delete(ctx, &nemoList.Items[i]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	cmList := &corev1.ConfigMapList{}
	if err := r.List(ctx, cmList, client.MatchingLabels{managedByLabel: instance.Name}); err != nil {
		return err
	}
	for i := range cmList.Items {
		logger.Info("Deleting managed ConfigMap", "name", cmList.Items[i].Name, "namespace", cmList.Items[i].Namespace)
		if err := r.Delete(ctx, &cmList.Items[i]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// updateCondition sets or updates a condition on the ClusterGuardrailPolicy status.
func (r *ClusterGuardrailPolicyReconciler) updateCondition(ctx context.Context, instance *clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy, condType string, status corev1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	newCondition := common.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	}

	for i, c := range instance.Status.Conditions {
		if c.Type == condType {
			if c.Status != status {
				instance.Status.Conditions[i] = newCondition
			} else {
				instance.Status.Conditions[i].Reason = reason
				instance.Status.Conditions[i].Message = message
			}
			return
		}
	}
	instance.Status.Conditions = append(instance.Status.Conditions, newCondition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterGuardrailPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy{}).
		Watches(
			&nemoguardrailsv1alpha1.NemoGuardrails{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				labels := obj.GetLabels()
				if labels == nil {
					return nil
				}
				policyName, ok := labels[managedByLabel]
				if !ok {
					return nil
				}
				return []reconcile.Request{
					{NamespacedName: types.NamespacedName{Name: policyName}},
				}
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

// ControllerSetUp is the registered function to set up the CLUSTER_GUARDRAIL_POLICY controller.
func ControllerSetUp(mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error {
	return (&ClusterGuardrailPolicyReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Namespace: ns,
		Recorder:  recorder,
	}).SetupWithManager(mgr)
}
