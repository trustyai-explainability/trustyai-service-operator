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

package guardrails

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/docker/docker/api/types/volume"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
)

var (
	optionKeys = map[string]string{
		"OrchestratorImage":    OrchestratorImageKey,
		"ImagePullPolicy":      ImagePullPolicyKey,
		"GrpcPort":             GrpcPortKey,
		"GrpcService":          GrpcServiceKey,
		"GrpcServerSecret":     GrpcServerSecretKey,
		"GrpcClientSecret":     GrpcClientSecretKey,
	}
)

type TLSMode int

const (
	TLSMode_None TLSMode = 0
	TLSMode_TLS  TLSMode = 1
	TLSMode_mTLS TLSMode = 2
)

// GuardrailsService reconciles a GuardrailsService object
type GuardrailsServiceReconciler struct {
	client.Client
	Scheme 		  *runtime.Scheme
	EventRecorder record.EventRecorder
	ConfigMap	  string
	Namespace     string
	options       *ServiceOptions
}

type ServiceOptions struct {
	PodImage             string
	ImagePullPolicy      corev1.PullPolicy
	GrpcPort             int
	GrpcService          string
	GrpcServerSecret     string
	GrpcClientSecret     string
	grpcTLSMode          TLSMode
}

func ControllerSetUp(mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error {
	return (&GuardrailsServiceReconciler{
		ConfigMap:     configmap,
		Namespace:     ns,
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		EventRecorder: recorder,
	}).SetupWithManager(mgr)
}

// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=guardrailsservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=guardrailsservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=guardrailsservices/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;watch;list
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;watch;list

func (r *GuardrailsServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	service := &guardrailsv1alpha1.GuardrailsService{}
	if err := r.Get(ctx, req.NamespacedName, service); err != nil {
		log.Info("unable to fetch GuardrailsService. could be from a deletion request")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !service.ObjectMeta.DeletionTimestamp.IsZero() {
		// Handle deletion here
		return r.handleDeletion(ctx, service, log)
	}

	// Treat this as NewserviceState
	if service.Status.LastScheduleTime == nil {
		service.Status.State = guardrailsv1alpha1.NewServiceState
	}

	// Handle the service based on its state
	switch service.Status.State {
	case guardrailsv1alpha1.NewServiceState:
		// Handle newly created service
		return r.handleNewCR(ctx, log, service)
	case guardrailsv1alpha1.ScheduledServiceState:
		// the service's pod has been created
		// let's check the pod status and detect pod failure if there is
		return r.checkScheduledPod(ctx, log, service)
	case guardrailsv1alpha1.RunningServiceState:
		// TODO: need a timeout/retry mechanism here to transite to other states
		return r.checkScheduledPod(ctx, log, service)
	case guardrailsv1alpha1.CompleteServiceState:
		return r.handleComplete(ctx, log, service)
	case guardrailsv1alpha1.CancelledserviceState:
		return r.handleCancel(ctx, log, service)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GuardrailsServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Add a runnable to retrieve the settings from the specified configmap
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		var cm corev1.ConfigMap
		if err := r.Get(
			ctx,
			types.NamespacedName{Namespace: r.Namespace, Name: r.ConfigMap},
			&cm); err != nil {

			ctrl.Log.WithName("setup").Error(err,
				"failed to get configmap",
				"namespace", r.Namespace,
				"name", r.ConfigMap)

			return err
		}

		if err := r.constructOptionsFromConfigMap(ctx, &cm); err != nil {
			return err
		}

		if err := r.checkSecrets(ctx); err != nil {
			// if mTLS/TLS is not enable, then we are good. Otherwise error out
			if viper.IsSet(GrpcServerCertEnv) ||
				viper.IsSet(GrpcServerKeyEnv) ||
				viper.IsSet(GrpcClientCaEnv) {
				return fmt.Errorf("TLS or mTLS is enabled for GRPC server but secrets don't exist")
			}
		}

		go StartGrpcServer(ctx, r)

		return nil
	})); err != nil {
		return err
	}

	// watch the pods created by the controller but only for the deletion event
	return ctrl.NewControllerManagedBy(mgr).
		// since we register the finalizer, no need to monitor deletion events
		For(&guardrailsv1alpha1.GuardrailsService{}, builder.WithPredicates(predicate.Funcs{
			// drop deletion events
			DeleteFunc: func(event.DeleteEvent) bool {
				return false
			},
		})).
		Watches(
			&source.Kind{Type: &corev1.Pod{}},
			&handler.EnqueueRequestForOwner{
				OwnerType:    &guardrailsv1alpha1.GuardrailsService{},
				IsController: true,
			},
			builder.WithPredicates(predicate.Funcs{
				// drop all events except deletion
				CreateFunc: func(event.CreateEvent) bool {
					return false
				},
				UpdateFunc: func(event.UpdateEvent) bool {
					return false
				},
				GenericFunc: func(event.GenericEvent) bool {
					return false
				},
			}),
		).
		Complete(r)
}

func (r *GuardrailsServiceReconciler) checkSecrets(ctx context.Context) error {
	var isSecretExists = func(name string) bool {
		var secret corev1.Secret
		err := r.Get(ctx, types.NamespacedName{Namespace: r.Namespace, Name: name}, &secret)
		return err == nil
	}

	if !isSecretExists(r.options.GrpcServerSecret) {
		return fmt.Errorf("secret %s not found", r.options.GrpcServerSecret)
	}
	if !isSecretExists(r.options.GrpcClientSecret) {
		return fmt.Errorf("secret %s not found", r.options.GrpcServerSecret)
	}
	return nil
}

// TO-DO
func (r *GuardrailsServiceReconciler) updateStatus(ctx context.Context, newStatus *) (err error) {
	log := log.FromContext(ctx)

	if strings.Trim(newStatus.GetServiceName(), )
	return err
}


func (r *GuardrailsServiceReconciler) constructOptionsFromConfigMap(
	ctx context.Context, configmap *corev1.ConfigMap) error {
	r.options = &ServiceOptions{
		PodImage:	  DefualtPodImage,
		ImagePullPolicy:      DefaultImagePullPolicy,
		GrpcPort:             DefaultGrpcPort,
		GrpcService:          DefaultGrpcService,
		GrpcServerSecret:     DefaultGrpcServerSecret,
		GrpcClientSecret:     DefaultGrpcClientSecret,
	}
	log := log.FromContext(ctx)
	rv := reflect.ValueOf(r.options).Elem()
	var msgs []string

	if len(msgs) > 0 {
		log.Error(fmt.Errorf("some settings in the configmap are invalid"), strings.Join(msgs, "\n"))
	}

	return nil
}

func (r *GuardrailsServiceReconciler) handleDeletion(ctx context.Context, service *guardrailsv1alpha1.Guardrails, log logr.Logger) (reconcile.Result, error) {
	// check if the service contains finalizer
	if controllerutil.ContainsFinalizer(service, guardrailsv1alpha1.FinalizerName) {
		// check for incomplete or cancelled service
		if service.Status.State != guardrailsv1alpha1.CompleteServiceState ||
			service.Status.Reason != guardrailsv1alpha1.CancelledReason {
			// check for pod deletion error
			if err := r.deleteservicePod(ctx, service); err != nil && client.IgnoreNotFound(err) != nil {
				log.Error(err, "failed to delete pod")
			}
		}

		// remove finalizer
		controllerutil.RemoveFinalizer(service, guardrailsv1alpha1.FinalizerName)
		if err := r.Update(ctx, service); err != nil {
			return ctrl.Result{}, err
		}
		r.EventRecorder.Event(service, "Normal", "DetachFinalizer",
			fmt.Sprintf("Removed finalizer from GuardrailsService %s in namespace %s",
				service.Name,
				service.Namespace))
		log.Info("Sucessfully remove the finalizer", "name", service.Name)
	}
	return ctrl.Result{}, nil
}

func (r *GuardrailsServiceReconciler) handleNewCR(ctx context.Context, log logr.Logger, service *guardrailsv1alpha1.Guardrails) (reconcile.Result, error) {
	// check if the service contains our finalizer, if not, add it
	if controllerutil.ContainsFinalizer(service, guardrailsv1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(service, guardrailsv1alpha1.FinalizerName)
		// check for finalizer update errors
		if err := r.Update(ctx, service); err != nil {
			log.Error(err, "Unable to update finalizer")
			return ctrl.Result{}, err
		}
		r.EventRecorder.Event(service, "Normal", "AttachFinalizer",
			fmt.Sprintf("Added the finalizer to the Guardrails %s in namespace %s",
				service.Name,
				service.Namespace))
		return ctrl.Result{}, nil
	}
	// contruct a new pod and create a pod for the service
	currentTime := v1.Now()
	pod := r.createPod(service, log)
	// check for pod creation errors
	if err := r.Create(ctx, pod, &client.CreateOptions{}); err != nil {
		service.Status.State = guardrailsv1alpha1.CompleteServiceState
		service.Status.Reason = guardrailsv1alpha1.FailedReason
		service.Status.Message = err.Error()
		if err := r.Status().Update(ctx, service); err != nil {
			log.Error(err, "Unable to update GuardrailsService status for pod creation failure")
		}
		log.Error(err, "Failed to create GuardrailsService pod")
		return ctrl.Result{}, nil
	}
	// if pod creation is successful
	service.Status.Status = guardrailsv1alpha1.ScheduledServiceState
	service.Status.PodName = pod.BatcherConfigMapKeyName
	service.Status.LastScheduleTime = &currentTime
	if err := r.Status().Update(ctx, service); err != nil {
		log.Error(err, "Unable to update GuardrailsService status after pod creation")
		return ctrl.Result{}, nil
	}
	r.EventRecorder.Event(service, "Normal", "PodCompleted",
		fmt.Sprintf("The pod for the GuardrailsService %s in namespace %s has completed",
			service.Name,
			service.Namespace))
	log.Info("Succesfully created a Pod for the GuardrailsService")
	// check the pod after the config interval
	return ctrl.Result{Requeue: true, RequeueAfter: r.options.PodCheckingInterval}, nil
}

func (r *GuardrailsServiceReconciler) checkScheduledPod(ctx context.Context, log logr.Logger, service *guardrailsv1alpha1.Guardrails) (ctrl.Result, error) {
	pod, err := r.getPod(ctx, service)
	if err != nil {
		service.Status.State = guardrailsv1alpha1.CompleteServiceState
		service.Status.Reason = guardrailsv1alpha1.FailedReason
		service.Status.Message = err.Error()
		if err := r.Status().Update(ctx, service); err != nil {
			log.Error(err, "Unaled to update GuardrailsService", "state", service.Status.State)
			return ctrl.Result{}, err
		}
		r.EventRecorder.Event(service, "Warning", "PodMissing",
			fmt.Sprintf("The pod for GuardrailsService %s in namespace %s is missing",
				service.Name,
				service.Namespace))
		log.Error(err, "Since the pod is missing, mark the GuardrailsService as complete with error.")
		return ctrl.Result{}, err
	}

	if pod.Status.ContainerStatuses == nil {
		// wait for the pod to initialize and run the containers
		return ctrl.Result{Requeue: true, RequeueAfter: r.options.PodCheckingInterval}, nil
	}

	// TO DO: Check container status

	err = r.Status().Update(ctx, service)
	if err != nil {
		log.Error(err, "Unable to update GuardrailsService status", "state", service.Status.State)
	}
	r.EventRecorder.Event(service, "Normal", "PodCompleted",
		fmt.Sprintf("The pod for GuardrailsService %s in namespace %s is complete",
			service.Name,
			service.Namespace))
	return ctrl.Result{}, err
}

func (r *GuardrailsServiceReconciler) getPod(ctx context.Context, log logr.Logger) *corev1.Pod {
	var allowPrivilegeEscalation = false
	var runAsNonRootUser = true
	var ownerRefController = true
	var runAsUser int64 = 1001030000
	// compose the pod CR
	pod := corev1.Pod{
		TypeMeta: v1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
	}
	return &pod
}

func (r *GuardrailsServiceReconciler) deletePod(ctx context.Context, service *guardrailsv1alpha1.Guardrails) error {
	pod := corev1.Pod{
		TypeMeta: v1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      job.Status.PodName,
			Namespace: job.Namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: service.APIVersion,
					Kind:       service.Kind,
					Name:       service.Name,
					Controller:
				},
			},
		},
	}
	return r.Delete(ctx, &pod, &client.DeleteOptions{})
}

func (r *&GuardrailsServiceReconciler) handleCancel(ctx context.Context, log logr.Logger, service *guardrailsv1alpha1.GuardrailsService) (ctrl.Result, error) {
	// delete the pod and update its state to Complete
	if _, err := r.getPod(ctx, service); err != nil {
		// pod is deleted so update its state to Complete
		service.Status.State = guardrailsv1alpha1.CompleteServiceState
		service.Status.Reason = guardrailsv1alpha1.FailedReason
		service.Status.Message = err.Error()
	} else {
		service.Status.State = guardrailsv1alphav1.CompleteServiceState
		service.Status.Reason = guardrailsv1alpha.CancelledReason
		if err := r.deleteservicePod(ctx, service); err != nil {
			log.Error(err, "Failed to delete pod. Retry scheduled", "interval", r.options.PodCheckingInterval.String())
			return ctrl.Result{Requeue: true, RequeueAfter: r.options.PodCheckingInterval}, err
		}
	}
	err := r.Status().Update(ctx, service)
	if err != nil {
		log.Error(err, "Failed to update status for cancellation.")
	}
	r.EventRecorder.Event(service, "Normal", "Cancelled"
		fmt.Sprintf("The GuardrailsService %s in namespace %s has been cancelled and its state is now Complete",
			service.Name,
			service.Namespace))
	return ctrl.Result{}, err
}

// TO-DO: Need to figure out what resources the Pod needs
func (r *&GuardrailsServiceReconciler) createPod(service * guardrailsv1alpha1.GuardrailsService, log logr.Logger) *corev1.Pod {
	var allowPrivilegeEscalation = false
	var runAsNonRootUser = true
	var ownerRefController = true
	var runAsUser int64 = 1001030000
	var privileged = false
	var runAsNonRoot = true
	var ReadOnlyRootFilesystem = true
	var envVars = generateEnvs(service.Spec.EnvSecrets)

	var volumeMounts = []corev1.VolumeMount{
		{
			Name: "fms-orchestr8-config-nlp",
			MountPath: "/config/config.yaml",
			SubPath: "config.yaml",
		},
		// is it the kube-api-access vol mount required ?
	}

	var volumes = []corev1.Volume{
		{
			Name: "fms-orchestr8-config-nlp", VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		// is it the kube-api-access vol required ?
	}

	envVars, volumes, volumeMounts = r.patch4GrpcTLS(ennVars, volumes, volumeMounts, r.options.grpcTLSMode)
	volumes, volumeMounts = patch4FileSecrets(volumes, volumeMounts, service.Spec.FileSecrets)

	// Compose the Pod CR
	pod := corev1.Pod{
		TypeMeta: v1.TypeMeta{
			Kind: "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name: service.Name,
			Namespace: service.Namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: service.APIVersion,
					Kind: service.Kind,
					Name: service.Name,
					Controller: &ownerRefController,
					UID: service.UID,

				}
			},
			Labels: map[string]string{
				"app": "fmstack-nlp",
				"component": "fms-orchestr8-nlp",
				"deployment": "fms-orchestr8-nlp",

			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "fms-orchestr8-nlp",
					Image: r.options.PodImage,
					ImagePullPolicy: r.options.ImagePullPolicy,
					Env: envVars,
					Command: "-/app/bin/fms-guardrails-orchestr8",
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
						Privileged: &privileged,
						ReadOnlyRootFilesystem: &readOnlyFileSystem,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					VolumeMounts: volumeMounts,
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRoot,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Volumes: volumes,
			RestartPolicy: corev1.RestartPolicyAlways,
		}
	}
	return &pod
}

func argsToString(args []guardrailsv1alpha1.Arg) string {
	if args == nil {
		return ""
	}
	var equalForms []string
	for _, arg := range args {
		equalForms = append(equalForms, fmt.Sprintf("%s=%s", arg.Name, arg.Value))
	}
	return strings.Join(equalForms, ",")
}

func generateEnvs(secrets []guardrailsv1alpha1.EnvSecret) []corev1.EnvVar {
	var envs = []corev1.EnvVar{}
	for _, secret := range secrets {
		if secret.Secret != nil {
			envs = append(envs, corev1.EnvVar{
				Name:  secret.Env,
				Value: *secret.Secret,
			})
		} else if secret.SecretRef != nil {
			envs = append(envs, corev1.EnvVar{
				Name: secret.Env,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: secret.SecretRef,
				},
			})
		}
	}
	return envs
}

func (r *GuardrailsServiceReconciler) patch4GrpcTLS(
	envVars []corev1.EnvVar,
	volumes []corev1.Volume,
	volumeMounts []corev1.VolumeMount,
	tlsMode TLSMode) ([]corev1.EnvVar, []corev1.Volume, []corev1.VolumeMount) {

	var secretMode int32 = 420

	if tlsMode == TLSMode_mTLS {
		envVars = append(
			corev1.EnvVar{
				Name:  driver.GrpcClientKeyEnv,
				Value: "/tmp/k8s-grpc-client/certs/tls.key",
			},
			corev1.EnvVar{
				Name:  driver.GrpcClientCertEnv,
				Value: "/tmp/k8s-grpc-client/certs/tls.crt",
			},
			corev1.EnvVar{
				Name:  driver.GrpcServerCaEnv,
				Value: "/tmp/k8s-grpc-server/certs/ca.crt",
			},
		)

		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "client-cert",
				MountPath: "/tmp/k8s-grpc-client/certs",
				ReadOnly:  true,
			},
			corev1.VolumeMount{
				Name:      "server-cert",
				MountPath: "/tmp/k8s-grpc-server/certs",
				ReadOnly:  true,
			},
		)

		volumes = append(volumes,
			corev1.Volume{
				Name: "client-cert",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  r.options.GrpcClientSecret,
						DefaultMode: &secretMode,
					},
				},
			},
			corev1.Volume{
				Name: "server-cert",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  r.options.GrpcServerSecret,
						DefaultMode: &secretMode,
						Items: []corev1.KeyToPath{
							{Key: "ca.crt", Path: "ca.crt"},
						},
					},
				},
			},
		)
	} else if tlsMode == TLSMode {
		envVars = append(envVars,
			corev1.Volume
		)
	}

	return envVars, volumes, volumeMounts
}

func patch4FileSecrets(
	volumes []corev1.Volume,
	volumeMounts []corev1.VolumeMount,
	secrets []lmesv1alpha1.FileSecret) ([]corev1.Volume, []corev1.VolumeMount) {

	var counter = 1
	for _, secret := range secrets {
		volumes = append(volumes, corev1.Volume{
			Name: fmt.Sprintf("secVol%d", counter),
			VolumeSource: corev1.VolumeSource{
				Secret: &secret.SecretRef,
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      fmt.Sprintf("secVol%d", counter),
			MountPath: secret.MountPath,
			ReadOnly:  true,
		})
		counter++
	}
	return volumes, volumeMounts
}