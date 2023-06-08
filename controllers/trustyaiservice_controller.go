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
	goerrors "errors"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var ErrPVCNotReady = goerrors.New("PVC is not ready")

const (
	defaultImage         = string("quay.io/trustyai/trustyai-service")
	defaultTag           = string("latest")
	defaultPvcName       = "trustyai-pvc"
	containerName        = "trustyai-service"
	serviceMonitorName   = "trustyai-metrics"
	finalizerName        = "trustyai.opendatahub.io/finalizer"
	payloadProcessorName = "MM_PAYLOAD_PROCESSORS"
)

// TrustyAIServiceReconciler reconciles a TrustyAIService object
type TrustyAIServiceReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Namespace string
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
//+kubebuilder:rbac:groups=serving.kserve.io,resources=servingruntimes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=serving.kserve.io,resources=servingruntimes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;update;list;create

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
			// CR not found, it may have been deleted, so we'll remove the payload processor from the ModelMesh deployment
			err := updatePayloadProcessor(ctx, r.Client, "mlserver", payloadProcessorName, req.Name, req.Namespace, true)
			if err != nil {
				// handle error
			}
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Check if the CR is being deleted
	if instance.DeletionTimestamp != nil {
		// CR is being deleted
		if containsString(instance.Finalizers, finalizerName) {
			// The finalizer is present, so we handle external dependency deletion
			if err := r.deleteExternalDependency(req.Name, req.Namespace, ctx); err != nil {
				// If fail to delete the external dependency here, return with error
				// so that it can be retried
				log.FromContext(ctx).Error(err, "Failed to delete external dependencies.")
				return ctrl.Result{}, err
			}

			// Remove the finalizer from the list and update it.
			instance.Finalizers = removeString(instance.Finalizers, finalizerName)
			if err := r.Update(ctx, instance); err != nil {
				log.FromContext(ctx).Error(err, "Failed to remove the finalizer.")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add the finalizer if it does not exist
	if !containsString(instance.Finalizers, finalizerName) {
		instance.Finalizers = append(instance.Finalizers, finalizerName)
		if err := r.Update(context.Background(), instance); err != nil {
			log.FromContext(ctx).Error(err, "Failed to add the finalizer.")
			return ctrl.Result{}, err
		}
	}

	instance.Status.Ready = corev1.ConditionTrue

	// CR found, add or update the URL
	err = updatePayloadProcessor(ctx, r.Client, "mlserver", payloadProcessorName, instance.Name, instance.Namespace, false)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to update ModelMesh payload processor.")
		// handle error
	}

	// PV not found condition
	pvAvailableCondition := trustyaiopendatahubiov1alpha1.Condition{
		Type:    "PVAvailable",
		Status:  corev1.ConditionFalse,
		Reason:  "PVNotFound",
		Message: "PV not found",
	}

	// Update the instance status to Not Ready
	instance.Status.Phase = "Not Ready"
	instance.Status.Ready = corev1.ConditionFalse

	pv, err := r.ensurePV(ctx, instance)
	if err != nil {
		log.FromContext(ctx).Error(err, "Could not find requested PersistentVolume.")
	} else {
		// Set the conditions appropriately
		pvAvailableCondition.Status = corev1.ConditionTrue
		pvAvailableCondition.Reason = "PVFound"
		pvAvailableCondition.Message = "PersistentVolume found"
	}

	// Set the condition
	if err := r.setCondition(instance, pvAvailableCondition); err != nil {
		log.FromContext(ctx).Error(err, "Failed to set condition")
		return ctrl.Result{}, err
	}

	if err != nil {
		// If there was an error finding the PV, requeue the request
		return ctrl.Result{}, err
	}

	// PV not found condition
	pvcAvailableCondition := trustyaiopendatahubiov1alpha1.Condition{
		Type:    "PVCAvailable",
		Status:  corev1.ConditionFalse,
		Reason:  "PVCNotFound",
		Message: "PVC not found",
	}

	// Ensure PVC
	err = r.ensurePVC(ctx, instance, pv)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error creating PVC storage.")
	} else {
		// Set the conditions appropriately
		pvcAvailableCondition.Status = corev1.ConditionTrue
		pvcAvailableCondition.Reason = "PVCFound"
		pvcAvailableCondition.Message = "PersistentVolumeClaim found"
	}

	// Set the condition
	if err = r.setCondition(instance, pvcAvailableCondition); err != nil {
		log.FromContext(ctx).Error(err, "Failed to set condition")
		return ctrl.Result{}, err
	}

	if err != nil {
		// If there was an error finding the PVC, requeue the request
		return ctrl.Result{}, err
	}

	// Ensure Deployment object
	err = r.ensureDeployment(ctx, instance)
	if err != nil {
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
	err = r.reconcileServiceMonitor(instance, ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Create route
	err = r.reconcileRoute(instance, ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// At the end of reconcile, update the instance status to Ready
	instance.Status.Phase = "Ready"
	instance.Status.Ready = corev1.ConditionTrue

	// Populate statuses
	if err = r.reconcileStatuses(instance, ctx); err != nil {
		log.FromContext(ctx).Error(err, "Error creating the statuses.")
		return ctrl.Result{}, err
	}

	// Deployment already exists - don't requeue
	return ctrl.Result{}, nil
}

func (r *TrustyAIServiceReconciler) ensureDeployment(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {

	// Get image and tag from ConfigMap
	// If there's a ConfigMap with custom images, it is only applied when the operator is first deployed
	// Changing (or creating) the ConfigMap after the operator is deployed will not have any effect
	imageName, imageTag, err := r.getImageAndTagFromConfigMap(ctx)
	if err != nil {
		return err
	}

	deploy := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, deploy)
	if err != nil {
		if errors.IsNotFound(err) {
			// Deployment does not exist, create it
			log.FromContext(ctx).Info("Could not find deployment.")
			return r.createDeployment(ctx, instance, imageName, imageTag)
		}

		// Some other error occurred when trying to get the Deployment
		return err
	}

	// Deployment already exists
	pvc := &corev1.PersistentVolumeClaim{}

	err = r.Get(ctx, types.NamespacedName{Name: defaultPvcName, Namespace: instance.Namespace}, pvc)
	if err != nil {
		return err
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		// The PVC is not ready yet.
		return ErrPVCNotReady
	}

	// Check if the PVC is set in the Deployment
	volumeExists := false
	for _, v := range deploy.Spec.Template.Spec.Volumes {
		if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == defaultPvcName {
			volumeExists = true
			break
		}
	}

	if !volumeExists {
		// PVC is ready but not set in Deployment, so we'll update the Deployment to use the PVC
		volume := corev1.Volume{
			Name: defaultPvcName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: defaultPvcName,
				},
			},
		}
		deploy.Spec.Template.Spec.Volumes = append(deploy.Spec.Template.Spec.Volumes, volume)

		if err := r.Update(ctx, deploy); err != nil {
			return err
		}
	}

	// Deployment is ready and using the PVC
	return nil
}

// reconcileDeployment returns a Deployment object with the same name/namespace as the cr
func (r *TrustyAIServiceReconciler) createDeployment(ctx context.Context, cr *trustyaiopendatahubiov1alpha1.TrustyAIService, imageName string, imageTag string) error {

	labels := getCommonLabels(cr.Name)

	replicas := int32(1)
	if cr.Spec.Replicas == nil {
		cr.Spec.Replicas = &replicas
	}

	containers := []corev1.Container{
		{
			Name:  containerName,
			Image: fmt.Sprintf("%s:%s", imageName, imageTag),
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
		log.FromContext(ctx).Error(err, "Error setting TrustyAIService as owner of Deployment.")
		return err
	}

	err := r.Create(ctx, deployment)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error creating Deployment.")
		return err
	}

	return nil

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

func (r *TrustyAIServiceReconciler) reconcileRoute(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {

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

	if err := controllerutil.SetControllerReference(cr, route, r.Scheme); err != nil {
		log.FromContext(ctx).Error(err, "Error setting TrustyAIService as route owner.")
		return err
	}

	// Use the client to create the route
	err := r.Client.Create(ctx, route)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error creating Route.")
		return err
	}
	return nil
}

func (r *TrustyAIServiceReconciler) reconcileServiceMonitor(cr *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {

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

	// Set TrustyAIService instance as the owner and controller
	err := ctrl.SetControllerReference(cr, serviceMonitor, r.Scheme)
	if err != nil {
		return err
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
				return err
			}
		} else {
			log.FromContext(ctx).Error(err, "Couldn't create new ServiceMonitor", "ServiceMonitor.Namespace", serviceMonitor.Namespace, "ServiceMonitor.Name", serviceMonitor.Name)
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

func (r *TrustyAIServiceReconciler) reconcileStatuses(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, ctx context.Context) error {
	deploymentList := &appsv1.DeploymentList{}
	labelSelector := client.MatchingLabels{"app.kubernetes.io/name": "modelmesh-controller"}
	err := r.Client.List(ctx, deploymentList, client.InNamespace("modelmesh-serving"), labelSelector)

	var condition trustyaiopendatahubiov1alpha1.Condition
	if err != nil {
		log.FromContext(ctx).Error(err, "Error creating condition.")
		return err
	}

	if len(deploymentList.Items) == 0 {
		// No deployments found with the given label
		condition = trustyaiopendatahubiov1alpha1.Condition{
			Type:               "ModelMeshReady",
			Status:             "False",
			LastTransitionTime: metav1.Now(),
			Reason:             "ModelMeshNotPresent",
			Message:            "ModelMesh operator Deployment not found",
		}
	} else {
		// We'll just check the first deployment found
		deployment := &deploymentList.Items[0]
		// The Deployment exists, check if it's ready
		if isDeploymentReady(deployment) {
			condition = trustyaiopendatahubiov1alpha1.Condition{
				Type:               "ModelMeshReady",
				Status:             "True",
				LastTransitionTime: metav1.Now(),
				Reason:             "ModelMeshHealthy",
				Message:            "ModelMesh operator is running and healthy",
			}
		} else {
			condition = trustyaiopendatahubiov1alpha1.Condition{
				Type:               "ModelMeshReady",
				Status:             "False",
				LastTransitionTime: metav1.Now(),
				Reason:             "ModelMeshNotHealthy",
				Message:            "ModelMesh operator Deployment is not healthy",
			}
		}
	}

	// Update the condition
	instance.Status.Conditions = append(instance.Status.Conditions, condition)

	// Update the status of the custom resource
	err = r.Status().Update(ctx, instance)
	if err != nil {
		log.FromContext(ctx).Error(err, "Error updating conditions.")
		return err
	}

	return nil
}

// getTrustyAIImageAndTagFromConfigMap gets a custom TrustyAI image and tag from a ConfigMap in the operator's namespace
func (r *TrustyAIServiceReconciler) getImageAndTagFromConfigMap(ctx context.Context) (string, string, error) {
	if r.Namespace != "" {
		// Define the key for the ConfigMap
		configMapKey := types.NamespacedName{
			Namespace: r.Namespace,
			Name:      "trustyai-service-operator-config",
		}

		// Create an empty ConfigMap object
		var cm corev1.ConfigMap

		// Try to get the ConfigMap
		if err := r.Get(ctx, configMapKey, &cm); err != nil {
			if errors.IsNotFound(err) {
				// ConfigMap not found, fallback to default values
				return defaultImage, defaultTag, nil
			}
			// Other error occurred when trying to fetch the ConfigMap
			return defaultImage, defaultTag, fmt.Errorf("Error reading configmap %s", configMapKey)
		}

		// ConfigMap is found, extract the image and tag
		imageName, ok1 := cm.Data["trustyaiServiceImageName"]
		imageTag, ok2 := cm.Data["trustyaiServiceImageTag"]

		if !ok1 || !ok2 {
			// One or both of the keys are not present in the ConfigMap, return error
			return defaultImage, defaultTag, fmt.Errorf("configmap %s does not contain necessary keys", configMapKey)
		}

		// Return the image and tag
		return imageName, imageTag, nil
	} else {
		return defaultImage, defaultTag, nil
	}
}
