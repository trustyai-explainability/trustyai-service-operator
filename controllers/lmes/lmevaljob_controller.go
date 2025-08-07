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
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/trustyai-explainability/trustyai-service-operator/controllers/metrics"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/remotecommand"
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

	"github.com/go-logr/logr"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/driver"
)

var (
	pullPolicyMap = map[corev1.PullPolicy]corev1.PullPolicy{
		corev1.PullAlways:       corev1.PullAlways,
		corev1.PullNever:        corev1.PullNever,
		corev1.PullIfNotPresent: corev1.PullIfNotPresent,
	}

	optionKeys = map[string]string{
		"PodImage":            PodImageKey,
		"DriverImage":         DriverImageKey,
		"PodCheckingInterval": PodCheckingIntervalKey,
		"ImagePullPolicy":     ImagePullPolicyKey,
		"DefaultBatchSize":    DefaultBatchSizeKey,
		"MaxBatchSize":        MaxBatchSizeKey,
		"DetectDevice":        DetectDeviceKey,
		"AllowOnline":         AllowOnline,
		"AllowCodeExecution":  AllowCodeExecution,
		"DriverPort":          DriverPort,
	}

	labelFilterPrefixes       = []string{}
	annotationFilterPrefixes  = []string{}
	allowPrivilegeEscalation  = false
	runAsNonRootUser          = true
	ownerRefController        = true
	defaultPodSecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: &runAsNonRootUser,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	defaultSecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				"ALL",
			},
		},
	}
)

// maintain a list of key-time pair data.
// provide a function to add the key and update the time
// atomically and return a reconcile requeue event
// if needed.
type syncedMap4Reconciler struct {
	data  map[string]time.Time
	mutex sync.Mutex
}

// LMEvalJobReconciler reconciles a LMEvalJob object
type LMEvalJobReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	ConfigMap   string
	Namespace   string
	restConfig  *rest.Config
	restClient  rest.Interface
	pullingJobs *syncedMap4Reconciler
}

// The registered function to set up LMES controller
func ControllerSetUp(mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error {
	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	return (&LMEvalJobReconciler{
		ConfigMap:   configmap,
		Namespace:   ns,
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		Recorder:    mgr.GetEventRecorderFor("lm-eval-service-controller"),
		restConfig:  mgr.GetConfig(),
		restClient:  clientset.CoreV1().RESTClient(),
		pullingJobs: newSyncedMap4Reconciler(),
	}).SetupWithManager(mgr)
}

func newSyncedMap4Reconciler() *syncedMap4Reconciler {
	return &syncedMap4Reconciler{data: make(map[string]time.Time)}
}

// check if the paired time of the key is passed. if yes, update the time and
// return a requeue result. otherwise an empty result
func (q *syncedMap4Reconciler) addOrUpdate(key string, after time.Duration) reconcile.Result {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	v, ok := q.data[key]
	if ok && time.Now().Before(v) {
		// no need to requeue since there is an existing one
		return reconcile.Result{}
	}
	value := time.Now().Add(after)
	q.data[key] = value
	return reconcile.Result{Requeue: true, RequeueAfter: after}
}

// remove the key from the list
func (q *syncedMap4Reconciler) remove(key string) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	delete(q.data, key)
}

// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=lmevaljobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=lmevaljobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=lmevaljobs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;watch;list
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;watch;list
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=list;get;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=list;get;watch;create;update;patch;delete

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
	if job.Status.LastScheduleTime == nil && job.Status.CompleteTime == nil {
		job.Status.State = lmesv1alpha1.NewJobState
	}

	if JobMgrEnabled && job.Status.State != lmesv1alpha1.CompleteJobState {
		//the job requires kueue.x-k8s.io/queue-name label if Job Manager is enabled
		if _, ok := job.ObjectMeta.GetLabels()["kueue.x-k8s.io/queue-name"]; !ok {
			job.Status.State = lmesv1alpha1.CompleteJobState
			job.Status.Reason = lmesv1alpha1.FailedReason
			job.Status.Message = "job requires kueue.x-k8s.io/queue-name label"
			log.Error(fmt.Errorf("job %s requires kueue.x-k8s.io/queue-name label", job.Name), "LMevalJob requires kueue.x-k8s.io/queue-name label when Job Manager is enabled")
			return r.handleComplete(ctx, log, job)
		} else if job.Spec.Suspend {
			return r.handleSuspend(ctx, log, job)
		}
	}

	// If outputs have been explicitly set
	if job.Spec.HasCustomOutput() {
		// If managed PVC is set
		if job.Spec.Outputs.HasManagedPVC() {
			if job.Spec.Outputs.HasExistingPVC() {
				log.Info("LMEvalJob has both managed and existing PVCs defined. Existing PVC configuration will be ignored.")
			}
			err := r.handleManagedPVC(ctx, log, job)
			if err != nil {
				return ctrl.Result{}, err
			}
		} else if job.Spec.Outputs.HasExistingPVC() {
			err := r.handleExistingPVC(ctx, log, job)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	log.Info("Checking the job state")

	// Handle the job based on its state
	switch job.Status.State {
	case lmesv1alpha1.NewJobState:
		// Handle newly created job
		return r.handleNewCR(ctx, log, job)
	case lmesv1alpha1.ScheduledJobState:
		// the job's pod has been created and the driver hasn't updated the state yet
		// let's check the pod status and detect pod failure if there is
		// TODO: need a timeout/retry mechanism here to transit to other states
		return r.checkScheduledPod(ctx, log, job)
	case lmesv1alpha1.RunningJobState:
		// TODO: need a timeout/retry mechanism here to transit to other states
		return r.checkScheduledPod(ctx, log, job)
	case lmesv1alpha1.CompleteJobState:
		return r.handleComplete(ctx, log, job)
	case lmesv1alpha1.CancelledJobState:
		return r.handleCancel(ctx, log, job)
	case lmesv1alpha1.SuspendedJobState:
		if !job.Spec.Suspend {
			return r.handleResume(ctx, log, job)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LMEvalJobReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Add a runnable to retrieve the settings from the specified configmap
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		var cm corev1.ConfigMap
		log := log.FromContext(ctx)
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
		if err := constructOptionsFromConfigMap(&log, &cm); err != nil {
			return err
		}

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
			&corev1.Pod{},
			handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetClient().RESTMapper(), &lmesv1alpha1.LMEvalJob{}, handler.OnlyControllerOwner()),
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

func (r *LMEvalJobReconciler) updateStatus(ctx context.Context, log logr.Logger, job *lmesv1alpha1.LMEvalJob) error {
	stdin, _, err := r.remoteCommand(ctx, job, fmt.Sprintf("%s %s", DestDriverPath, "--get-status"))
	if err != nil {
		return err
	}
	newStatus := lmesv1alpha1.LMEvalJobStatus{}
	if err = json.Unmarshal(stdin, &newStatus); err != nil {
		return err
	}

	// Check if the pod is ready before accepting a Complete status from the driver
	if newStatus.State == lmesv1alpha1.CompleteJobState {
		pod, err := r.getPod(ctx, job)
		if err != nil {
			log.Error(err, "unable to get pod to verify completion status")
			return err
		}

		if mainIdx := getContainerByName(&pod.Status, "main"); mainIdx == -1 {
			// Main container not found, pod still initialising
			log.Info("ignoring Complete status from driver - pod still initialising", "podName", job.GetPodName())
			return nil
		} else if pod.Status.ContainerStatuses[mainIdx].State.Running == nil {
			// Main container not running, pod still initialising
			log.Info("ignoring Complete status from driver - pod not running yet", "podName", job.GetPodName())
			return nil
		}
	}

	// driver only provides updates for these fields
	// only update is progress bar percent-complete or message has varied
	if newStatus.State != job.Status.State ||
		newStatus.Message != job.Status.Message ||
		newStatus.Reason != job.Status.Reason ||
		newStatus.Results != job.Status.Results ||
		!utils.ProgressArrayTriggeredChange(newStatus.ProgressBars, job.Status.ProgressBars) {

		job.Status.State = newStatus.State
		job.Status.Message = newStatus.Message
		job.Status.Reason = newStatus.Reason
		job.Status.Results = newStatus.Results
		job.Status.ProgressBars = newStatus.ProgressBars

		err = r.Status().Update(ctx, job)
		if err != nil {
			log.Error(err, "failed to update status")
		}
	}
	return err
}

func (r *LMEvalJobReconciler) shutdownDriver(ctx context.Context, job *lmesv1alpha1.LMEvalJob) error {
	_, _, err := r.remoteCommand(ctx, job, fmt.Sprintf("%s %s", DestDriverPath, "--shutdown"))
	return err
}

func (r *LMEvalJobReconciler) remoteCommand(ctx context.Context, job *lmesv1alpha1.LMEvalJob, command string) ([]byte, []byte, error) {
	request := r.restClient.Post().
		Namespace(job.GetNamespace()).
		Resource("pods").
		Name(job.GetName()).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   []string{"/bin/sh", "-c", command},
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			Container: "main",
		}, scheme.ParameterCodec)

	outBuff := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	exec, err := remotecommand.NewSPDYExecutor(r.restConfig, "POST", request.URL())
	if err != nil {
		return nil, nil, err
	}
	if err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: outBuff,
		Stderr: errBuf,
	}); err != nil {
		return nil, nil, err
	}
	return outBuff.Bytes(), errBuf.Bytes(), nil
}

func (r *LMEvalJobReconciler) handleDeletion(ctx context.Context, job *lmesv1alpha1.LMEvalJob, log logr.Logger) (reconcile.Result, error) {
	defer r.pullingJobs.remove(string(job.GetUID()))

	if controllerutil.ContainsFinalizer(job, lmesv1alpha1.FinalizerName) {
		// delete the corresponding pod if needed
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

// createJobCreationMetrics collects and publishes metric information about newly created LM-Eval jobs
func createJobCreationMetrics(log logr.Logger, job *lmesv1alpha1.LMEvalJob) {
	// Update the Prometheus metrics for each task in the tasklist
	log.Info("Creating a new LMEvalJob metric", "name", job.Name)
	for _, task := range job.Spec.TaskList.TaskNames {
		labels := make(map[string]string)

		// add job information to metric
		labels["eval_job_namespace"] = job.Namespace
		labels["framework"] = "lm-evaluation-harness"
		labels["model_type"] = job.Spec.Model
		labels["task"] = task

		// grab model name
		hasUrl := false
		hasName := false
		for _, arg := range job.Spec.ModelArgs {
			if arg.Name == "model" {
				labels["model_name"] = arg.Value
				hasUrl = true
			}
			if arg.Name == "base_url" {
				labels["base_url"] = arg.Value
				hasName = true
			}
			if hasUrl && hasName {
				break
			}
		}

		// create/update metric counter
		counter := metrics.GetOrCreateEvalCounter(labels)
		counter.Inc()
	}
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
		// End the current reconcile and get revisioned job in next reconcile
		return ctrl.Result{}, nil
	}

	// Validate user input
	if err := ValidateUserInput(job); err != nil {
		// Input validation failed
		job.Status.State = lmesv1alpha1.CompleteJobState
		job.Status.Reason = lmesv1alpha1.FailedReason
		job.Status.Message = fmt.Sprintf("Input validation failed: %s", err.Error())

		current := v1.Now()
		job.Status.CompleteTime = &current
		if err := r.Status().Update(ctx, job); err != nil {
			log.Error(err, "unable to update LMEvalJob status for input validation error")
		}
		log.Error(err, "Input validation failed for LMEvalJob", "name", job.Name)
		return ctrl.Result{}, err
	}

	// Validate the custom card if exists
	// FIXME: Move the validation to the webhook once we enable it.
	if err := r.validateCustomRecipes(job, log); err != nil {
		// custom card validation failed
		job.Status.State = lmesv1alpha1.CompleteJobState
		job.Status.Reason = lmesv1alpha1.FailedReason
		job.Status.Message = err.Error()
		// also update the complete time
		current := v1.Now()
		job.Status.CompleteTime = &current
		if err := r.Status().Update(ctx, job); err != nil {
			log.Error(err, "unable to update LMEvalJob status for custom recipe validation error")
		}
		log.Error(err, "Contain invalid custom recipe in the LMEvalJob", "name", job.Name)
		return ctrl.Result{}, err
	}

	// construct a new pod and create a pod for the job
	currentTime := v1.Now()
	pod := CreatePod(Options, job, log)
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

	// Create metrics
	createJobCreationMetrics(log, job)

	// Create the pod successfully. Wait for the driver to update the status
	job.Status.State = lmesv1alpha1.ScheduledJobState
	job.Status.PodName = job.GetPodName()
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
	return r.pullingJobs.addOrUpdate(string(job.GetUID()), Options.PodCheckingInterval), nil
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
		r.Recorder.Event(job, "Warning", "PodMissing",
			fmt.Sprintf("the pod for the LMEvalJob %s in namespace %s is gone",
				job.Name,
				job.Namespace))
		log.Error(err, "since the job's pod is gone, mark the job as complete with error result.")
		return ctrl.Result{}, err
	}

	if mainIdx := getContainerByName(&pod.Status, "main"); mainIdx == -1 {
		// waiting for the main container to be up
		return r.pullingJobs.addOrUpdate(string(job.GetUID()), Options.PodCheckingInterval), nil
	} else if podFailed, msg := isContainerFailed(&pod.Status.ContainerStatuses[mainIdx]); podFailed {
		job.Status.State = lmesv1alpha1.CompleteJobState
		job.Status.Reason = lmesv1alpha1.FailedReason
		job.Status.Message = msg
		if err := r.Status().Update(ctx, job); err != nil {
			log.Error(err, "unable to update LMEvalJob status for pod failure")
		}
		log.Info("detect an error on the job's pod. marked the job as done", "name", job.GetPodName())
		return ctrl.Result{}, err
	} else if pod.Status.ContainerStatuses[mainIdx].State.Running == nil {
		// Pod is not running yet, don't accept completion status from driver
		// This prevents the driver from marking the job as complete during pod initialisation
		log.Info("pod not running yet, skipping status update from driver", "podName", job.GetPodName())
		return r.pullingJobs.addOrUpdate(string(job.GetUID()), Options.PodCheckingInterval), nil
	}

	// pull status from the driver
	if err = r.updateStatus(ctx, log, job); err == nil && job.Status.State == lmesv1alpha1.CompleteJobState {
		// Job completed successfully, handle cleanup
		result, handleErr := r.handleComplete(ctx, log, job)
		if handleErr != nil {
			log.Error(handleErr, "failed to handle job completion, will retry")
			// If handleComplete fails, we should retry after the polling interval
			return r.pullingJobs.addOrUpdate(string(job.GetUID()), Options.PodCheckingInterval), handleErr
		}
		return result, nil
	}
	if err != nil {
		log.Error(err, "unable to retrieve the status from the job's pod. retry after the pulling interval")
	}
	return r.pullingJobs.addOrUpdate(string(job.GetUID()), Options.PodCheckingInterval), nil
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
	if job.Status.PodName == "" {
		return nil
	}

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
		// make sure the pod is in the complete state. if not, run the shutdown command
		if job.Status.PodName != "" {
			pod, err := r.getPod(ctx, job)
			if err == nil {
				if getRunningContainerByName(&pod.Status, "main") != -1 {
					// send shutdown command if the main container is running
					if err := r.shutdownDriver(ctx, job); err != nil {
						log.Error(err, "failed to shutdown the job pod. retry after the pulling interval")
						return r.pullingJobs.addOrUpdate(string(job.GetUID()), Options.PodCheckingInterval), nil
					}
				}
			} else {
				// the pod is gone ??
				log.Error(err, "LMEvalJob is marked as Complete but the pod is gone")
			}
		}

		r.Recorder.Event(job, "Normal", "JobCompleted",
			fmt.Sprintf("The LMEvalJob %s in namespace %s has completed",
				job.Name,
				job.Namespace))

		// record the CompleteTime
		current := v1.Now()
		job.Status.CompleteTime = &current
		if err := r.Status().Update(ctx, job); err != nil {
			log.Error(err, "failed to update status for completion")
		}
	}

	// make sure to clean up the pullingJobs
	r.pullingJobs.remove(string(job.GetUID()))
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
			log.Error(err, "failed to delete pod. scheduled a retry", "interval", Options.PodCheckingInterval.String())
			return r.pullingJobs.addOrUpdate(string(job.GetUID()), Options.PodCheckingInterval), err
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
	r.pullingJobs.remove(string(job.GetUID()))
	return ctrl.Result{}, err
}

func (r *LMEvalJobReconciler) handleSuspend(ctx context.Context, log logr.Logger, job *lmesv1alpha1.LMEvalJob) (ctrl.Result, error) {
	defer r.pullingJobs.remove(string(job.GetUID()))
	if job.Status.State != lmesv1alpha1.NewJobState {
		log.Info("Suspend job")
		if err := r.deleteJobPod(ctx, job); err != nil && client.IgnoreNotFound(err) != nil {
			log.Error(err, "failed to delete pod for suspended job")
			return r.pullingJobs.addOrUpdate(string(job.GetUID()), Options.PodCheckingInterval), nil
		}
	} else {
		log.Info("Create job in suspend state.")
	}
	job.Status.State = lmesv1alpha1.SuspendedJobState
	err := r.Status().Update(ctx, job)
	if err != nil {
		log.Error(err, "failed to update job status to suspended")
	}

	return ctrl.Result{}, err
}

func (r *LMEvalJobReconciler) handleResume(ctx context.Context, log logr.Logger, job *lmesv1alpha1.LMEvalJob) (ctrl.Result, error) {
	log.Info("Resume job")
	pod := CreatePod(Options, job, log)
	if err := r.Create(ctx, pod); err != nil {
		log.Error(err, "failed to create pod to resume job")
		return r.pullingJobs.addOrUpdate(string(job.GetUID()), Options.PodCheckingInterval), nil
	}
	job.Status.State = lmesv1alpha1.ScheduledJobState
	err := r.Status().Update(ctx, job)
	if err != nil {
		log.Error(err, "failed to update job status to scheduled")
	}
	return ctrl.Result{}, err
}

func (r *LMEvalJobReconciler) validateCustomRecipes(job *lmesv1alpha1.LMEvalJob, log logr.Logger) error {
	if job.Spec.TaskList.TaskRecipes == nil {
		return nil
	}

	customArtifacts := job.Spec.TaskList.CustomArtifacts
	for _, taskRecipe := range job.Spec.TaskList.TaskRecipes {
		if taskRecipe.Card.Custom != "" {
			_, err := unmarshal(taskRecipe.Card.Custom, []string{"loader"})
			if err != nil {
				log.Error(err, "failed to parse the custom card")
				return fmt.Errorf("failed to parse the custom card. %s", err.Error())
			}
		}

		// check if the reference custom template is defined under the custom section
		if taskRecipe.Template != nil && taskRecipe.Template.Ref != "" {
			custom := getCustomArtifactByName(customArtifacts.GetTemplates(), taskRecipe.Template.Ref)
			if custom == nil {
				return fmt.Errorf("the reference name of the custom template is not defined: %s", taskRecipe.Template.Ref)
			}
		}

		// check if the system prompt is defined in the custom section or not
		if taskRecipe.SystemPrompt != nil && taskRecipe.SystemPrompt.Ref != "" {
			if getCustomArtifactByName(customArtifacts.GetSystemPrompts(), taskRecipe.SystemPrompt.Ref) == nil {
				return fmt.Errorf("the reference name of the custom system prompt is not defined: %s", taskRecipe.SystemPrompt.Ref)
			}
		}

		// check if there any custom metric and check if it is defined in the custom section
		if len(taskRecipe.Metrics) > 0 {
			customMetrics := customArtifacts.GetMetrics()
			for _, metric := range taskRecipe.Metrics {
				if metric.Ref != "" {
					customMetric := getCustomArtifactByName(customMetrics, metric.Ref)
					if customMetric == nil {
						return fmt.Errorf("the reference name of the custom metric is not defined: %s", metric.Ref)
					}
				}
			}
		}

		// check if the task is defined in the custom section or not
		if taskRecipe.Task != nil && taskRecipe.Task.Ref != "" {
			if getCustomArtifactByName(customArtifacts.GetTasks(), taskRecipe.Task.Ref) == nil {
				return fmt.Errorf("the reference name of the custom task is not defined: %s", taskRecipe.Task.Ref)
			}
		}
	}

	// Check the format of each CustomArtifact
	for _, customT := range customArtifacts.GetTemplates() {
		_, err := unmarshal(customT.Value, []string{"input_format", "output_format"})
		if err != nil {
			log.Error(err, "failed to parse the custom template")
			return fmt.Errorf("failed to parse the custom template: %s. %s", customT.Name, err.Error())
		}
	}

	for _, customM := range customArtifacts.GetMetrics() {
		_, err := unmarshal(customM.Value, []string{"__type__"})
		if err != nil {
			log.Error(err, "failed to parse the custom metric.")
			return fmt.Errorf("failed to parse the custom metric: %s. %s", customM.Name, err.Error())
		}
	}

	for _, customTask := range customArtifacts.GetTasks() {
		metricObj, err := unmarshal(customTask.Value, []string{"__type__", "input_fields"})
		if err != nil {
			log.Error(err, "failed to parse the custom task")
			return fmt.Errorf("failed to parse the custom task:%s. %s", customTask.Name, err.Error())
		}

		if metricObj["__type__"] != "task" {
			err := fmt.Errorf("custom task's type is incorrect")
			log.Error(err, "task %s validation failed", customTask.Name)
			return err
		}
	}

	return nil
}

func getCustomArtifactByName(customs []lmesv1alpha1.CustomArtifact, name string) *lmesv1alpha1.CustomArtifact {
	if len(customs) == 0 {
		return nil
	}
	for _, custom := range customs {
		if custom.Name == name {
			return &custom
		}
	}
	return nil
}

func unmarshal(custom string, props []string) (map[string]interface{}, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(custom), &obj); err != nil {
		return nil, err
	}

	for _, prop := range props {
		if _, ok := obj[prop]; !ok {
			err := fmt.Errorf("missing %s definition", prop)
			return nil, err
		}
	}
	return obj, nil
}

func CreatePod(svcOpts *serviceOptions, job *lmesv1alpha1.LMEvalJob, log logr.Logger) *corev1.Pod {

	var envVars = removeProtectedEnvVars(job.Spec.Pod.GetContainer().GetEnv())

	disableTelemetryEnvVars := []corev1.EnvVar{
		{
			Name:  "HF_HUB_DISABLE_TELEMETRY",
			Value: "1",
		},
		{
			Name:  "DO_NOT_TRACK",
			Value: "1",
		},
	}

	envVars = append(envVars, disableTelemetryEnvVars...)

	var volumeMounts = []corev1.VolumeMount{
		{
			Name:      "shared",
			MountPath: "/opt/app-root/src/bin",
		},
	}

	if job.Spec.HasCustomOutput() {
		outputPVCMount := corev1.VolumeMount{
			Name:      "outputs",
			MountPath: OutputPath,
		}
		volumeMounts = append(volumeMounts, outputPVCMount)

	}

	var volumes = []corev1.Volume{
		{
			Name: "shared", VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	if job.Spec.HasCustomOutput() {

		var claimName string
		if job.Spec.Outputs.HasManagedPVC() {
			claimName = generateManagedPVCName(job)
		} else if job.Spec.Outputs.HasExistingPVC() {
			claimName = *job.Spec.Outputs.PersistentVolumeClaimName
		}

		outputPVC := corev1.Volume{
			Name: "outputs",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: claimName,
					ReadOnly:  false,
				},
			},
		}
		volumes = append(volumes, outputPVC)
	}

	disallowRemoteCodeEnvVars := []corev1.EnvVar{
		{
			Name:  "TRUST_REMOTE_CODE",
			Value: "0",
		},
		{
			Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
			Value: "0",
		},
		{
			Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
			Value: "False",
		},
	}
	allowRemoteCodeEnvVars := []corev1.EnvVar{
		{
			Name:  "TRUST_REMOTE_CODE",
			Value: "1",
		},
		{
			Name:  "HF_DATASETS_TRUST_REMOTE_CODE",
			Value: "1",
		},
		{
			Name:  "UNITXT_ALLOW_UNVERIFIED_CODE",
			Value: "True",
		},
	}

	if job.Spec.AllowCodeExecution != nil && *job.Spec.AllowCodeExecution {
		// Disable remote code execution by default

		if !svcOpts.AllowCodeExecution {
			log.Error(fmt.Errorf("code execution not allowed by the operator"), "change this setting and redeploy the operator")
			envVars = append(envVars, disallowRemoteCodeEnvVars...)
		} else {
			log.Info("enabling code execution")
			envVars = append(envVars, allowRemoteCodeEnvVars...)
		}
	} else {
		envVars = append(envVars, disallowRemoteCodeEnvVars...)
	}

	offlineHuggingFaceEnvVars := []corev1.EnvVar{
		{
			Name:  "HF_DATASETS_OFFLINE",
			Value: "1",
		},
		{
			Name:  "HF_HUB_OFFLINE",
			Value: "1",
		},
		{
			Name:  "TRANSFORMERS_OFFLINE",
			Value: "1",
		},
		{
			Name:  "HF_EVALUATE_OFFLINE",
			Value: "1",
		},
		{
			Name:  "UNITXT_USE_ONLY_LOCAL_CATALOGS",
			Value: "True",
		},
	}

	// Enforce offline mode by default
	if job.Spec.AllowOnline != nil && *job.Spec.AllowOnline {

		if !svcOpts.AllowOnline {
			log.Error(fmt.Errorf("online mode not allowed by the operator"), "change this setting and redeploy the operator")
			envVars = append(envVars, offlineHuggingFaceEnvVars...)
		}
	} else {
		envVars = append(envVars, offlineHuggingFaceEnvVars...)
	}

	if job.Spec.IsOffline() {

		if job.Spec.HasOfflinePVC() {
			offlinePVCMount := corev1.VolumeMount{
				Name:      "offline",
				MountPath: HuggingFaceHomePath,
			}
			volumeMounts = append(volumeMounts, offlinePVCMount)

			offlinePVC := corev1.Volume{
				Name: "offline",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: *job.Spec.Offline.StorageSpec.PersistentVolumeClaimName,
						ReadOnly:  false,
					},
				},
			}
			volumes = append(volumes, offlinePVC)
		}

		if job.Spec.HasOfflineS3() {

			sslVerify := "true"

			if job.Spec.Offline.StorageSpec.S3Spec.VerifySSL != nil {
				sslVerify = strconv.FormatBool(*job.Spec.Offline.StorageSpec.S3Spec.VerifySSL)
			}

			s3EnvVars := []corev1.EnvVar{
				{
					Name: "AWS_ACCESS_KEY_ID",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: job.Spec.Offline.StorageSpec.S3Spec.AccessKeyIdRef.Name,
							},
							Key: job.Spec.Offline.StorageSpec.S3Spec.AccessKeyIdRef.Key,
						},
					},
				},
				{
					Name: "AWS_SECRET_ACCESS_KEY",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: job.Spec.Offline.StorageSpec.S3Spec.SecretAccessKeyRef.Name,
							},
							Key: job.Spec.Offline.StorageSpec.S3Spec.SecretAccessKeyRef.Key,
						},
					},
				},
				{
					Name: "AWS_DEFAULT_REGION",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: job.Spec.Offline.StorageSpec.S3Spec.Region.Name,
							},
							Key: job.Spec.Offline.StorageSpec.S3Spec.Region.Key,
						},
					},
				},
				{
					Name: "AWS_S3_BUCKET",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: job.Spec.Offline.StorageSpec.S3Spec.Bucket.Name,
							},
							Key: job.Spec.Offline.StorageSpec.S3Spec.Bucket.Key,
						},
					},
				},
				{
					Name: "AWS_S3_ENDPOINT",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: job.Spec.Offline.StorageSpec.S3Spec.Endpoint.Name,
							},
							Key: job.Spec.Offline.StorageSpec.S3Spec.Endpoint.Key,
						},
					},
				},
				{
					Name:  "AWS_PATH",
					Value: job.Spec.Offline.StorageSpec.S3Spec.Path,
				},
				{
					Name:  "S3_VERIFY_SSL",
					Value: sslVerify,
				},
			}
			envVars = append(envVars, s3EnvVars...)

			// If certificates are specified, create volume to hold them
			if job.Spec.Offline.StorageSpec.S3Spec.HasCertificates() {

				s3CertificatesMount := corev1.VolumeMount{
					Name:      "certificates-s3",
					MountPath: "/etc/certificates/s3",
				}
				volumeMounts = append(volumeMounts, s3CertificatesMount)

				s3CertificatesVolume := corev1.Volume{
					Name: "certificates-s3",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: job.Spec.Offline.StorageSpec.S3Spec.CABundle.Name,
							},
						},
					},
				}

				volumes = append(volumes, s3CertificatesVolume)

				s3CertificateEnvVars := []corev1.EnvVar{
					{
						Name:  "AWS_CA_BUNDLE",
						Value: fmt.Sprintf("/etc/certificates/s3/%s", job.Spec.Offline.StorageSpec.S3Spec.CABundle.Key),
					},
				}

				envVars = append(envVars, s3CertificateEnvVars...)
			}

		}

	}

	volumes = append(volumes, job.Spec.Pod.GetVolumes()...)
	volumeMounts = append(volumeMounts, job.Spec.Pod.GetContainer().GetVolumMounts()...)
	labels := getPodLabels(job.Labels, log)
	annotations := getAnnotations(job.Annotations, log)
	resources := getResources(job.Spec.Pod.GetContainer().GetResources())
	affinity := job.Spec.Pod.GetAffinity()
	podSecurityContext := getPodSecurityContext(job.Spec.Pod.GetSecurityContext())
	mainSecurityContext := getMainSecurityContext(job.Spec.Pod.GetContainer().GetSecurityContext())
	containers := []corev1.Container{
		{
			Name:            "main",
			Image:           svcOpts.PodImage,
			ImagePullPolicy: svcOpts.ImagePullPolicy,
			Env:             envVars,
			Command:         generateCmd(svcOpts, job),
			Args:            generateArgs(svcOpts, job, log),
			SecurityContext: mainSecurityContext,
			VolumeMounts:    volumeMounts,
			Resources:       *resources,
			Ports: []corev1.ContainerPort{
				{
					ContainerPort: int32(svcOpts.DriverPort),
				},
			},
		},
	}
	containers = append(containers, job.Spec.Pod.GetSideCards()...)

	// Then compose the Pod CR
	pod := corev1.Pod{
		TypeMeta: v1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      job.GetPodName(),
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
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:            "driver",
					Image:           svcOpts.DriverImage,
					ImagePullPolicy: svcOpts.ImagePullPolicy,
					Command:         []string{DriverPath, "--copy", DestDriverPath},
					SecurityContext: defaultSecurityContext,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "shared",
							MountPath: "/opt/app-root/src/bin",
						},
					},
				},
			},
			Containers:      containers,
			SecurityContext: podSecurityContext,
			Affinity:        affinity,
			Volumes:         volumes,
			RestartPolicy:   corev1.RestartPolicyNever,
		},
	}
	return &pod
}

func getPodLabels(src map[string]string, log logr.Logger) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name": "ta-lmes",
	}
	mergeMapWithFilters(labels, src, labelFilterPrefixes, log)
	return labels
}

func getAnnotations(annotations map[string]string, log logr.Logger) map[string]string {
	if len(annotations) == 0 {
		return nil
	}
	dest := map[string]string{}
	mergeMapWithFilters(dest, annotations, annotationFilterPrefixes, log)
	return dest
}

func getResources(resources *corev1.ResourceRequirements) *corev1.ResourceRequirements {
	if resources == nil {
		return &corev1.ResourceRequirements{}
	}
	return resources
}

func getPodSecurityContext(securityContext *corev1.PodSecurityContext) *corev1.PodSecurityContext {
	// user config Overrides default config
	if securityContext == nil {
		return defaultPodSecurityContext
	}
	return securityContext
}

func getMainSecurityContext(securityContext *corev1.SecurityContext) *corev1.SecurityContext {
	// user config Overrides default config
	if securityContext == nil {
		return defaultSecurityContext
	}
	return securityContext
}

// Merge the map based on the filters. If the names in the `src` map contains any prefixes
// in the prefixFilters list, those KV will be discarded, otherwise, KV will be merge into
// `dest` map.
func mergeMapWithFilters(dest, src map[string]string, prefixFilters []string, log logr.Logger) {
	if len(prefixFilters) == 0 {
		// Fast path if the labelFilterPrefix is empty.
		maps.Copy(dest, src)
	} else {
		for k, v := range src {
			if slices.ContainsFunc(prefixFilters, func(prefix string) bool {
				return strings.HasPrefix(k, prefix)
			}) {
				log.Info("the label is not propagated to the pod", k, v)
			} else {
				dest[k] = v
			}
		}
	}
}

func validateBatchSize(input string, maxBatchSize int, log logr.Logger) string {

	maxBatchSizeString := strconv.Itoa(maxBatchSize)

	if input == "auto" {
		// No validation needed, return original
		return input
	}

	// Validate "auto:N" style batch size
	if strings.HasPrefix(input, "auto:") {
		autoN := strings.TrimPrefix(input, "auto:")
		if n, err := strconv.Atoi(autoN); err == nil && n > 0 {
			// If N is a positive integer, use it and ignore maxBatchSize, since is now the maximum batch size
			return input
		}
		// If N is an invalid integer, use "auto:maxBatchSize"
		log.Info(input + " not supported. Using auto:" + maxBatchSizeString)
		return "auto:" + maxBatchSizeString
	}

	// Validate N batch size
	if n, err := strconv.Atoi(input); err == nil && n > 0 {
		// If N is valid, but larger than maxBatchSize, set it to maximum batch size
		if n > maxBatchSize {
			log.Info("batchSize is greater than max-batch-size of the controller's configuration, use the max-batch-size instead")
			return maxBatchSizeString
		}
		// If N is valid, use it
		return strconv.Itoa(n)
	}

	log.Info("invalid batchSize " + input + " using batch size " + DefaultBatchSize)
	return DefaultBatchSize
}

func generateArgs(svcOpts *serviceOptions, job *lmesv1alpha1.LMEvalJob, log logr.Logger) []string {
	if job == nil {
		return nil
	}

	// Use argument escaping
	cmds := []string{
		"python", "-m", "lm_eval",
		"--output_path", "/opt/app-root/src/output",
		"--model", job.Spec.Model,
	}

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
	var batchSize = svcOpts.DefaultBatchSize
	if job.Spec.BatchSize != nil {
		// This could be done in the webhook if it's enabled.
		batchSize = validateBatchSize(*job.Spec.BatchSize, svcOpts.MaxBatchSize, log)
	}

	cmds = append(cmds, "--batch_size", batchSize)
	// --system_instruction
	if job.Spec.SystemInstruction != "" {
		cmds = append(cmds, "--system_instruction", job.Spec.SystemInstruction)
	}

	// --apply_chat_template
	// check for the default string value of "false"
	if job.Spec.ChatTemplate != nil {
		if !job.Spec.ChatTemplate.Enabled {

		} else if job.Spec.ChatTemplate.Enabled && job.Spec.ChatTemplate.Name == "" {
			cmds = append(cmds, "--apply_chat_template")
		} else {
			cmds = append(cmds, "--apply_chat_template", job.Spec.ChatTemplate.Name)
		}
	}

	return cmds
}

func concatTasks(tasks lmesv1alpha1.TaskList) []string {
	if len(tasks.TaskRecipes) == 0 {
		return tasks.TaskNames
	}
	recipesName := make([]string, len(tasks.TaskRecipes))
	for i := range tasks.TaskRecipes {
		// assign internal used task name
		recipesName[i] = fmt.Sprintf("%s_%d", driver.TaskRecipePrefix, i)
	}

	return append(tasks.TaskNames, recipesName...)
}

func generateCmd(svcOpts *serviceOptions, job *lmesv1alpha1.LMEvalJob) []string {
	if job == nil {
		return nil
	}
	cmds := []string{
		DestDriverPath,
		"--output-path", "/opt/app-root/src/output",
	}

	if job.Spec.HasOfflineS3() {
		cmds = append(cmds, "--download-assets-s3")
	}

	if svcOpts.DetectDevice {
		cmds = append(cmds, "--detect-device")
	}

	if svcOpts.DriverPort != 0 && svcOpts.DriverPort != driver.DefaultPort {
		cmds = append(cmds, "--listen-port", fmt.Sprintf("%d", svcOpts.DriverPort))
	}

	if job.Spec.AllowOnline != nil && *job.Spec.AllowOnline && svcOpts.AllowOnline {
		cmds = append(cmds, "--allow-online")
	}

	if job.Spec.TaskList.HasCustomTasksWithGit() {
		cmds = append(cmds, "--custom-task-git-url", job.Spec.TaskList.CustomTasks.Source.GitSource.URL)

		if job.Spec.TaskList.CustomTasks.Source.GitSource.Branch != nil {
			cmds = append(cmds, "--custom-task-git-branch", *job.Spec.TaskList.CustomTasks.Source.GitSource.Branch)
		}

		if job.Spec.TaskList.CustomTasks.Source.GitSource.Commit != nil {
			cmds = append(cmds, "--custom-task-git-commit", *job.Spec.TaskList.CustomTasks.Source.GitSource.Commit)
		}

		if job.Spec.TaskList.CustomTasks.Source.GitSource.Path != "" {
			cmds = append(cmds, "--custom-task-git-path", job.Spec.TaskList.CustomTasks.Source.GitSource.Path)
		}

		for _, taskName := range job.Spec.TaskList.TaskNames {
			cmds = append(cmds, "--task-name", taskName)
		}
	}

	appendArtifactCmd := func(taskName string, artifacts []lmesv1alpha1.CustomArtifact) {
		for _, artifact := range artifacts {
			cmds = append(cmds, "--custom-artifact", fmt.Sprintf("%s|%s", taskName, artifact.String()))
		}
	}

	cr_idx := 0
	for _, recipe := range job.Spec.TaskList.TaskRecipes {
		// duplicate the TaskRecipe and update its content to generate proper recipe string
		dupRecipe := recipe.DeepCopy()

		if recipe.Card.Custom != "" {
			// custom card, need to inject --custom-artifact arg as well
			// the format of a custom card's name: custom_<index>
			dupRecipe.Card.Name = fmt.Sprintf("cards.%s_%d", driver.CustomCardPrefix, cr_idx)
			cmds = append(cmds, "--custom-artifact", fmt.Sprintf("card|%s_%d|%s", driver.CustomCardPrefix, cr_idx, dupRecipe.Card.Custom))
			cr_idx++
		}

		cmds = append(cmds, "--task-recipe", dupRecipe.String())
	}

	// go through custom artificats and add corresponding arguments
	if job.Spec.TaskList.CustomArtifacts != nil {
		appendArtifactCmd("template", job.Spec.TaskList.CustomArtifacts.GetTemplates())
		appendArtifactCmd("system_prompt", job.Spec.TaskList.CustomArtifacts.GetSystemPrompts())
		appendArtifactCmd("metric", job.Spec.TaskList.CustomArtifacts.GetMetrics())
		appendArtifactCmd("task", job.Spec.TaskList.CustomArtifacts.GetTasks())
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

func isContainerFailed(status *corev1.ContainerStatus) (bool, string) {
	if status.State.Waiting != nil &&
		status.State.Waiting.Reason != "PodInitializing" {
		return true, status.State.Waiting.Reason
	}
	if status.State.Terminated != nil {
		// Container is considered failed if it has a non-zero exit code OR an unexpected termination reason
		if status.State.Terminated.ExitCode != 0 ||
			(status.State.Terminated.Reason != "Completed" && status.State.Terminated.Reason != "") {
			return true, status.State.Terminated.Reason
		}
	}
	return false, ""
}

// return the index of the container which is in running state and with the specified name
// otherwise return -1
func getRunningContainerByName(status *corev1.PodStatus, name string) int {
	if idx := getContainerByName(status, name); idx != -1 && status.ContainerStatuses[idx].State.Running != nil {
		return idx
	}
	return -1
}

func getContainerByName(status *corev1.PodStatus, name string) int {
	if status.ContainerStatuses == nil {
		return -1
	}
	return slices.IndexFunc(status.ContainerStatuses, func(s corev1.ContainerStatus) bool {
		return s.Name == name
	})
}

var ProtectedEnvVarNames = []string{
	"TRUST_REMOTE_CODE",
	"HF_DATASETS_TRUST_REMOTE_CODE",
	"HF_DATASETS_OFFLINE",
	"HF_HUB_OFFLINE",
	"TRANSFORMERS_OFFLINE",
	"HF_EVALUATE_OFFLINE",
	"UNITXT_ALLOW_UNVERIFIED_CODE",
}

// removeProtectedEnvVars removes protected EnvVars from a list of EnvVars
func removeProtectedEnvVars(envVars []corev1.EnvVar) []corev1.EnvVar {
	var allowedEnvVars []corev1.EnvVar

	for _, env := range envVars {
		exclude := false
		for _, name := range ProtectedEnvVarNames {
			if env.Name == name {
				exclude = true
				break
			}
		}
		if !exclude {
			allowedEnvVars = append(allowedEnvVars, env)
		}
	}

	return allowedEnvVars
}
