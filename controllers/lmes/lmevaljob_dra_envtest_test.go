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

package lmes_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var _ = Describe("LMEvalJob DRA", func() {
	newJob := func(name string) *lmesv1alpha1.LMEvalJob {
		return &lmesv1alpha1.LMEvalJob{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: lmesv1alpha1.LMEvalJobSpec{
				Model:       "hf",
				AllowOnline: ptr.To(true),
				TaskList:    lmesv1alpha1.TaskList{TaskNames: []string{"arc_easy"}},
				Pod: &lmesv1alpha1.LMEvalPodSpec{
					ResourceClaims: []corev1.PodResourceClaim{{Name: "gpu", ResourceClaimTemplateName: ptr.To("gpu-template")}},
					Container: &lmesv1alpha1.LMEvalContainer{Resources: &corev1.ResourceRequirements{
						Claims: []corev1.ResourceClaim{{Name: "gpu", Request: "device"}},
					}},
				},
			},
		}
	}

	It("preserves both claim levels through CR admission and reconciliation", func() {
		clientset, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		serverVersion, err := clientset.Discovery().ServerVersion()
		Expect(err).NotTo(HaveOccurred())
		if !version.MustParseGeneric(serverVersion.GitVersion).AtLeast(version.MustParseGeneric("1.34.0")) {
			Skip("native Pod claim propagation requires the Kubernetes 1.34+ DRA baseline")
		}

		job := newJob("dra-propagation")
		job.Spec.Pod.ResourceClaims = append(job.Spec.Pod.ResourceClaims, corev1.PodResourceClaim{
			Name: "model-gpu", ResourceClaimName: ptr.To("model-claim"),
		})
		job.Spec.Pod.SideCars = []corev1.Container{{Name: "model", Image: "model:test", Resources: corev1.ResourceRequirements{
			Claims: []corev1.ResourceClaim{{Name: "model-gpu"}},
		}}}
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
		stored := &lmesv1alpha1.LMEvalJob{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, stored)).To(Succeed())
		Expect(stored.Spec.Pod.ResourceClaims).To(Equal(job.Spec.Pod.ResourceClaims))
		Expect(stored.Spec.Pod.Container.Resources.Claims).To(Equal(job.Spec.Pod.Container.Resources.Claims))

		pod := &corev1.Pod{}
		WaitFor(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: job.GetPodName(), Namespace: job.Namespace}, pod)
		}, "DRA job Pod was not created")
		Expect(pod.Spec.ResourceClaims).To(Equal(job.Spec.Pod.ResourceClaims))
		Expect(pod.Spec.Containers[0].Resources.Claims).To(Equal(job.Spec.Pod.Container.Resources.Claims))
		Expect(pod.Spec.Containers[1].Resources.Claims).To(Equal(job.Spec.Pod.SideCars[0].Resources.Claims))
		Expect(pod.Spec.InitContainers[0].Resources.Claims).To(BeEmpty())

		// An edit does not patch the running Pod's immutable allocation contract.
		WaitFor(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, stored); err != nil {
				return err
			}
			if stored.Status.State != lmesv1alpha1.ScheduledJobState {
				return fmt.Errorf("waiting for Scheduled state: %s", stored.Status.State)
			}
			stored.Spec.Pod.ResourceClaims[0].ResourceClaimTemplateName = ptr.To("different-template")
			return k8sClient.Update(ctx, stored)
		}, "could not update the scheduled CR")
		Consistently(func() []corev1.PodResourceClaim {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: job.GetPodName(), Namespace: job.Namespace}, pod)).To(Succeed())
			return pod.Spec.ResourceClaims
		}, "1s", defaultPolling).Should(Equal(job.Spec.Pod.ResourceClaims))
	})

	It("rejects invalid claim sources and duplicate aliases at CR admission", func() {
		for _, tc := range []struct {
			name   string
			claims []corev1.PodResourceClaim
		}{
			{"no-source", []corev1.PodResourceClaim{{Name: "gpu"}}},
			{"both-sources", []corev1.PodResourceClaim{{Name: "gpu", ResourceClaimName: ptr.To("claim"), ResourceClaimTemplateName: ptr.To("template")}}},
			{"empty-template", []corev1.PodResourceClaim{{Name: "gpu", ResourceClaimTemplateName: ptr.To("")}}},
			{"empty-claim", []corev1.PodResourceClaim{{Name: "gpu", ResourceClaimName: ptr.To("")}}},
			{"duplicate", []corev1.PodResourceClaim{{Name: "gpu", ResourceClaimTemplateName: ptr.To("a")}, {Name: "gpu", ResourceClaimTemplateName: ptr.To("b")}}},
		} {
			job := newJob("dra-invalid-" + tc.name)
			job.Spec.Pod.ResourceClaims = tc.claims
			err := k8sClient.Create(ctx, job)
			Expect(apierrors.IsInvalid(err)).To(BeTrue(), "case %s: %v", tc.name, err)
		}
	})

	It("reports dangling references without creating an evaluation Pod", func() {
		job := newJob("dra-dangling")
		job.Spec.Pod.Container.Resources.Claims[0].Name = "undeclared"
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
		stored := &lmesv1alpha1.LMEvalJob{}
		WaitFor(func() error {
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, stored); err != nil {
				return err
			}
			if stored.Status.State != lmesv1alpha1.CompleteJobState {
				return fmt.Errorf("waiting for validation failure: %s", stored.Status.State)
			}
			return nil
		}, "job did not report invalid claim references")
		Expect(stored.Status.Reason).To(Equal(lmesv1alpha1.FailedReason))
		Expect(stored.Status.Message).To(ContainSubstring("resource claims"))
		Expect(stored.Status.Message).To(ContainSubstring("undeclared"))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx,
			types.NamespacedName{Name: job.GetPodName(), Namespace: job.Namespace}, &corev1.Pod{}))).To(BeTrue())
	})
})
