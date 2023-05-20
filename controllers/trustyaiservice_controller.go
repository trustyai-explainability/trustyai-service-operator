/*
Copyright 2023.

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

package controllers

import (
	"context"
	"fmt"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1alpha1 "github.com/ruivieira/trustyai-service-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const defaultImage = string("quay.io/trustyai/trustyai-service")
const defaultTag = string("latest")

// TrustyAIServiceReconciler reconciles a TrustyAIService object
type TrustyAIServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=trustyai.opendatahub.io.trustyai.opendatahub.io,resources=trustyaiservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=trustyai.opendatahub.io.trustyai.opendatahub.io,resources=trustyaiservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=trustyai.opendatahub.io.trustyai.opendatahub.io,resources=trustyaiservices/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=list;watch;get;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// getCommonLabels returns the service's common labels
func getCommonLabels(serviceName string) map[string]string {
	return map[string]string{
		"app":                        serviceName,
		"app.kubernetes.io/name":     serviceName,
		"app.kubernetes.io/instance": serviceName,
		"app.kubernetes.io/part-of":  serviceName,
		"app.kubernetes.io/version":  "0.1.0",
	}
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TrustyAIServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	// Fetch the AppService instance
	instance := &trustyaiopendatahubiov1alpha1.TrustyAIService{}
	err := r.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		// Handle error
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	if instance.Spec.Image == "" {
		instance.Spec.Image = defaultImage
	}
	if instance.Spec.Tag == "" {
		instance.Spec.Tag = defaultTag
	}

	// Create or update ModelMesh's ConfigMap
	if err := r.createOrUpdateModelMeshConfigMap(instance, ctx); err != nil {
		if errors.IsNotFound(err) {
			// Log the error but don't return
			log.FromContext(ctx).Error(err, "ModelMesh's ConfigMap not found")
		} else {
			// If it's another error, return
			return ctrl.Result{}, err
		}
	}

	// Define a new Deployment object
	deploy := r.deploymentForTrustyAIService(instance)

	// Check if this Deployment already exists

	found := &appsv1.Deployment{}
	err = r.Get(context.TODO(), types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment doesn't exist - create it
			err = r.Create(context.TODO(), deploy)
			if err != nil {
				// Handle error
			}
		}
		// Handle error
	}

	// Fetch the AppService instance
	trustyAIServiceService := &trustyaiopendatahubiov1alpha1.TrustyAIService{}
	err = r.Get(ctx, req.NamespacedName, trustyAIServiceService)

	// Create service
	service, err := r.createService(trustyAIServiceService)
	if err != nil {
		// handle error
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, service); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Service already exists, no problem
			return ctrl.Result{}, nil
		}
		// handle error
		return ctrl.Result{}, err
	}

	// Service Monitor
	err = r.reconcileServiceMonitor(instance, ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Create route
	err = r.createRoute(instance, ctx)
	if err != nil {

	}

	// Deployment already exists - don't requeue
	return ctrl.Result{}, nil
}

// deploymentForTrustyAIService returns a Deployment object with the same name/namespace as the cr
func (r *TrustyAIServiceReconciler) deploymentForTrustyAIService(cr *trustyaiopendatahubiov1alpha1.TrustyAIService) *appsv1.Deployment {

	labels := getCommonLabels(cr.Name)

	containers := []corev1.Container{
		{
			Name:  "trustyai-service",
			Image: fmt.Sprintf("%s:%s", cr.Spec.Image, cr.Spec.Tag),
			Env: []corev1.EnvVar{
				{
					Name:  "STORAGE_DATA_FILENAME",
					Value: cr.Spec.Data.Filename,
				},
				{
					Name:  "SERVICE_STORAGE_FORMAT",
					Value: cr.Spec.Storage.Format,
				},
				{
					Name:  "STORAGE_DATA_FOLDER",
					Value: cr.Spec.Storage.Folder,
				},
				{
					Name:  "SERVICE_DATA_FORMAT",
					Value: cr.Spec.Data.Format,
				},
				{
					Name:  "SERVICE_METRICS_SCHEDULE",
					Value: cr.Spec.Metrics.Schedule,
				},
			},
			// rest of the container spec
		},
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Spec.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: cr.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: containers,
				},
			},
		},
	}
}

func (r *TrustyAIServiceReconciler) createService(cr *trustyaiopendatahubiov1alpha1.TrustyAIService) (*corev1.Service, error) {
	annotations := map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/path":   "/q/metrics",
		"prometheus.io/port":   "8080",
		"prometheus.io/scheme": "http",
	}
	labels := getCommonLabels(cr.Name)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.Name,
			Namespace:   cr.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
			},
			Selector: labels,
			Type:     corev1.ServiceTypeClusterIP,
		},
	}
	if err := ctrl.SetControllerReference(cr, service, r.Scheme); err != nil {
		return nil, err
	}
	return service, nil
}

func (r *TrustyAIServiceReconciler) createOrUpdateModelMeshConfigMap(trustyAIService *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "model-serving-config",
			Namespace: trustyAIService.Namespace,
		},
		Data: map[string]string{
			"config.yaml": "payloadProcessors: http://trustyai-service." + trustyAIService.Namespace + "/consumer/kserve/v2",
		},
	}

	if err := ctrl.SetControllerReference(trustyAIService, cm, r.Scheme); err != nil {
		return err
	}

	err := r.Create(ctx, cm)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// If ConfigMap already exists, update it anyway
			log.FromContext(ctx).Info("ModelMesh's ConfigMap already exists, re-applying")
			return r.Update(ctx, cm)
		}

		// Some other error, requeue
		return err
	}
	return nil
}

func (r *TrustyAIServiceReconciler) createRoute(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {

	labels := getCommonLabels(cr.Name)

	route := &routev1.Route{

		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: cr.Name,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "http",
				},
			},
			TLS: nil,
		},
	}

	// Use the client to create the route
	err := r.Client.Create(context.Background(), route)
	if err != nil {
		return err
	}
	return nil
}

func (r *TrustyAIServiceReconciler) reconcileServiceMonitor(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {

	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "trustyai-metrics",
			Namespace: cr.Namespace,
			Labels: map[string]string{
				"modelmesh-service": "modelmesh-serving",
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					Interval:        "4s",
					Path:            "/q/metrics",
					HonorLabels:     true,
					TargetPort:      &intstr.IntOrString{IntVal: 8080},
					Scheme:          "http",
					BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					//BearerTokenSecret: monitoringv1.SecretKeySelector {
					//	Key: ""
					//},
					Params: map[string][]string{
						"match[]": {
							`{__name__= "trustyai_spd"}`,
							`{__name__= "trustyai_dir"}`,
						},
					},
					MetricRelabelConfigs: []*monitoringv1.RelabelConfig{
						{
							Action:       "keep",
							Regex:        "trustyai_.*",
							SourceLabels: []monitoringv1.LabelName{"__name__"},
						},
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name": cr.Name,
				},
			},
		},
	}

	// Set AppService instance as the owner and controller
	ctrl.SetControllerReference(cr, serviceMonitor, r.Scheme)

	// Check if this ServiceMonitor already exists
	found := &monitoringv1.ServiceMonitor{}
	err := r.Get(ctx, types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating a new ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			err = r.Create(ctx, serviceMonitor)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrustyAIServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&trustyaiopendatahubiov1alpha1.TrustyAIService{}).
		Complete(r)
}
