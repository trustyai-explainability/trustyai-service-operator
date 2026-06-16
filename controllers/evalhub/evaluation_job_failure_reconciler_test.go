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
	})

	Describe("sanitizeErrorMessage", func() {
		It("returns short messages unchanged", func() {
			msg := "Image not found"
			Expect(sanitizeErrorMessage(msg)).To(Equal(msg))
		})

		It("condenses OCI artifact pull unauthorized errors", func() {
			msg := `[unable to pull image or OCI artifact: pull image err: copying system image from manifest list: writing blob: storing blob to file "/var/tmp/container_images_storage2488073217/1": happened during read: unexpected EOF (while reconnecting: Get "https://cdn01.quay.io/quayio-production-s3/sha256/c4/c4d19f59e080ea5baf32f8368164510de7465fc76d39c87c0d009e287a9ab65d?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIATAAF2YHTGR23ZTE6%2F20260616%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20260616T091507Z&X-Amz-Expires=600&X-Amz-SignedHeaders=host&X-Amz-Signature=3cdc9793295e07c792d7c31b501f391a21ffd6182f980187dccbc74d30f70a76&region=us-east-1&namespace=rhoai&username=rhoai+devops_rhoai_readonly_bot&repo_name=odh-ta-lmes-job-rhel9&akamai_signature=exp=1781602207~hmac=cf338885c900223b86adbb2ca8e14dea3067bbe85e7ffd14faae64275e89f896": EOF); artifact err: provided artifact is a container image, unable to pull image or OCI artifact: pull image err: initializing source docker://quay.io/rhoai/odh-ta-lmes-job-rhel9@sha256:1acfc26eb6cca49e318bcdee30f0ce4ea2ceb81f4bbaa68efe3025a664a7a1fb: reading manifest sha256:1acfc26eb6cca49e318bcdee30f0ce4ea2ceb81f4bbaa68efe3025a664a7a1fb in quay.io/rhoai/odh-ta-lmes-job-rhel9: unauthorized: access to the requested resource is not authorized; artifact err: get manifest: build image source: reading manifest sha256:1acfc26eb6cca49e318bcdee30f0ce4ea2ceb81f4bbaa68efe3025a664a7a1fb in quay.io/rhoai/odh-ta-lmes-job-rhel9: unauthorized: access to the requested resource is not authorized]`

			result := sanitizeErrorMessage(msg)
			Expect(result).To(Equal("OCI artifact pull failed: unauthorized access to registry. Verify image pull secrets and registry credentials."))
			Expect(len(result)).To(BeNumerically("<", len(msg)), "condensed message should be shorter than original")
		})

		It("condenses OCI artifact pull network errors", func() {
			msg := `unable to pull image or OCI artifact: pull image err: copying system image: reading blob: Get "https://registry.example.com/v2/repo/blobs/sha256:abc123...": EOF (while reconnecting after network timeout)`

			result := sanitizeErrorMessage(msg)
			Expect(result).To(Equal("OCI artifact pull failed: network connectivity issue. Check registry accessibility and network policies."))
		})

		It("condenses OCI artifact pull not found errors", func() {
			msg := `unable to pull image or OCI artifact: pull image err: reading manifest latest in docker.io/library/nonexistent: manifest unknown: manifest unknown`

			result := sanitizeErrorMessage(msg)
			Expect(result).To(Equal("OCI artifact pull failed: artifact not found in registry. Verify the image/artifact reference and tag."))
		})

		It("condenses OCI artifact pull invalid name errors", func() {
			msg := `unable to pull image or OCI artifact: pull image err: invalid reference format: repository name must be lowercase`

			result := sanitizeErrorMessage(msg)
			Expect(result).To(Equal("OCI artifact pull failed: invalid artifact name or reference format."))
		})

		It("provides generic message for unknown OCI artifact pull patterns", func() {
			msg := `unable to pull image or OCI artifact: pull image err: some unknown error occurred during image download that we haven't seen before and is very specific to this case`

			result := sanitizeErrorMessage(msg)
			Expect(result).To(Equal("OCI artifact pull failed. Check artifact reference, registry credentials, and network connectivity."))
		})

		It("matches known patterns regardless of message casing", func() {
			msg := `Unable to pull image or OCI artifact: Pull image err: unauthorized: access to the requested resource is not authorized`

			result := sanitizeErrorMessage(msg)
			Expect(result).To(Equal("OCI artifact pull failed: unauthorized access to registry. Verify image pull secrets and registry credentials."))
		})

		It("truncates non-matching long messages", func() {
			// Create a message longer than maxErrorMessageLength (500) that doesn't match any pattern
			longMsg := "Some completely different error: " + string(make([]byte, 500))

			result := sanitizeErrorMessage(longMsg)
			Expect(len(result)).To(Equal(maxErrorMessageLength))
			Expect(result).To(HaveSuffix("..."))
		})
	})
})
