/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_parseEvalHubJobSA(t *testing.T) {
	t.Parallel()
	cases := []struct {
		saName    string
		wantName  string
		wantNS    string
		wantError bool
	}{
		{"evalhub-prabhu-job", "evalhub", "prabhu", false},
		{"eval-hub-prabhu-job", "eval-hub", "prabhu", false},
		// Namespace must be a single hyphen-free segment for unambiguous parse (matches common case prabhu, prod, etc.).
		{"my-evalhub-myns-job", "my-evalhub", "myns", false},
		{"no-suffix", "", "", true},
		{"only-job", "", "", true},
		{"x-job", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.saName, func(t *testing.T) {
			t.Parallel()
			gotName, gotNS, err := parseEvalHubJobSA(tc.saName)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEvalHubJobSA: %v", err)
			}
			if gotName != tc.wantName || gotNS != tc.wantNS {
				t.Fatalf("got name=%q ns=%q, want name=%q ns=%q", gotName, gotNS, tc.wantName, tc.wantNS)
			}
		})
	}
}

func Test_benchmarkIndexFromJob(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		job   *batchv1.Job
		want  int
	}{
		{"nil labels", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: nil}}, 0},
		{"missing label", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}}, 0},
		{"index 0", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{evalHubBenchmarkIndexLabel: "0"}}}, 0},
		{"index 2", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{evalHubBenchmarkIndexLabel: "2"}}}, 2},
		{"invalid falls back", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{evalHubBenchmarkIndexLabel: "x"}}}, 0},
		{"negative falls back", &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{evalHubBenchmarkIndexLabel: "-1"}}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := benchmarkIndexFromJob(tc.job); got != tc.want {
				t.Fatalf("benchmarkIndexFromJob() = %d, want %d", got, tc.want)
			}
		})
	}
}

func Test_podOperatorOnlyFailureMessage(t *testing.T) {
	t.Parallel()
	t.Run("adapter OOM", func(t *testing.T) {
		t.Parallel()
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
		if !ok {
			t.Fatal("expected operator-only failure for adapter OOMKilled")
		}
	})
	t.Run("init ErrImagePull", func(t *testing.T) {
		t.Parallel()
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
		if !ok {
			t.Fatal("expected operator-only failure for init ErrImagePull")
		}
	})
	t.Run("adapter exit 0 completed no report", func(t *testing.T) {
		t.Parallel()
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
		if ok {
			t.Fatal("did not expect operator-only failure for successful adapter completion")
		}
	})
	t.Run("adapter Error exit 1 after typical EvalHub callback no report", func(t *testing.T) {
		t.Parallel()
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
		if ok {
			t.Fatal("did not expect operator-only failure: adapter often exits Error/1 after POSTing failed to EvalHub")
		}
	})
	t.Run("sidecar Error exit 1 still report", func(t *testing.T) {
		t.Parallel()
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
		if !ok {
			t.Fatal("expected operator-only failure for sidecar Error exit 1")
		}
	})
	t.Run("sidecar CrashLoopBackOff", func(t *testing.T) {
		t.Parallel()
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
		if !ok {
			t.Fatal("expected operator-only failure for sidecar CrashLoopBackOff")
		}
	})
}
