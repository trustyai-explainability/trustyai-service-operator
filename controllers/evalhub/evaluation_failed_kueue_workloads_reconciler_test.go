/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
)

var _ = Describe("Kueue failed workload reconciler helpers", func() {
	Describe("workloadQuotaReservedInadmissibleCondition", func() {
		It("returns QuotaReserved False Inadmissible with message", func() {
			wl := &kueue.Workload{
				Status: kueue.WorkloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:    kueue.WorkloadQuotaReserved,
							Status:  metav1.ConditionFalse,
							Reason:  "Inadmissible",
							Message: "ClusterQueue foo is stopped",
						},
					},
				},
			}
			c, ok := workloadQuotaReservedInadmissibleCondition(wl)
			Expect(ok).To(BeTrue())
			Expect(c).NotTo(BeNil())
			Expect(c.Message).To(Equal("ClusterQueue foo is stopped"))
		})

		It("does not match Custom type False (non-QuotaReserved)", func() {
			wl := &kueue.Workload{
				Status: kueue.WorkloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Custom",
							Status: metav1.ConditionFalse,
							Reason: "NotInadmissible",
						},
					},
				},
			}
			_, ok := workloadQuotaReservedInadmissibleCondition(wl)
			Expect(ok).To(BeFalse())
		})

		It("does not match QuotaReserved False with non-Inadmissible reason", func() {
			wl := &kueue.Workload{
				Status: kueue.WorkloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:   kueue.WorkloadQuotaReserved,
							Status: metav1.ConditionFalse,
							Reason: "OtherReason",
						},
					},
				},
			}
			_, ok := workloadQuotaReservedInadmissibleCondition(wl)
			Expect(ok).To(BeFalse())
		})

		It("returns false when only QuotaReserved True is present", func() {
			wl := &kueue.Workload{
				Status: kueue.WorkloadStatus{
					Conditions: []metav1.Condition{
						{
							Type:   kueue.WorkloadQuotaReserved,
							Status: metav1.ConditionTrue,
							Reason: "QuotaReserved",
						},
					},
				},
			}
			_, ok := workloadQuotaReservedInadmissibleCondition(wl)
			Expect(ok).To(BeFalse())
		})
	})

	Describe("jobOwnerFromWorkload", func() {
		It("returns the batch/v1 Job owner when mixed with other owner kinds", func() {
			uid := types.UID("deadbeef")
			wl := &kueue.Workload{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{APIVersion: "v1", Kind: "Pod", Name: "ignore", UID: "1"},
						{APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job", Name: "eval-job-1", UID: uid},
					},
				},
			}
			name, gotUID, ok := jobOwnerFromWorkload(wl)
			Expect(ok).To(BeTrue())
			Expect(name).To(Equal("eval-job-1"))
			Expect(gotUID).To(Equal(uid))
		})

		It("accepts legacy batch/v1 apiVersion string", func() {
			wl := &kueue.Workload{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{APIVersion: "batch/v1", Kind: "Job", Name: "legacy-api", UID: "2"},
					},
				},
			}
			name, _, ok := jobOwnerFromWorkload(wl)
			Expect(ok).To(BeTrue())
			Expect(name).To(Equal("legacy-api"))
		})

		It("returns false when there is no Job owner", func() {
			_, _, ok := jobOwnerFromWorkload(&kueue.Workload{})
			Expect(ok).To(BeFalse())
		})
	})

	Describe("isEvalHubEvaluationJob", func() {
		It("returns true when app and component labels match EvalHub evaluation job", func() {
			j := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						evalHubAppLabel:       evalHubAppValue,
						evalHubComponentLabel: evalHubComponentValue,
					},
				},
			}
			Expect(isEvalHubEvaluationJob(j)).To(BeTrue())
		})

		It("returns false when component label is missing", func() {
			j := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{evalHubAppLabel: evalHubAppValue},
				},
			}
			Expect(isEvalHubEvaluationJob(j)).To(BeFalse())
		})
	})

	Describe("jobRequestsGPU", func() {
		It("returns true when a container requests nvidia.com/gpu", func() {
			j := gpuJob("nvidia.com/gpu", "1")
			Expect(jobRequestsGPU(j)).To(BeTrue())
		})

		It("returns true when a container requests amd.com/gpu", func() {
			j := gpuJob("amd.com/gpu", "1")
			Expect(jobRequestsGPU(j)).To(BeTrue())
		})

		It("returns false when no GPU resources are requested", func() {
			j := &batchv1.Job{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "adapter",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("250m"),
											corev1.ResourceMemory: resource.MustParse("512Mi"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(jobRequestsGPU(j)).To(BeFalse())
		})

		It("returns false for an empty job", func() {
			Expect(jobRequestsGPU(&batchv1.Job{})).To(BeFalse())
		})
	})

	Describe("kueueConditionMentionsGPU", func() {
		It("returns true when message contains /gpu suffix", func() {
			Expect(kueueConditionMentionsGPU("insufficient quota for nvidia.com/gpu in flavor default")).To(BeTrue())
		})

		It("returns true when message contains amd.com/gpu", func() {
			Expect(kueueConditionMentionsGPU("insufficient quota for amd.com/gpu in cluster queue")).To(BeTrue())
		})

		It("returns true when GPU resource name is quoted", func() {
			Expect(kueueConditionMentionsGPU(`insufficient quota for "nvidia.com/gpu"`)).To(BeTrue())
		})

		It("returns false when message has no GPU reference", func() {
			Expect(kueueConditionMentionsGPU("ClusterQueue foo is stopped")).To(BeFalse())
		})

		It("returns false for empty message", func() {
			Expect(kueueConditionMentionsGPU("")).To(BeFalse())
		})
	})

	Describe("classifyKueueAdmissionFailure", func() {
		It("returns gpu_unavailable code when job requests GPU and condition mentions GPU", func() {
			job := gpuJob("nvidia.com/gpu", "1")
			cond := &metav1.Condition{
				Type:    "QuotaReserved",
				Status:  metav1.ConditionFalse,
				Reason:  "Inadmissible",
				Message: "insufficient quota for nvidia.com/gpu in flavor default, requested: 1, used: 0, borrowable: 0",
			}
			msg, code := classifyKueueAdmissionFailure(job, cond)
			Expect(code).To(Equal(messageCodeGPUUnavailable))
			Expect(msg).NotTo(BeEmpty())
			Expect(msg).NotTo(ContainSubstring("nvidia.com/gpu"))
			Expect(msg).NotTo(ContainSubstring("flavor"))
		})

		It("returns queue_error code when job requests GPU but condition does not mention GPU", func() {
			job := gpuJob("nvidia.com/gpu", "1")
			cond := &metav1.Condition{
				Type:    "QuotaReserved",
				Status:  metav1.ConditionFalse,
				Reason:  "Inadmissible",
				Message: "ClusterQueue foo is stopped",
			}
			_, code := classifyKueueAdmissionFailure(job, cond)
			Expect(code).To(Equal(messageCodeQueueError))
		})

		It("returns queue_error code for a CPU-only job even if condition mentions GPU", func() {
			job := &batchv1.Job{}
			cond := &metav1.Condition{
				Message: "insufficient quota for nvidia.com/gpu",
			}
			_, code := classifyKueueAdmissionFailure(job, cond)
			Expect(code).To(Equal(messageCodeQueueError))
		})

		It("returns queue_error code for CPU-only job with non-GPU failure", func() {
			job := &batchv1.Job{}
			cond := &metav1.Condition{
				Message: "insufficient quota for cpu",
			}
			_, code := classifyKueueAdmissionFailure(job, cond)
			Expect(code).To(Equal(messageCodeQueueError))
		})
	})
})

// gpuJob builds a minimal batchv1.Job that requests the given GPU resource.
func gpuJob(resourceName, quantity string) *batchv1.Job {
	return &batchv1.Job{
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "adapter",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceName(resourceName): resource.MustParse(quantity),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceName(resourceName): resource.MustParse(quantity),
								},
							},
						},
					},
				},
			},
		},
	}
}
