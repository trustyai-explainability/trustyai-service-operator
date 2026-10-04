/*
Copyright 2026 The TrustyAI Authors.

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
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func draJob() *lmesv1alpha1.LMEvalJob {
	return &lmesv1alpha1.LMEvalJob{
		ObjectMeta: metav1.ObjectMeta{Name: "dra", Namespace: "test"},
		Spec: lmesv1alpha1.LMEvalJobSpec{
			Model:    "hf",
			TaskList: lmesv1alpha1.TaskList{TaskNames: []string{"arc_easy"}},
			Pod: &lmesv1alpha1.LMEvalPodSpec{
				ResourceClaims: []corev1.PodResourceClaim{{Name: "gpu", ResourceClaimTemplateName: ptr.To("gpu-template")}},
				Container: &lmesv1alpha1.LMEvalContainer{Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
					Claims:   []corev1.ResourceClaim{{Name: "gpu", Request: "device"}},
				}},
			},
		},
	}
}

func TestCreatePodResourceClaims(t *testing.T) {
	for _, consumer := range []string{"main", "sidecar", "shared"} {
		t.Run(consumer, func(t *testing.T) {
			job := draJob()
			if consumer != "main" {
				job.Spec.Pod.SideCars = []corev1.Container{{Name: "model", Image: "model:test", Resources: corev1.ResourceRequirements{
					Claims: []corev1.ResourceClaim{{Name: "gpu", Request: "device/subdevice"}},
				}}}
			}
			if consumer == "sidecar" {
				job.Spec.Pod.Container.Resources.Claims = nil
			}
			// Exercise both template-backed and existing-claim sources.
			job.Spec.Pod.ResourceClaims = append(job.Spec.Pod.ResourceClaims, corev1.PodResourceClaim{
				Name: "existing", ResourceClaimName: ptr.To("existing-claim"),
			})
			before := job.DeepCopy()
			require.NoError(t, ValidateUserInput(job))
			pod := CreatePod(&serviceOptions{PodImage: "eval:test", DriverImage: "driver:test"}, job,
				NewDefaultPermissionConfig(), nil, "", logr.Discard())
			require.Equal(t, job.Spec.Pod.ResourceClaims, pod.Spec.ResourceClaims)
			require.Equal(t, *job.Spec.Pod.Container.Resources, pod.Spec.Containers[0].Resources)
			if consumer != "main" {
				require.Equal(t, job.Spec.Pod.SideCars[0], pod.Spec.Containers[1])
			}
			for _, init := range pod.Spec.InitContainers {
				require.Empty(t, init.Resources.Claims, "helper init container must not consume devices")
			}

			pod.Spec.ResourceClaims[0].Name = "changed"
			*pod.Spec.ResourceClaims[0].ResourceClaimTemplateName = "changed-template"
			*pod.Spec.ResourceClaims[1].ResourceClaimName = "changed-claim"
			pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("4")
			pod.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = resource.MustParse("4Gi")
			if len(pod.Spec.Containers[0].Resources.Claims) > 0 {
				pod.Spec.Containers[0].Resources.Claims[0].Request = "changed-request"
			}
			if consumer != "main" {
				pod.Spec.Containers[1].Resources.Claims[0].Name = "changed-sidecar"
			}
			require.Equal(t, before, job, "generated Pod must not alias the input CR")
		})
	}
}

func TestCreatePodWithoutResourceClaims(t *testing.T) {
	for _, name := range []string{"nil-pod", "empty-pod", "empty-claims", "legacy-gpu"} {
		t.Run(name, func(t *testing.T) {
			job := draJob()
			switch name {
			case "nil-pod":
				job.Spec.Pod = nil
			case "empty-pod":
				job.Spec.Pod = &lmesv1alpha1.LMEvalPodSpec{}
			case "empty-claims":
				job.Spec.Pod = &lmesv1alpha1.LMEvalPodSpec{ResourceClaims: []corev1.PodResourceClaim{}}
			case "legacy-gpu":
				job.Spec.Pod.ResourceClaims = nil
				job.Spec.Pod.Container.Resources.Claims = nil
				job.Spec.Pod.Container.Resources.Limits["nvidia.com/gpu"] = resource.MustParse("1")
			}
			require.NoError(t, ValidateUserInput(job))
			pod := CreatePod(&serviceOptions{}, job, NewDefaultPermissionConfig(), nil, "", logr.Discard())
			require.Empty(t, pod.Spec.ResourceClaims)
			require.Empty(t, pod.Spec.Containers[0].Resources.Claims)
			if name == "legacy-gpu" {
				require.Equal(t, job.Spec.Pod.Container.Resources.Limits, pod.Spec.Containers[0].Resources.Limits)
			}
		})
	}
}

func TestValidateResourceClaims(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*lmesv1alpha1.LMEvalPodSpec)
		want   string
	}{
		{"template", func(p *lmesv1alpha1.LMEvalPodSpec) {}, ""},
		{"existing", func(p *lmesv1alpha1.LMEvalPodSpec) {
			p.ResourceClaims[0].ResourceClaimTemplateName = nil
			p.ResourceClaims[0].ResourceClaimName = ptr.To("existing-claim")
		}, ""},
		{"neither-source", func(p *lmesv1alpha1.LMEvalPodSpec) { p.ResourceClaims[0].ResourceClaimTemplateName = nil }, "exactly one"},
		{"both-sources", func(p *lmesv1alpha1.LMEvalPodSpec) { p.ResourceClaims[0].ResourceClaimName = ptr.To("existing") }, "exactly one"},
		{"empty-template", func(p *lmesv1alpha1.LMEvalPodSpec) { p.ResourceClaims[0].ResourceClaimTemplateName = ptr.To("") }, "source"},
		{"bad-template", func(p *lmesv1alpha1.LMEvalPodSpec) {
			p.ResourceClaims[0].ResourceClaimTemplateName = ptr.To("Bad_Template")
		}, "source"},
		{"empty-existing", func(p *lmesv1alpha1.LMEvalPodSpec) {
			p.ResourceClaims[0].ResourceClaimTemplateName = nil
			p.ResourceClaims[0].ResourceClaimName = ptr.To("")
		}, "source"},
		{"bad-alias", func(p *lmesv1alpha1.LMEvalPodSpec) { p.ResourceClaims[0].Name = "GPU" }, "name"},
		{"duplicate-alias", func(p *lmesv1alpha1.LMEvalPodSpec) { p.ResourceClaims = append(p.ResourceClaims, p.ResourceClaims[0]) }, "duplicate"},
		{"missing-main-declaration", func(p *lmesv1alpha1.LMEvalPodSpec) { p.ResourceClaims = nil }, "main"},
		{"dangling-main", func(p *lmesv1alpha1.LMEvalPodSpec) { p.Container.Resources.Claims[0].Name = "missing" }, "main"},
		{"dangling-sidecar", func(p *lmesv1alpha1.LMEvalPodSpec) {
			p.Container = nil
			p.SideCars = []corev1.Container{{Name: "model", Resources: corev1.ResourceRequirements{Claims: []corev1.ResourceClaim{{Name: "missing"}}}}}
		}, "sideCars[0]"},
		{"no-main-resources", func(p *lmesv1alpha1.LMEvalPodSpec) { p.Container.Resources = nil }, ""},
		{"bad-request", func(p *lmesv1alpha1.LMEvalPodSpec) { p.Container.Resources.Claims[0].Request = "BAD" }, "request"},
		{"bad-subrequest", func(p *lmesv1alpha1.LMEvalPodSpec) { p.Container.Resources.Claims[0].Request = "device/" }, "request"},
		{"too-many-request-parts", func(p *lmesv1alpha1.LMEvalPodSpec) { p.Container.Resources.Claims[0].Request = "a/b/c" }, "request"},
		{"subrequest", func(p *lmesv1alpha1.LMEvalPodSpec) { p.Container.Resources.Claims[0].Request = "device/subdevice" }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := draJob()
			tc.change(job.Spec.Pod)
			err := ValidateUserInput(job)
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
}

func TestResumeRejectsInvalidResourceClaims(t *testing.T) {
	// Validation must run before permissions, CA resolution, or Pod creation.
	job := draJob()
	job.Spec.Pod.ResourceClaims = nil
	r := &LMEvalJobReconciler{Client: fake.NewClientBuilder().Build()}
	_, err := r.handleResume(context.Background(), logr.Discard(), job)
	require.ErrorContains(t, err, "resource claims")
	require.ErrorContains(t, err, "main")
}
