package evalhub

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// evalHubDeploymentOperatorSettings is built from built-in defaults with optional
// resource overrides from the operator ConfigMap (trustyai-service-operator-config).
type evalHubDeploymentOperatorSettings struct {
	EvalHubResources       corev1.ResourceRequirements
	KubeRBACProxyResources corev1.ResourceRequirements
}

func defaultEvalHubDeploymentOperatorSettings() evalHubDeploymentOperatorSettings {
	return evalHubDeploymentOperatorSettings{
		EvalHubResources:       cloneResourceRequirements(defaultResourceRequirements),
		KubeRBACProxyResources: cloneResourceRequirements(defaultKubeRBACProxyResourceRequirements),
	}
}

func cloneResourceRequirements(r corev1.ResourceRequirements) corev1.ResourceRequirements {
	out := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	for k, v := range r.Requests {
		q := v.DeepCopy()
		out.Requests[k] = q
	}
	for k, v := range r.Limits {
		q := v.DeepCopy()
		out.Limits[k] = q
	}
	return out
}

func (r *EvalHubReconciler) operatorNamespace() string {
	if r.Namespace != "" {
		return r.Namespace
	}
	return "trustyai-service-operator-system"
}

func (r *EvalHubReconciler) effectiveOperatorConfigMapName() string {
	if r.OperatorConfigMapName != "" {
		return r.OperatorConfigMapName
	}
	return configMapName
}

// readOperatorConfigMapData returns ConfigMap .Data for the operator settings CM, or nil if absent / on read error.
func (r *EvalHubReconciler) readOperatorConfigMapData(ctx context.Context) map[string]string {
	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Namespace: r.operatorNamespace(), Name: r.effectiveOperatorConfigMapName()}
	err := r.Get(ctx, key, cm)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		log.FromContext(ctx).Error(err, "could not read operator ConfigMap for EvalHub deployment tuning; using built-in defaults",
			"namespace", key.Namespace, "name", key.Name)
		return nil
	}
	return cm.Data
}

func mergeEvalHubDeploymentOperatorSettings(ctx context.Context, data map[string]string) evalHubDeploymentOperatorSettings {
	logger := log.FromContext(ctx)
	s := defaultEvalHubDeploymentOperatorSettings()
	if len(data) == 0 {
		return s
	}

	applyQuantity := func(list corev1.ResourceList, key string, name corev1.ResourceName) {
		raw, ok := data[key]
		if !ok || raw == "" {
			return
		}
		q, err := resource.ParseQuantity(raw)
		if err != nil {
			logger.Info("ignoring invalid resource quantity in operator ConfigMap", "key", key, "value", raw, "error", err.Error())
			return
		}
		list[name] = q
	}

	applyQuantity(s.EvalHubResources.Requests, configMapEvalHubCPURequestKey, corev1.ResourceCPU)
	applyQuantity(s.EvalHubResources.Requests, configMapEvalHubMemoryRequestKey, corev1.ResourceMemory)
	applyQuantity(s.EvalHubResources.Limits, configMapEvalHubCPULimitKey, corev1.ResourceCPU)
	applyQuantity(s.EvalHubResources.Limits, configMapEvalHubMemoryLimitKey, corev1.ResourceMemory)

	applyQuantity(s.KubeRBACProxyResources.Requests, configMapKubeRBACProxyCPURequestKey, corev1.ResourceCPU)
	applyQuantity(s.KubeRBACProxyResources.Requests, configMapKubeRBACProxyMemoryRequestKey, corev1.ResourceMemory)
	applyQuantity(s.KubeRBACProxyResources.Limits, configMapKubeRBACProxyCPULimitKey, corev1.ResourceCPU)
	applyQuantity(s.KubeRBACProxyResources.Limits, configMapKubeRBACProxyMemoryLimitKey, corev1.ResourceMemory)

	return s
}
