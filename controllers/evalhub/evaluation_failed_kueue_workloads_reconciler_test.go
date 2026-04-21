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
})
