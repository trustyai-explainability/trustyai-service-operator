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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultImage       = string("quay.io/trustyai/trustyai-service")
	defaultTag         = string("latest")
	defaultPvcName     = "trustyai-pvc"
	containerName      = "trustyai-service"
	serviceMonitorName = "trustyai-metrics"
)

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
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=list;watch;create
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=list;get;watch
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=list;get;watch;create;update;patch;delete

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

	// Ensure PVC
	err = r.ensurePVC(ctx, instance)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error creating PVC storage.")
		return ctrl.Result{}, err
	}

	// Ensure Deployment object
	deployment, err := r.ensureDeployment(ctx, instance)
	if err != nil {
		// handle error, potentially requeue request
		return ctrl.Result{}, err
	}

	// Fetch the TrustyAIService instance
	trustyAIServiceService := &trustyaiopendatahubiov1alpha1.TrustyAIService{}
	err = r.Get(ctx, req.NamespacedName, trustyAIServiceService)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not fetch service.")
		return ctrl.Result{}, err
	}

	// Create service
	service, err := r.reconcileService(trustyAIServiceService)
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
	serviceMonitor, err := r.reconcileServiceMonitor(instance, ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Create route
	route, err := r.reconcileRoute(instance, ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Add the TrustyAI instance as owner
	// Set TrustyAIService instance as the owner and controller
	if err := controllerutil.SetControllerReference(service, deployment, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := controllerutil.SetControllerReference(service, route, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := controllerutil.SetControllerReference(service, serviceMonitor, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	// Deployment already exists - don't requeue
	return ctrl.Result{}, nil
}

func (r *TrustyAIServiceReconciler) ensureDeployment(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) (*appsv1.Deployment, error) {
	deploy := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, deploy)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment does not exist, create it
			log.FromContext(ctx).Info("Could not find deployment.")
			return r.createDeployment(ctx, instance)
		}

		// Some other error occurred when trying to get the Deployment
		return nil, err
	}
	return deploy, nil
}

func (r *TrustyAIServiceReconciler) ensurePVC(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	pvc := &corev1.PersistentVolumeClaim{}

	err := r.Get(ctx, types.NamespacedName{Name: defaultPvcName, Namespace: instance.Namespace}, pvc)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("PVC not found. Creating.")
			// The PVC doesn't exist, so we need to create it
			return r.createPVC(ctx, instance)
		}
		return err
	}

	return nil
}

func (r *TrustyAIServiceReconciler) createPVC(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	storageClass := ""
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultPvcName,
			Namespace: instance.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			StorageClassName: &storageClass,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(instance.Spec.Storage.Size),
				},
			},
			VolumeName: instance.Spec.Storage.PV,
			VolumeMode: func() *corev1.PersistentVolumeMode {
				volumeMode := corev1.PersistentVolumeFilesystem
				return &volumeMode
			}(),
		},
	}

	if err := ctrl.SetControllerReference(instance, pvc, r.Scheme); err != nil {
		return err
	}

	return r.Create(ctx, pvc)
}

// reconcileDeployment returns a Deployment object with the same name/namespace as the cr
func (r *TrustyAIServiceReconciler) createDeployment(ctx context.Context, cr *trustyaiopendatahubiov1alpha1.TrustyAIService) (*appsv1.Deployment, error) {

	labels := getCommonLabels(cr.Name)

	if cr.Spec.Image == "" {
		cr.Spec.Image = defaultImage
	}
	if cr.Spec.Tag == "" {
		cr.Spec.Tag = defaultTag
	}

	replicas := int32(1)
	if cr.Spec.Replicas == nil {
		cr.Spec.Replicas = &replicas
	}

	containers := []corev1.Container{
		{
			Name:  containerName,
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

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: cr.Namespace,
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

	pvc := &corev1.PersistentVolumeClaim{}
	pvcerr := r.Get(ctx, types.NamespacedName{Name: defaultPvcName, Namespace: cr.Namespace}, pvc)
	if pvcerr != nil {
		log.FromContext(ctx).Error(pvcerr, "PVC not ready")
	}
	if pvcerr == nil && pvc.Status.Phase == corev1.ClaimBound {
		// The PVC is ready. We can now add it to the Deployment spec.
		deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "volume",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: defaultPvcName,
						ReadOnly:  false,
					},
				},
			},
		}
	}

	if err := ctrl.SetControllerReference(cr, deployment, r.Scheme); err != nil {
		return nil, err
	}

	err := r.Create(ctx, deployment)
	if err != nil {
		return nil, err
	}

	return deployment, nil

}

func (r *TrustyAIServiceReconciler) reconcileService(cr *trustyaiopendatahubiov1alpha1.TrustyAIService) (*corev1.Service, error) {
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

func (r *TrustyAIServiceReconciler) reconcileRoute(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) (*routev1.Route, error) {

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
	err := r.Client.Create(ctx, route)
	if err != nil {
		return nil, err
	}
	return route, nil
}

func (r *TrustyAIServiceReconciler) reconcileServiceMonitor(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) (*monitoringv1.ServiceMonitor, error) {

	serviceMonitor := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceMonitorName,
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
	err := ctrl.SetControllerReference(cr, serviceMonitor, r.Scheme)
	if err != nil {
		return nil, err
	}

	// Check if this ServiceMonitor already exists
	found := &monitoringv1.ServiceMonitor{}
	err = r.Get(ctx, types.NamespacedName{Name: serviceMonitor.Name, Namespace: serviceMonitor.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).Info("Creating a new ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			err = r.Create(ctx, serviceMonitor)
			if err != nil {
				log.FromContext(ctx).Error(err, "Not found ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
				return nil, err
			}
		} else {
			log.FromContext(ctx).Error(err, "Couldn't create new ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
			return nil, err
		}
	}

	return serviceMonitor, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrustyAIServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&trustyaiopendatahubiov1alpha1.TrustyAIService{}).
		Complete(r)
}
