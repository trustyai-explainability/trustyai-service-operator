/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Evaluation job failure reconciler helpers", func() {
	Describe("benchmarkIndexFromJob", func() {
		DescribeTable("returns benchmark index from Job labels",
			func(job *batchv1.Job, want int) {
				Expect(benchmarkIndexFromJob(job)).To(Equal(want))
			},
			Entry("nil labels", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: nil}}, 0),
			Entry("missing label", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}}, 0),
			Entry("index 0", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{evalHubBenchmarkIndexLabel: "0"}}}, 0),
			Entry("index 2", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{evalHubBenchmarkIndexLabel: "2"}}}, 2),
			Entry("invalid falls back", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{evalHubBenchmarkIndexLabel: "x"}}}, 0),
			Entry("negative falls back", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{evalHubBenchmarkIndexLabel: "-1"}}}, 0),
		)
	})

	Describe("podOperatorOnlyFailureMessage", func() {
		It("reports adapter OOMKilled", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{{
						Name: adapterContainerName,
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137},
						},
					}},
				},
			}
			_, ok := podOperatorOnlyFailureMessage(pod)
			Expect(ok).To(BeTrue(), "expected operator-only failure for adapter OOMKilled")
		})

		It("reports init ErrImagePull", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{{
						Name: initContainerName,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull", Message: "pull failed"},
						},
					}},
				},
			}
			_, ok := podOperatorOnlyFailureMessage(pod)
			Expect(ok).To(BeTrue(), "expected operator-only failure for init ErrImagePull")
		})

		It("does not report adapter exit 0 completed", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{{
						Name: adapterContainerName,
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{Reason: "Completed", ExitCode: 0},
						},
					}},
				},
			}
			_, ok := podOperatorOnlyFailureMessage(pod)
			Expect(ok).To(BeFalse(), "did not expect operator-only failure for successful adapter completion")
		})

		It("does not report adapter Error exit 1 (typical post-EvalHub callback)", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{{
						Name: adapterContainerName,
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1},
						},
					}},
				},
			}
			_, ok := podOperatorOnlyFailureMessage(pod)
			Expect(ok).To(BeFalse(), "did not expect operator-only failure: adapter often exits Error/1 after POSTing failed to EvalHub")
		})

		It("reports sidecar Error exit 1", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{{
						Name: sidecarContainerName,
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1},
						},
					}},
				},
			}
			_, ok := podOperatorOnlyFailureMessage(pod)
			Expect(ok).To(BeTrue(), "expected operator-only failure for sidecar Error exit 1")
		})

		It("reports sidecar CrashLoopBackOff", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{{
						Name: sidecarContainerName,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off"},
						},
					}},
				},
			}
			_, ok := podOperatorOnlyFailureMessage(pod)
			Expect(ok).To(BeTrue(), "expected operator-only failure for sidecar CrashLoopBackOff")
		})

		It("reports pod unschedulable (e.g. missing PVC) after grace period", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					Conditions: []corev1.PodCondition{{
						Type:               corev1.PodScheduled,
						Status:             corev1.ConditionFalse,
						Reason:             corev1.PodReasonUnschedulable,
						Message:            "0/3 nodes available: persistentvolumeclaim \"my-pvc\" not found",
						LastTransitionTime: metav1.NewTime(time.Now().Add(-3 * time.Minute)),
					}},
				},
			}
			msg, ok := podOperatorOnlyFailureMessage(pod)
			Expect(ok).To(BeTrue(), "expected operator-only failure for unschedulable pod past grace period")
			Expect(msg).To(ContainSubstring("unschedulable"))
			Expect(msg).To(ContainSubstring("my-pvc"))
		})

		It("does not report pod unschedulable within grace period (transient autoscaler condition)", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					Conditions: []corev1.PodCondition{{
						Type:               corev1.PodScheduled,
						Status:             corev1.ConditionFalse,
						Reason:             corev1.PodReasonUnschedulable,
						Message:            "0/3 nodes available: insufficient memory",
						LastTransitionTime: metav1.NewTime(time.Now().Add(-30 * time.Second)),
					}},
				},
			}
			_, ok := podOperatorOnlyFailureMessage(pod)
			Expect(ok).To(BeFalse(), "should not report unschedulable pod within grace period")
		})

		It("does not report scheduled pod as unschedulable", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodScheduled,
						Status: corev1.ConditionTrue,
					}},
				},
			}
			_, ok := podOperatorOnlyFailureMessage(pod)
			Expect(ok).To(BeFalse(), "should not report a successfully scheduled running pod")
		})

		It("does not report pending pod without Unschedulable reason", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					Conditions: []corev1.PodCondition{{
						Type:   corev1.PodScheduled,
						Status: corev1.ConditionFalse,
						Reason: "ContainersNotReady",
					}},
				},
			}
			_, ok := podOperatorOnlyFailureMessage(pod)
			Expect(ok).To(BeFalse(), "should not report pending pod without Unschedulable reason")
		})
	})

	Describe("podSchedulingFailureMessage", func() {
		It("returns false for non-pending pod", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			}
			_, ok := podSchedulingFailureMessage(pod)
			Expect(ok).To(BeFalse())
		})

		It("returns true with message for Unschedulable pending pod past grace period", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					Conditions: []corev1.PodCondition{{
						Type:               corev1.PodScheduled,
						Status:             corev1.ConditionFalse,
						Reason:             corev1.PodReasonUnschedulable,
						Message:            "persistentvolumeclaim \"datasets-pvc\" not found",
						LastTransitionTime: metav1.NewTime(time.Now().Add(-3 * time.Minute)),
					}},
				},
			}
			msg, ok := podSchedulingFailureMessage(pod)
			Expect(ok).To(BeTrue())
			Expect(msg).To(ContainSubstring("datasets-pvc"))
		})

		It("returns false for Unschedulable pod within grace period", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					Conditions: []corev1.PodCondition{{
						Type:               corev1.PodScheduled,
						Status:             corev1.ConditionFalse,
						Reason:             corev1.PodReasonUnschedulable,
						Message:            "insufficient memory",
						LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Second)),
					}},
				},
			}
			_, ok := podSchedulingFailureMessage(pod)
			Expect(ok).To(BeFalse(), "should not fire within grace period")
		})

		It("uses fallback message when condition message is empty", func() {
			pod := &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					Conditions: []corev1.PodCondition{{
						Type:               corev1.PodScheduled,
						Status:             corev1.ConditionFalse,
						Reason:             corev1.PodReasonUnschedulable,
						LastTransitionTime: metav1.NewTime(time.Now().Add(-3 * time.Minute)),
					}},
				},
			}
			msg, ok := podSchedulingFailureMessage(pod)
			Expect(ok).To(BeTrue())
			Expect(msg).To(ContainSubstring("pod unschedulable"))
		})
	})
})
