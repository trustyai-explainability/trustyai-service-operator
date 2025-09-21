/*
Copyright 2024 IBM Corporation.

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

package job_mgr

import (
	"context"
	"fmt"

	workloadv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"
	"sigs.k8s.io/kueue/pkg/podset"
)

// +kubebuilder:rbac:groups=scheduling.k8s.io,resources=priorityclasses,verbs=list;get;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;watch;update;patch
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads/finalizers,verbs=update
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=resourceflavors,verbs=get;list;watch
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloadpriorityclasses,verbs=get;list;watch

type LMEvalJob struct {
	workloadv1alpha1.LMEvalJob
}

func ControllerSetUp(mgr manager.Manager, ns, configmap string, recorder record.EventRecorder) error {
	ctx := context.TODO()
	if err := jobframework.SetupWorkloadOwnerIndex(ctx, mgr.GetFieldIndexer(), workloadv1alpha1.GroupVersion.WithKind("LMEvalJob")); err != nil {
		return fmt.Errorf("workload indexer: %w", err)
	}
	lmes.JobMgrEnabled = true
	return jobframework.NewGenericReconcilerFactory(
		func() jobframework.GenericJob { return &LMEvalJob{} },
		func(b *builder.Builder, c client.Client) *builder.Builder {
			return b.Named("LMEvalJobWorkload")
		},
	)(mgr.GetClient(), mgr.GetEventRecorderFor("kueue")).SetupWithManager(mgr)

}

// Object returns the job instance.
func (job *LMEvalJob) Object() client.Object {
	return &job.LMEvalJob
}

// IsSuspended returns whether the job is suspended or not.
func (job *LMEvalJob) IsSuspended() bool {
	return job.Spec.Suspend
}

func (job *LMEvalJob) Suspend() {
	job.Spec.Suspend = true
}

// RunWithPodSetsInfo will inject the node affinity and podSet counts extracting from workload to job and unsuspend it.
func (job *LMEvalJob) RunWithPodSetsInfo(podSetsInfo []podset.PodSetInfo) error {
	job.Spec.Pod.Affinity = convertToAffinity(podSetsInfo)
	job.Spec.Suspend = false
	return nil
}

// RestorePodSetsInfo will restore the original node affinity and podSet counts of the job.
// Returns whether any change was done.
func (job *LMEvalJob) RestorePodSetsInfo(podSetsInfo []podset.PodSetInfo) bool {
	job.Spec.Pod.Affinity = convertToAffinity(podSetsInfo)
	return true
}

// Finished means whether the job is completed/failed or not,
// condition represents the workload finished condition.

func (job *LMEvalJob) Finished() (condition metav1.Condition, finished bool) {
	// ToDos: What should be in the condition and how will it be used?
	condition = metav1.Condition{
		Type:    "Finished",
		Status:  "False",
		Reason:  string(job.Status.Reason),
		Message: job.Status.Message,
	}
	finished = false

	if job.Status.State == workloadv1alpha1.CompleteJobState {
		condition.Status = "True"
		finished = true
		return
	}
	return
}

// PodSets will build workload podSets corresponding to the job.
func (job *LMEvalJob) PodSets() []kueue.PodSet {
	log := log.FromContext(context.TODO())
	// Use global Options permissions for job manager.
	// This will be updated before every job deployment.
	permConfig := &lmes.PermissionConfig{
		AllowOnline:        lmes.Options.AllowOnline,
		AllowCodeExecution: lmes.Options.AllowCodeExecution,
	}
	pod := lmes.CreatePod(lmes.Options, &job.LMEvalJob, permConfig, log)
	podSet := kueue.PodSet{
		Name:     job.GetPodName(),
		Count:    1,
		Template: corev1.PodTemplateSpec{Spec: pod.Spec},
	}
	podSets := []kueue.PodSet{}
	podSets = append(podSets, podSet)
	return podSets
}

// IsActive returns true if there are any running pods.
func (job *LMEvalJob) IsActive() bool {
	if job.Status.State == workloadv1alpha1.RunningJobState {
		return true
	} else {
		return false
	}
}

// PodsReady instructs whether job derived pods are all ready now.
func (job *LMEvalJob) PodsReady() bool {
	if job.Status.State == workloadv1alpha1.ScheduledJobState {
		return true
	} else {
		return false
	}
}

// GVK returns GVK (Group Version Kind) for the job.
func (job *LMEvalJob) GVK() schema.GroupVersionKind {
	return workloadv1alpha1.GroupVersion.WithKind("LMEvalJob")
}

// Convert NodeSelector in the PodSetInfo to Pod.Spec.Affinity
func convertToAffinity(psi []podset.PodSetInfo) *corev1.Affinity {
	if len(psi) > 0 {
		nsl := psi[0].NodeSelector // Note there is only 1 element in podset array see PodSets method above.
		if len(nsl) == 0 {
			return nil
		}
		nsra := []corev1.NodeSelectorRequirement{}
		for k, v := range nsl {
			nsr := corev1.NodeSelectorRequirement{
				Key:      k,
				Operator: "In",
				Values:   []string{v},
			}
			nsra = append(nsra, nsr)
		}
		nsta := []corev1.NodeSelectorTerm{}
		nsta = append(nsta, corev1.NodeSelectorTerm{MatchExpressions: nsra})
		return &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: nsta,
				},
			},
		}

	}
	return nil
}
