package tas

import (
	"context"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/tas/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
)

const kubeRBACProxyConfigTemplatePath = "kube-rbac-proxy/config.tmpl.yaml"

// createKubeRBACProxyConfigMapObject creates the ConfigMap object from template
func (r *TrustyAIServiceReconciler) createKubeRBACProxyConfigMapObject(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) (*corev1.ConfigMap, error) {
	return utils.DefineConfigMap(ctx, r.Client, instance, instance.Name+"-kube-rbac-proxy-config", constants.Version, kubeRBACProxyConfigTemplatePath, templateParser.ParseResource)
}

// ensureKubeRBACProxyConfigMap ensures the kube-rbac-proxy ConfigMap exists
func (r *TrustyAIServiceReconciler) ensureKubeRBACProxyConfigMap(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	rbacConfigMapName := instance.Name + "-kube-rbac-proxy-config"
	return utils.EnsureConfigMap(ctx, r.Client, instance, rbacConfigMapName, constants.Version, kubeRBACProxyConfigTemplatePath, templateParser.ParseResource)
}
