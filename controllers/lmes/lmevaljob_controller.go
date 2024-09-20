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

package lmes

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

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

	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	backendv1beta1 "github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/api/v1beta1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/driver"
)

var (
	pullPolicyMap = map[corev1.PullPolicy]corev1.PullPolicy{
		corev1.PullAlways:       corev1.PullAlways,
		corev1.PullNever:        corev1.PullNever,
		corev1.PullIfNotPresent: corev1.PullIfNotPresent,
	}

	optionKeys = map[string]string{
		"PodImage":             PodImageKey,
		"DriverImage":          DriverImageKey,
		"PodCheckingInterval":  PodCheckingIntervalKey,
		"ImagePullPolicy":      ImagePullPolicyKey,
		"GrpcPort":             GrpcPortKey,
		"GrpcService":          GrpcServiceKey,
		"GrpcServerSecret":     GrpcServerSecretKey,
		"GrpcClientSecret":     GrpcClientSecretKey,
		"DriverReportInterval": DriverReportIntervalKey,
		"DefaultBatchSize":     DefaultBatchSizeKey,
		"MaxBatchSize":         MaxBatchSizeKey,
		"DetectDevice":         DetectDeviceKey,
	}
)

type TLSMode int

const (
	TLSMode_None TLSMode = 0
	TLSMode_TLS  TLSMode = 1
	TLSMode_mTLS TLSMode = 2
)

// LMEvalJobReconciler reconciles a LMEvalJob object
type LMEvalJobReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	ConfigMap string
	Namespace string
	options   *ServiceOptions
}

type ServiceOptions struct {
	PodImage             string
	DriverImage          string
	DriverReportInterval time.Duration
	PodCheckingInterval  time.Duration
	ImagePullPolicy      corev1.PullPolicy
	GrpcPort             int
	GrpcService          string
	GrpcServerSecret     string
	GrpcClientSecret     string
	MaxBatchSize         int
	DefaultBatchSize     int
	DetectDevice         bool
	grpcTLSMode          TLSMode
}

func ControllerSetUp(mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error {
	return (&LMEvalJobReconciler{
		ConfigMap: configmap,
		Namespace: ns,
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("lm-eval-service-controller"),
	}).SetupWithManager(mgr)
}

// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=lmevaljobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=lmevaljobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=lmevaljobs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;watch;list
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;watch;list

func (r *LMEvalJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	job := &lmesv1alpha1.LMEvalJob{}
	if err := r.Get(ctx, req.NamespacedName, job); err != nil {
		log.Info("unable to fetch LMEvalJob. could be from a deletion request")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !job.ObjectMeta.DeletionTimestamp.IsZero() {
		// Handle deletion here
		return r.handleDeletion(ctx, job, log)
	}

	// Treat this as NewJobState
	if job.Status.LastScheduleTime == nil {
		job.Status.State = lmesv1alpha1.NewJobState
	}

	// Handle the job based on its state
	switch job.Status.State {
	case lmesv1alpha1.NewJobState:
		// Handle newly created job
		return r.handleNewCR(ctx, log, job)
	case lmesv1alpha1.ScheduledJobState:
		// the job's pod has been created and the driver hasn't updated the state yet
		// let's check the pod status and detect pod failure if there is
		// TODO: need a timeout/retry mechanism here to transite to other states
		return r.checkScheduledPod(ctx, log, job)
	case lmesv1alpha1.RunningJobState:
		// TODO: need a timeout/retry mechanism here to transite to other states
		return r.checkScheduledPod(ctx, log, job)
	case lmesv1alpha1.CompleteJobState:
		return r.handleComplete(ctx, log, job)
	case lmesv1alpha1.CancelledJobState:
		return r.handleCancel(ctx, log, job)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LMEvalJobReconciler) SetupWithManager(mgr ctrl.Manager) error {

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

		// ideally, this should be call in the main.go, but GRPC server depends on the
		// constructOptionsFromConfigMap to get the settings.
		go StartGrpcServer(ctx, r)

		return nil
	})); err != nil {
		return err
	}

	// watch the pods created by the controller but only for the deletion event
	return ctrl.NewControllerManagedBy(mgr).
		// since we register the finalizer, no need to monitor deletion events
		For(&lmesv1alpha1.LMEvalJob{}, builder.WithPredicates(predicate.Funcs{
			// drop deletion events
			DeleteFunc: func(event.DeleteEvent) bool {
				return false
			},
		})).
		Watches(
			&source.Kind{Type: &corev1.Pod{}},
			&handler.EnqueueRequestForOwner{
				OwnerType:    &lmesv1alpha1.LMEvalJob{},
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

func (r *LMEvalJobReconciler) checkSecrets(ctx context.Context) error {

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

func (r *LMEvalJobReconciler) updateStatus(ctx context.Context, newStatus *backendv1beta1.JobStatus) (err error) {
	log := log.FromContext(ctx)

	if strings.Trim(newStatus.GetJobName(), " ") == "" ||
		strings.Trim(newStatus.GetJobNamespace(), " ") == "" {

		return fmt.Errorf("JobName or JobNameSpace is empty")
	}

	job := &lmesv1alpha1.LMEvalJob{}
	if err = r.Get(ctx, types.NamespacedName{
		Namespace: newStatus.JobNamespace,
		Name:      newStatus.JobName,
	}, job); err != nil {
		log.Info("unable to fetch LMEvalJob")
		return err
	}

	newJobStatus := job.Status.DeepCopy()
	newJobStatus.State = lmesv1alpha1.JobState(newStatus.GetState())
	newJobStatus.Reason = lmesv1alpha1.Reason(newStatus.GetReason())

	if newStatus.GetStatusMessage() != "" {
		newJobStatus.Message = newStatus.GetStatusMessage()
	}
	if newStatus.Results != nil {
		newJobStatus.Results = newStatus.GetResults()
	}

	if !reflect.DeepEqual(job.Status, newJobStatus) {
		job.Status = *newJobStatus
		err = r.Status().Update(ctx, job)
		if err != nil {
			log.Error(err, "failed to update status")
		}
	}
	return err
}

func (r *LMEvalJobReconciler) constructOptionsFromConfigMap(
	ctx context.Context, configmap *corev1.ConfigMap) error {
	r.options = &ServiceOptions{
		DriverImage:          DefaultDriverImage,
		PodImage:             DefaultPodImage,
		DriverReportInterval: driver.DefaultDriverReportInterval,
		PodCheckingInterval:  DefaultPodCheckingInterval,
		ImagePullPolicy:      DefaultImagePullPolicy,
		GrpcPort:             DefaultGrpcPort,
		GrpcService:          DefaultGrpcService,
		GrpcServerSecret:     DefaultGrpcServerSecret,
		GrpcClientSecret:     DefaultGrpcClientSecret,
		MaxBatchSize:         DefaultMaxBatchSize,
		DetectDevice:         DefaultDetectDevice,
		DefaultBatchSize:     DefaultBatchSize,
	}

	log := log.FromContext(ctx)
	rv := reflect.ValueOf(r.options).Elem()
	var msgs []string

	for idx, cap := 0, rv.NumField(); idx < cap; idx++ {
		frv := rv.Field(idx)
		fname := rv.Type().Field(idx).Name
		configKey, ok := optionKeys[fname]
		if !ok {
			continue
		}

		if v, found := configmap.Data[configKey]; found {
			var err error
			switch frv.Type().Name() {
			case "string":
				frv.SetString(v)
			case "bool":
				val, err := strconv.ParseBool(v)
				if err != nil {
					val = DefaultDetectDevice
					msgs = append(msgs, fmt.Sprintf("invalid setting for %v: %v, use default setting instead", optionKeys[fname], val))
				}
				frv.SetBool(val)
			case "int":
				var intVal int
				intVal, err = strconv.Atoi(v)
				if err == nil {
					frv.SetInt(int64(intVal))
				}
			case "Duration":
				var d time.Duration
				d, err = time.ParseDuration(v)
				if err == nil {
					frv.Set(reflect.ValueOf(d))
				}
			case "PullPolicy":
				if p, found := pullPolicyMap[corev1.PullPolicy(v)]; found {
					frv.Set(reflect.ValueOf(p))
				} else {
					err = fmt.Errorf("invalid PullPolicy")
				}
			default:
				return fmt.Errorf("can not handle the config %v, type: %v", optionKeys[fname], frv.Type().Name())
			}

			if err != nil {
				msgs = append(msgs, fmt.Sprintf("invalid setting for %v: %v, use default setting instead", optionKeys[fname], v))
			}
		}
	}

	if len(msgs) > 0 {
		log.Error(fmt.Errorf("some settings in the configmap are invalid"), strings.Join(msgs, "\n"))
	}

	return nil
}

func (r *LMEvalJobReconciler) handleDeletion(ctx context.Context, job *lmesv1alpha1.LMEvalJob, log logr.Logger) (reconcile.Result, error) {
	if controllerutil.ContainsFinalizer(job, lmesv1alpha1.FinalizerName) {
		// delete the correspondling pod if needed
		// remove our finalizer from the list and update it.
		if job.Status.State != lmesv1alpha1.CompleteJobState ||
			job.Status.Reason != lmesv1alpha1.CancelledReason {

			if err := r.deleteJobPod(ctx, job); err != nil && client.IgnoreNotFound(err) != nil {
				log.Error(err, "failed to delete pod of the job")
			}
		}

		controllerutil.RemoveFinalizer(job, lmesv1alpha1.FinalizerName)
		if err := r.Update(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(job, "Normal", "DetachFinalizer",
			fmt.Sprintf("removed finalizer from LMEvalJob %s in namespace %s",
				job.Name,
				job.Namespace))
		log.Info("Successfully remove the finalizer", "name", job.Name)
	}

	return ctrl.Result{}, nil
}

func (r *LMEvalJobReconciler) handleNewCR(ctx context.Context, log logr.Logger, job *lmesv1alpha1.LMEvalJob) (reconcile.Result, error) {
	// If it doesn't contain our finalizer, add it
	if !controllerutil.ContainsFinalizer(job, lmesv1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(job, lmesv1alpha1.FinalizerName)
		if err := r.Update(ctx, job); err != nil {
			log.Error(err, "unable to update finalizer")
			return ctrl.Result{}, err
		}
		r.Recorder.Event(job, "Normal", "AttachFinalizer",
			fmt.Sprintf("added the finalizer to the LMEvalJob %s in namespace %s",
				job.Name,
				job.Namespace))
		// Since finalizers were updated. Need to fetch the new LMEvalJob
		// End the current reconsile and get revisioned job in next reconsile
		return ctrl.Result{}, nil
	}

	// construct a new pod and create a pod for the job
	currentTime := v1.Now()
	pod := r.createPod(job, log)
	if err := r.Create(ctx, pod, &client.CreateOptions{}); err != nil {
		// Failed to create the pod. Mark the status as complete with failed
		job.Status.State = lmesv1alpha1.CompleteJobState
		job.Status.Reason = lmesv1alpha1.FailedReason
		job.Status.Message = err.Error()
		if err := r.Status().Update(ctx, job); err != nil {
			log.Error(err, "unable to update LMEvalJob status for pod creation failure")
		}
		log.Error(err, "Failed to create pod for the LMEvalJob", "name", job.Name)
		return ctrl.Result{}, err
	}

	// Create the pod successfully. Wait for the driver to update the status
	job.Status.State = lmesv1alpha1.ScheduledJobState
	job.Status.PodName = pod.Name
	job.Status.LastScheduleTime = &currentTime
	if err := r.Status().Update(ctx, job); err != nil {
		log.Error(err, "unable to update LMEvalJob status (pod creation done)")
		return ctrl.Result{}, err
	}
	r.Recorder.Event(job, "Normal", "PodCreation",
		fmt.Sprintf("the LMEvalJob %s in namespace %s created a pod",
			job.Name,
			job.Namespace))
	log.Info("Successfully create a Pod for the Job")
	// Check the pod after the config interval
	return ctrl.Result{Requeue: true, RequeueAfter: r.options.PodCheckingInterval}, nil
}

func (r *LMEvalJobReconciler) checkScheduledPod(ctx context.Context, log logr.Logger, job *lmesv1alpha1.LMEvalJob) (ctrl.Result, error) {
	pod, err := r.getPod(ctx, job)
	if err != nil {
		// a weird state, someone delete the corresponding pod? mark this as CompleteJobState
		// with error message
		job.Status.State = lmesv1alpha1.CompleteJobState
		job.Status.Reason = lmesv1alpha1.FailedReason
		job.Status.Message = err.Error()
		if err := r.Status().Update(ctx, job); err != nil {
			log.Error(err, "unable to update LMEvalJob status", "state", job.Status.State)
			return ctrl.Result{}, err
		}
		r.Recorder.Event(job, "Warning", "PodMising",
			fmt.Sprintf("the pod for the LMEvalJob %s in namespace %s is gone",
				job.Name,
				job.Namespace))
		log.Error(err, "since the job's pod is gone, mark the job as complete with error result.")
		return ctrl.Result{}, err
	}

	if pod.Status.ContainerStatuses == nil {
		// wait for the pod to initialize and run the containers
		return ctrl.Result{Requeue: true, RequeueAfter: r.options.PodCheckingInterval}, nil
	}

	mainIndex := slices.IndexFunc(pod.Status.ContainerStatuses, func(s corev1.ContainerStatus) bool {
		return s.Name == "main"
	})

	if mainIndex == -1 || pod.Status.ContainerStatuses[mainIndex].State.Terminated == nil {
		// wait for the main container to finish
		return ctrl.Result{Requeue: true, RequeueAfter: r.options.PodCheckingInterval}, nil
	}

	// main container finished. update status
	job.Status.State = lmesv1alpha1.CompleteJobState
	if pod.Status.ContainerStatuses[mainIndex].State.Terminated.ExitCode == 0 {
		job.Status.Reason = lmesv1alpha1.SucceedReason
	} else {
		job.Status.Reason = lmesv1alpha1.FailedReason
		job.Status.Message = pod.Status.ContainerStatuses[mainIndex].State.Terminated.Reason
	}

	err = r.Status().Update(ctx, job)
	if err != nil {
		log.Error(err, "unable to update LMEvalJob status", "state", job.Status.State)
	}
	r.Recorder.Event(job, "Normal", "PodCompleted",
		fmt.Sprintf("The pod for the LMEvalJob %s in namespace %s has completed",
			job.Name,
			job.Namespace))
	return ctrl.Result{}, err
}

func (r *LMEvalJobReconciler) getPod(ctx context.Context, job *lmesv1alpha1.LMEvalJob) (*corev1.Pod, error) {
	var pod = corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: job.Namespace, Name: job.Name}, &pod); err != nil {
		return nil, err
	}
	for _, ref := range pod.OwnerReferences {
		if ref.APIVersion == job.APIVersion &&
			ref.Kind == job.Kind &&
			ref.Name == job.Name {

			return &pod, nil
		}
	}
	return nil, fmt.Errorf("pod doesn't have proper entry in the OwnerReferences")
}

func (r *LMEvalJobReconciler) deleteJobPod(ctx context.Context, job *lmesv1alpha1.LMEvalJob) error {
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
					APIVersion: job.APIVersion,
					Kind:       job.Kind,
					Name:       job.Name,
				},
			},
		},
	}
	return r.Delete(ctx, &pod, &client.DeleteOptions{})
}

func (r *LMEvalJobReconciler) handleComplete(ctx context.Context, log logr.Logger, job *lmesv1alpha1.LMEvalJob) (ctrl.Result, error) {
	if job.Status.CompleteTime == nil {
		r.Recorder.Event(job, "Normal", "JobCompleted",
			fmt.Sprintf("The LMEvalJob %s in namespace %s has completed",
				job.Name,
				job.Namespace))
		// TODO: final wrap up/clean up
		current := v1.Now()
		job.Status.CompleteTime = &current
		if err := r.Status().Update(ctx, job); err != nil {
			log.Error(err, "failed to update status for completion")
		}
	}
	return ctrl.Result{}, nil
}

func (r *LMEvalJobReconciler) handleCancel(ctx context.Context, log logr.Logger, job *lmesv1alpha1.LMEvalJob) (ctrl.Result, error) {
	// delete the pod and update the state to complete
	if _, err := r.getPod(ctx, job); err != nil {
		// pod is gone. update status
		job.Status.State = lmesv1alpha1.CompleteJobState
		job.Status.Reason = lmesv1alpha1.FailedReason
		job.Status.Message = err.Error()
	} else {
		job.Status.State = lmesv1alpha1.CompleteJobState
		job.Status.Reason = lmesv1alpha1.CancelledReason
		if err := r.deleteJobPod(ctx, job); err != nil {
			// leave the state as is and retry again
			log.Error(err, "failed to delete pod. scheduled a retry", "interval", r.options.PodCheckingInterval.String())
			return ctrl.Result{Requeue: true, RequeueAfter: r.options.PodCheckingInterval}, err
		}
	}

	err := r.Status().Update(ctx, job)
	if err != nil {
		log.Error(err, "failed to update status for cancellation")
	}
	r.Recorder.Event(job, "Normal", "Cancelled",
		fmt.Sprintf("The LMEvalJob %s in namespace %s has cancelled and changed its state to Complete",
			job.Name,
			job.Namespace))
	return ctrl.Result{}, err
}

func (r *LMEvalJobReconciler) createPod(job *lmesv1alpha1.LMEvalJob, log logr.Logger) *corev1.Pod {
	var allowPrivilegeEscalation = false
	var runAsNonRootUser = true
	var ownerRefController = true
	var runAsUser int64 = 1001030000

	var envVars = generateEnvs(job.Spec.EnvSecrets)

	var volumeMounts = []corev1.VolumeMount{
		{
			Name:      "shared",
			MountPath: "/opt/app-root/src/bin",
		},
	}

	var volumes = []corev1.Volume{
		{
			Name: "shared", VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	envVars, volumes, volumeMounts = r.patch4GrpcTLS(envVars, volumes, volumeMounts, r.options.grpcTLSMode)
	volumes, volumeMounts = patch4FileSecrets(volumes, volumeMounts, job.Spec.FileSecrets)

	// Then compose the Pod CR
	pod := corev1.Pod{
		TypeMeta: v1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      job.Name,
			Namespace: job.Namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: job.APIVersion,
					Kind:       job.Kind,
					Name:       job.Name,
					Controller: &ownerRefController,
					UID:        job.UID,
				},
			},
			Labels: map[string]string{
				"app.kubernetes.io/name": "lm-eval-service",
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:            "driver",
					Image:           r.options.DriverImage,
					ImagePullPolicy: r.options.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "main",
					Image:           r.options.PodImage,
					ImagePullPolicy: r.options.ImagePullPolicy,
					Env:             envVars,
					Command:         r.generateCmd(job),
					Args:            r.generateArgs(job, log),
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivilegeEscalation,
						RunAsUser:                &runAsUser,
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
				RunAsNonRoot: &runAsNonRootUser,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Volumes:       volumes,
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	return &pod
}

func (r *LMEvalJobReconciler) generateArgs(job *lmesv1alpha1.LMEvalJob, log logr.Logger) []string {
	if job == nil {
		return nil
	}

	cmds := make([]string, 0, 10)
	cmds = append(cmds, "python", "-m", "lm_eval", "--output_path", "/opt/app-root/src/output")
	// --model
	cmds = append(cmds, "--model", job.Spec.Model)
	// --model_args
	if job.Spec.ModelArgs != nil {
		cmds = append(cmds, "--model_args", argsToString(job.Spec.ModelArgs))
	}
	// --tasks
	cmds = append(cmds, "--tasks", strings.Join(concatTasks(job.Spec.TaskList), ","))
	// --include
	cmds = append(cmds, "--include_path", driver.DefaultTaskRecipesPath)
	// --num_fewshot
	if job.Spec.NumFewShot != nil {
		cmds = append(cmds, "--num_fewshot", fmt.Sprintf("%d", *job.Spec.NumFewShot))
	}
	// --limit
	if job.Spec.Limit != "" {
		cmds = append(cmds, "--limit", job.Spec.Limit)
	}
	// --gen_kwargs
	if job.Spec.GenArgs != nil {
		cmds = append(cmds, "--gen_kwargs", argsToString(job.Spec.GenArgs))
	}
	// --log_samples
	if job.Spec.LogSamples != nil && *job.Spec.LogSamples {
		cmds = append(cmds, "--log_samples")
	}
	// --batch_size
	var batchSize = r.options.DefaultBatchSize
	if job.Spec.BatchSize != nil && *job.Spec.BatchSize > 0 {
		batchSize = *job.Spec.BatchSize
	}
	// This could be done in the webhook if it's enabled.
	if batchSize > r.options.MaxBatchSize {
		batchSize = r.options.MaxBatchSize
		log.Info("batchSize is greater than max-batch-size of the controller's configuration, use the max-batch-size instead")
	}
	cmds = append(cmds, "--batch_size", fmt.Sprintf("%d", batchSize))

	return []string{"sh", "-ec", strings.Join(cmds, " ")}
}

func concatTasks(tasks lmesv1alpha1.TaskList) []string {
	if len(tasks.TaskRecipes) == 0 {
		return tasks.TaskNames
	}
	recipesName := make([]string, len(tasks.TaskRecipes))
	for i := range tasks.TaskRecipes {
		// assign internal userd task name
		recipesName[i] = fmt.Sprintf("%s_%d", driver.TaskRecipe_Prefix, i)
	}
	return append(tasks.TaskNames, recipesName...)
}

func (r *LMEvalJobReconciler) generateCmd(job *lmesv1alpha1.LMEvalJob) []string {
	if job == nil {
		return nil
	}
	cmds := []string{
		DestDriverPath,
		"--job-namespace", job.Namespace,
		"--job-name", job.Name,
		"--grpc-service", fmt.Sprintf("%s.%s.svc", r.options.GrpcService, r.Namespace),
		"--grpc-port", strconv.Itoa(r.options.GrpcPort),
		"--output-path", "/opt/app-root/src/output",
		"--report-interval", r.options.DriverReportInterval.String(),
	}

	if r.options.DetectDevice {
		cmds = append(cmds, "--detect-device")
	}

	for _, recipe := range job.Spec.TaskList.TaskRecipes {
		cmds = append(cmds, "--task-recipe", recipe.String())
	}

	cmds = append(cmds, "--")
	return cmds
}

func argsToString(args []lmesv1alpha1.Arg) string {
	if args == nil {
		return ""
	}
	var equalForms []string
	for _, arg := range args {
		equalForms = append(equalForms, fmt.Sprintf("%s=%s", arg.Name, arg.Value))
	}
	return strings.Join(equalForms, ",")
}

func generateEnvs(secrets []lmesv1alpha1.EnvSecret) []corev1.EnvVar {
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

func (r *LMEvalJobReconciler) patch4GrpcTLS(
	envVars []corev1.EnvVar,
	volumes []corev1.Volume,
	volumeMounts []corev1.VolumeMount,
	tlsMode TLSMode) ([]corev1.EnvVar, []corev1.Volume, []corev1.VolumeMount) {

	var secretMode int32 = 420

	if tlsMode == TLSMode_mTLS {
		envVars = append(envVars,
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
	} else if tlsMode == TLSMode_TLS {
		envVars = append(envVars,
			corev1.EnvVar{
				Name:  driver.GrpcServerCaEnv,
				Value: "/tmp/k8s-grpc-server/certs/ca.crt",
			},
		)

		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "server-cert",
				MountPath: "/tmp/k8s-grpc-server/certs",
			},
		)

		volumes = append(volumes,
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
