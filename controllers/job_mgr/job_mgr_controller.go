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
	workloadv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

type LMEvalJob workloadv1alpha1.LMEvalJob

var (
	GVK                = workloadv1alpha1.GroupVersion.WithKind("LMEvalJob")
	WorkloadReconciler = jobframework.NewGenericReconcilerFactory(
		func() jobframework.GenericJob { return &LMEvalJob{} },
		func(b *builder.Builder, c client.Client) *builder.Builder {
			return b.Named("LMEvalJobWorkload")
		},
	)
)

// Object returns the job instance.
func (lmej *LMEvalJob) Object() client.Object {
	return (*workloadv1alpha1.LMEvalJob)(lmej)
}

// IsSuspended returns whether the job is suspended or not.
func (lmej *LMEvalJob) IsSuspended() bool {
	return lmej.Spec.Suspend
}

func (lmej *LMEvalJob) Suspend() {
	// ToDo: I need to create methods to handle:
	// 1. Suspend(=true) Upon LMEvalJob creation. This means the job is created but pods are running.
	// 2. Suspend(=true) while the LMEvalJob is running. This means delete the pods owned by the job.
	// 3. Suspend(=false) while the LMEvalJob is in suspended state. This means re-create the pods owned by the job.
	lmej.Spec.Suspend = true
}

// RunWithPodSetsInfo will inject the node affinity and podSet counts extracting from workload to job and unsuspend it.
func (lmej *LMEvalJob) RunWithPodSetsInfo(podSetsInfo []podset.PodSetInfo) error {
	// Question: Where do I inject the node affinity and podSet counts in the LMEvaljob object ?
	lmej.Spec.Suspend = false
	return nil
}

// RestorePodSetsInfo will restore the original node affinity and podSet counts of the job.
// Returns whether any change was done.
func (lmej *LMEvalJob) RestorePodSetsInfo(podSetsInfo []podset.PodSetInfo) bool {
	// Question: Where do I inject the node affinity and podSet counts in the LMEvaljob object ?
	return true
}

// Finished means whether the job is completed/failed or not,
// condition represents the workload finished condition.
func (lmej *LMEvalJob) Finished() (condition metav1.Condition, finished bool) {
	// Question: What should be in the condition and how will it be used?
	if lmej.Status.State == workloadv1alpha1.CompleteJobState {
		finished = true
	}
	return metav1.Condition{}, finished
}

// PodSets will build workload podSets corresponding to the job.
func (lmej *LMEvalJob) PodSets() []kueue.PodSet {
	// ToDo: Need to create a function to extract PodSpec from the LMEvalJob object to create a PodSet for Workload.
	// Currently PodSpec is not stored in the LMEvalJob object.
	podSet := kueue.PodSet{
		Name:     lmej.Status.PodName,
		Count:    1,
		Template: corev1.PodTemplateSpec{},
	}
	podSets := []kueue.PodSet{}
	podSets = append(podSets, podSet)
	return podSets
}

// IsActive returns true if there are any running pods.
func (lmej *LMEvalJob) IsActive() bool {
	if lmej.Status.State == workloadv1alpha1.RunningJobState {
		return true
	} else {
		return false
	}
}

// PodsReady instructs whether job derived pods are all ready now.
func (lmej *LMEvalJob) PodsReady() bool {
	//Question: Need figure out what pods ready mean. The ScheduledJobState may not carry the podsReady meaning.
	if lmej.Status.State == workloadv1alpha1.ScheduledJobState {
		return true
	} else {
		return false
	}
}

// GVK returns GVK (Group Version Kind) for the job.
func (lmej *LMEvalJob) GVK() schema.GroupVersionKind {
	return GVK
}
