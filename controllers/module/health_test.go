package module

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("ServiceHealthChecker", func() {
	Context("RunningServiceChecker", func() {
		var checker *RunningServiceChecker
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			checker = NewRunningServiceChecker("test-service", k8sClient, "default")
		})

		AfterEach(func() {
			if deployment != nil {
				_ = k8sClient.Delete(ctx, deployment)
			}
		})

		It("Should return healthy when deployment has all replicas ready", func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name": "test-service",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(3)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test",
									Image: "test:latest",
								},
							},
						},
					},
				},
				Status: appsv1.DeploymentStatus{
					Replicas:      3,
					ReadyReplicas: 3,
				},
			}

			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Update status subresource
			deployment.Status = appsv1.DeploymentStatus{
				Replicas:      3,
				ReadyReplicas: 3,
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			healthy, reason := checker.IsHealthy(ctx)
			Expect(healthy).To(BeTrue())
			Expect(reason).To(Equal("deployment ready"))
		})

		It("Should return unhealthy when deployment has partial replicas ready", func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name": "test-service",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(3)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test",
									Image: "test:latest",
								},
							},
						},
					},
				},
				Status: appsv1.DeploymentStatus{
					Replicas:      3,
					ReadyReplicas: 1,
				},
			}

			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Update status subresource
			deployment.Status = appsv1.DeploymentStatus{
				Replicas:      3,
				ReadyReplicas: 1,
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			healthy, reason := checker.IsHealthy(ctx)
			Expect(healthy).To(BeFalse())
			Expect(reason).To(Equal("1/3 replicas ready"))
		})

		It("Should return unhealthy when deployment has zero replicas ready", func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name": "test-service",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(3)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test",
									Image: "test:latest",
								},
							},
						},
					},
				},
				Status: appsv1.DeploymentStatus{
					Replicas:      3,
					ReadyReplicas: 0,
				},
			}

			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Update status subresource
			deployment.Status = appsv1.DeploymentStatus{
				Replicas:      3,
				ReadyReplicas: 0,
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			healthy, reason := checker.IsHealthy(ctx)
			Expect(healthy).To(BeFalse())
			Expect(reason).To(Equal("0/3 replicas ready"))
		})

		It("Should return unhealthy when deployment is scaled to zero", func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name": "test-service",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(0)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test",
									Image: "test:latest",
								},
							},
						},
					},
				},
				Status: appsv1.DeploymentStatus{
					Replicas:      0,
					ReadyReplicas: 0,
				},
			}

			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Update status subresource
			deployment.Status = appsv1.DeploymentStatus{
				Replicas:      0,
				ReadyReplicas: 0,
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			healthy, reason := checker.IsHealthy(ctx)
			Expect(healthy).To(BeFalse())
			Expect(reason).To(Equal("deployment scaled to 0 replicas"))
		})

		It("Should return unhealthy when deployment does not exist", func() {
			checker = NewRunningServiceChecker("nonexistent-service", k8sClient, "default")

			healthy, reason := checker.IsHealthy(ctx)
			Expect(healthy).To(BeFalse())
			Expect(reason).To(Equal("deployment not found"))
		})
	})
})
