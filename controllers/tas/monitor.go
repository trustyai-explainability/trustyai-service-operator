package tas

import (
	"context"
	"reflect"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/constants"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/tas/templates"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	centralServiceMonitorTemplatePath    = "service/service-monitor-central.tmpl.yaml"
	localServiceMonitorTemplatePath      = "service/service-monitor-local.tmpl.yaml"
	metricsCABundleConfigMapTemplatePath = "service/service-metrics-ca-bundle-configmap.tmpl.yaml"
	metricsCABundleConfigMapSuffix       = "-metrics-ca-bundle"
	metricsCABundleConfigMapKey          = "service-ca.crt"
)

type ServiceMonitorConfig struct {
	Namespace             string
	ComponentName         string
	ServiceName           string
	CABundleConfigMapName string
}

// createCentralServiceMonitorObject generates the ServiceMonitor spec for central ServiceMonitor
func createCentralServiceMonitorObject(ctx context.Context, deploymentNamespace string) (*monitoringv1.ServiceMonitor, error) {

	config := ServiceMonitorConfig{
		Namespace:     deploymentNamespace,
		ComponentName: componentName,
		ServiceName:   serviceMonitorName,
	}

	var serviceMonitor *monitoringv1.ServiceMonitor
	serviceMonitor, err := templateParser.ParseResource[*monitoringv1.ServiceMonitor](centralServiceMonitorTemplatePath, config, reflect.TypeOf(&monitoringv1.ServiceMonitor{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the central ServiceMonitor template")
		return nil, err
	}

	return serviceMonitor, nil
}

// ensureCentralServiceMonitor ensures that the central ServiceMonitor is created
func (r *TrustyAIServiceReconciler) ensureCentralServiceMonitor(ctx context.Context) error {
	serviceMonitor, err := createCentralServiceMonitorObject(ctx, r.Namespace)
	if err != nil {
		return err
	}

	// Check if this ServiceMonitor already exists
	found := &monitoringv1.ServiceMonitor{}
	err = r.Get(ctx, types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating a new central ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			err = r.Create(ctx, serviceMonitor)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to create central ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
				return err
			}
		} else {
			log.FromContext(ctx).Error(err, "Failed to get central ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			return err
		}
	}

	return nil
}

// ensureMetricsCABundleConfigMap creates the ConfigMap that OpenShift's service-CA operator
// will populate with the cluster service-CA certificate (key: "service-ca.crt"), which the
// local ServiceMonitor uses for TLS verification of scrape targets.
//
// The ConfigMap is created once and never updated: after creation, the service-CA operator
// owns the data field, and overwriting it on reconcile would wipe the injected certificate.
func (r *TrustyAIServiceReconciler) ensureMetricsCABundleConfigMap(ctx context.Context, instance *trustyaiopendatahubiov1.TrustyAIService) error {
	cmName := instance.Name + metricsCABundleConfigMapSuffix

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: instance.Namespace}, existing)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	cm, err := utils.DefineConfigMap(ctx, r.Client, instance, cmName, constants.Version, metricsCABundleConfigMapTemplatePath, templateParser.ParseResource)
	if err != nil {
		return err
	}
	return r.Create(ctx, cm)
}

// createLocalServiceMonitorObject generates the ServiceMonitor spec for a local ServiceMonitor
func createLocalServiceMonitorObject(ctx context.Context, deploymentNamespace string, serviceMonitorName string) (*monitoringv1.ServiceMonitor, error) {

	config := ServiceMonitorConfig{
		Namespace:             deploymentNamespace,
		ComponentName:         componentName,
		ServiceName:           serviceMonitorName,
		CABundleConfigMapName: serviceMonitorName + metricsCABundleConfigMapSuffix,
	}

	var serviceMonitor *monitoringv1.ServiceMonitor
	serviceMonitor, err := templateParser.ParseResource[*monitoringv1.ServiceMonitor](localServiceMonitorTemplatePath, config, reflect.TypeOf(&monitoringv1.ServiceMonitor{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the central ServiceMonitor template")
		return nil, err
	}

	return serviceMonitor, nil

}

// ensureLocalServiceMonitor ensures that the local ServiceMonitor is created
func (r *TrustyAIServiceReconciler) ensureLocalServiceMonitor(cr *trustyaiopendatahubiov1.TrustyAIService, ctx context.Context) error {
	if err := r.ensureMetricsCABundleConfigMap(ctx, cr); err != nil {
		return err
	}
	serviceMonitor, err := createLocalServiceMonitorObject(ctx, cr.Namespace, cr.Name)
	if err != nil {
		return err
	}

	// Set TrustyAIService instance as the owner and controller
	err = ctrl.SetControllerReference(cr, serviceMonitor, r.Scheme)
	if err != nil {
		return err
	}

	// Check if the ServiceMonitor already exists
	found := &monitoringv1.ServiceMonitor{}
	err = r.Get(ctx, types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating a new local ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			err = r.Create(ctx, serviceMonitor)
			if err != nil {
				log.FromContext(ctx).Error(err, "Failed to create local ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
				return err
			} else {
				r.eventLocalServiceMonitorCreated(cr)
			}
		} else {
			log.FromContext(ctx).Error(err, "Failed to get local ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			return err
		}
	}

	return nil
}
