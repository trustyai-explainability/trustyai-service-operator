package tas

import (
	"context"
	"reflect"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	templateParser "github.com/trustyai-explainability/trustyai-service-operator/controllers/tas/templates"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	centralServiceMonitorTemplatePath = "service/service-monitor-central.tmpl.yaml"
	localServiceMonitorTemplatePath   = "service/service-monitor-local.tmpl.yaml"
)

const (
	metricsCABundleConfigMapSuffix = "-metrics-ca-bundle"
	metricsCABundleConfigMapKey    = "service-ca.crt"
	metricsReaderSuffix            = "-metrics-reader"
	metricsReaderTokenSuffix       = "-metrics-reader-token"
)

type ServiceMonitorConfig struct {
	Namespace             string
	ComponentName         string
	ServiceName           string
	CABundleConfigMapName string
	TokenSecretName       string
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

// createLocalServiceMonitorObject generates the ServiceMonitor spec for a local ServiceMonitor
func createLocalServiceMonitorObject(ctx context.Context, deploymentNamespace string, serviceMonitorName string) (*monitoringv1.ServiceMonitor, error) {

	config := ServiceMonitorConfig{
		Namespace:             deploymentNamespace,
		ComponentName:         componentName,
		ServiceName:           serviceMonitorName,
		CABundleConfigMapName: serviceMonitorName + metricsCABundleConfigMapSuffix,
		TokenSecretName:       serviceMonitorName + metricsReaderTokenSuffix,
	}

	var serviceMonitor *monitoringv1.ServiceMonitor
	serviceMonitor, err := templateParser.ParseResource[*monitoringv1.ServiceMonitor](localServiceMonitorTemplatePath, config, reflect.TypeOf(&monitoringv1.ServiceMonitor{}))
	if err != nil {
		log.FromContext(ctx).Error(err, "Error parsing the local ServiceMonitor template")
		return nil, err
	}

	return serviceMonitor, nil

}

// ensureLocalServiceMonitor ensures that the local ServiceMonitor is created
func (r *TrustyAIServiceReconciler) ensureLocalServiceMonitor(cr *trustyaiopendatahubiov1.TrustyAIService, ctx context.Context) error {
	serviceMonitor, err := createLocalServiceMonitorObject(ctx, cr.Namespace, cr.Name)
	if err != nil {
		return err
	}

	// Set TrustyAIService instance as the owner and controller
	err = controllerutil.SetControllerReference(cr, serviceMonitor, r.Scheme)
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
	} else if !reflect.DeepEqual(found.Spec, serviceMonitor.Spec) {
		found.Spec = serviceMonitor.Spec
		if err = r.Update(ctx, found); err != nil {
			log.FromContext(ctx).Error(err, "Failed to update local ServiceMonitor")
			return err
		}
	}

	return nil
}

// ensureMetricsCABundleConfigMap creates an empty ConfigMap annotated so that
// the OpenShift service-CA operator injects the cluster CA bundle into it.
// The local ServiceMonitor references this ConfigMap for TLS verification.
func (r *TrustyAIServiceReconciler) ensureMetricsCABundleConfigMap(cr *trustyaiopendatahubiov1.TrustyAIService, ctx context.Context) error {
	cmName := cr.Name + metricsCABundleConfigMapSuffix

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cr.Namespace,
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle": "true",
			},
			Labels: map[string]string{
				"app.kubernetes.io/part-of": componentName,
			},
		},
	}

	if err := controllerutil.SetControllerReference(cr, cm, r.Scheme); err != nil {
		return err
	}

	found := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cr.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating metrics CA bundle ConfigMap", "Namespace", cr.Namespace, "Name", cmName)
		if err = r.Create(ctx, cm); err != nil {
			return err
		}
		r.eventMetricsCABundleConfigMapCreated(cr)
		return nil
	}
	return err
}

// ensureMetricsReaderServiceAccount creates a dedicated ServiceAccount and a
// long-lived token Secret for Prometheus to authenticate through kube-rbac-proxy.
func (r *TrustyAIServiceReconciler) ensureMetricsReaderServiceAccount(cr *trustyaiopendatahubiov1.TrustyAIService, ctx context.Context) error {
	saName := cr.Name + metricsReaderSuffix
	secretName := cr.Name + metricsReaderTokenSuffix

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: cr.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/part-of": componentName,
			},
		},
	}

	if err := controllerutil.SetControllerReference(cr, sa, r.Scheme); err != nil {
		return err
	}

	foundSA := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: saName, Namespace: cr.Namespace}, foundSA)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating metrics-reader ServiceAccount", "Namespace", cr.Namespace, "Name", saName)
		if err = r.Create(ctx, sa); err != nil {
			return err
		}
		r.eventMetricsReaderSACreated(cr)
	} else if err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: cr.Namespace,
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": saName,
			},
			Labels: map[string]string{
				"app.kubernetes.io/part-of": componentName,
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}

	if err := controllerutil.SetControllerReference(cr, secret, r.Scheme); err != nil {
		return err
	}

	foundSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cr.Namespace}, foundSecret)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating metrics-reader token Secret", "Namespace", cr.Namespace, "Name", secretName)
		if err = r.Create(ctx, secret); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}

// ensurePrometheusRBAC creates a Role and RoleBinding granting the metrics-reader
// SA permission to GET the TrustyAI service in this namespace.
// kube-rbac-proxy performs a SubjectAccessReview for "get services/<name>",
// so the SA needs this permission for Prometheus to scrape metrics through the proxy.
func (r *TrustyAIServiceReconciler) ensurePrometheusRBAC(cr *trustyaiopendatahubiov1.TrustyAIService, ctx context.Context) error {
	saName := cr.Name + metricsReaderSuffix
	roleName := cr.Name + metricsReaderSuffix

	desiredRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: cr.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/part-of": componentName,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{""},
				Resources:     []string{"services"},
				ResourceNames: []string{cr.Name},
				Verbs:         []string{"get"},
			},
			{
				APIGroups:     []string{""},
				Resources:     []string{"secrets"},
				ResourceNames: []string{cr.Name + metricsReaderTokenSuffix},
				Verbs:         []string{"get"},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cr, desiredRole, r.Scheme); err != nil {
		return err
	}

	foundRole := &rbacv1.Role{}
	err := r.Get(ctx, types.NamespacedName{Name: desiredRole.Name, Namespace: desiredRole.Namespace}, foundRole)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating Prometheus metrics-reader Role", "Namespace", desiredRole.Namespace)
		if err = r.Create(ctx, desiredRole); err != nil {
			return err
		}
		r.eventMetricsRoleCreated(cr)
	} else if err != nil {
		return err
	} else if !reflect.DeepEqual(foundRole.Rules, desiredRole.Rules) {
		foundRole.Rules = desiredRole.Rules
		if err = r.Update(ctx, foundRole); err != nil {
			return err
		}
	}

	desiredBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: cr.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/part-of": componentName,
			},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: cr.Namespace,
			},
			{
				Kind:      "ServiceAccount",
				Name:      "prometheus-user-workload",
				Namespace: prometheusUserWorkloadNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     roleName,
			APIGroup: rbacv1.GroupName,
		},
	}

	if err := controllerutil.SetControllerReference(cr, desiredBinding, r.Scheme); err != nil {
		return err
	}

	foundBinding := &rbacv1.RoleBinding{}
	err = r.Get(ctx, types.NamespacedName{Name: desiredBinding.Name, Namespace: desiredBinding.Namespace}, foundBinding)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating Prometheus metrics-reader RoleBinding", "Namespace", desiredBinding.Namespace)
		if err = r.Create(ctx, desiredBinding); err != nil {
			return err
		}
		r.eventMetricsRoleBindingCreated(cr)
	} else if err != nil {
		return err
	} else if !reflect.DeepEqual(foundBinding.RoleRef, desiredBinding.RoleRef) {
		// RoleRef is immutable — must delete and recreate
		if err = r.Delete(ctx, foundBinding); err != nil {
			return err
		}
		if err = r.Create(ctx, desiredBinding); err != nil {
			return err
		}
		r.eventMetricsRoleBindingCreated(cr)
	} else if !reflect.DeepEqual(foundBinding.Subjects, desiredBinding.Subjects) {
		foundBinding.Subjects = desiredBinding.Subjects
		if err = r.Update(ctx, foundBinding); err != nil {
			return err
		}
	}

	return nil
}
