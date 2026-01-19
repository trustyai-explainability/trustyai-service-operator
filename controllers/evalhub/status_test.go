package evalhub

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("EvalHub Status Updates", func() {
	const (
		testNamespacePrefix = "evalhub-status-test"
		evalHubName         = "status-evalhub"
	)

	var (
		testNamespace string
		namespace     *corev1.Namespace
		evalHub       *evalhubv1alpha1.EvalHub
		deployment    *appsv1.Deployment
		reconciler    *EvalHubReconciler
	)

	BeforeEach(func() {
		// Create unique namespace name to avoid conflicts
		testNamespace = fmt.Sprintf("%s-%d", testNamespacePrefix, time.Now().UnixNano())

		// Create test namespace
		namespace = createNamespace(testNamespace)
		Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

		// Create EvalHub instance
		evalHub = createEvalHubInstance(evalHubName, testNamespace)
		Expect(k8sClient.Create(ctx, evalHub)).Should(Succeed())

		// Create deployment for testing
		deployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      evalHubName,
				Namespace: testNamespace,
				Labels: map[string]string{
					"app":       "eval-hub",
					"instance":  evalHubName,
					"component": "api",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: evalHub.Spec.Replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":       "eval-hub",
						"instance":  evalHubName,
						"component": "api",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":       "eval-hub",
							"instance":  evalHubName,
							"component": "api",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "evalhub",
								Image: "test-image",
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

		// Setup reconciler
		reconciler, _ = setupReconciler(testNamespace)
	})

	AfterEach(func() {
		// Clean up resources in namespace first
		if deployment != nil {
			k8sClient.Delete(ctx, deployment)
		}
		cleanupResourcesInNamespace(testNamespace, evalHub, nil)

		// Then delete namespace
		deleteNamespace(namespace)

		// Reset variables
		deployment = nil
		evalHub = nil
		namespace = nil
	})

	Context("When updating EvalHub status", func() {
		It("should update status to Pending when deployment is not ready", func() {
			By("Setting deployment status to not ready")
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 0
			err := k8sClient.Status().Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Updating EvalHub status")
			err = reconciler.updateStatus(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Checking updated status")
			updatedEvalHub := &evalhubv1alpha1.EvalHub{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())

			Expect(updatedEvalHub.Status.Phase).To(Equal("Pending"))
			Expect(updatedEvalHub.Status.Ready).To(Equal(corev1.ConditionFalse))
			Expect(updatedEvalHub.Status.Replicas).To(Equal(int32(1)))
			Expect(updatedEvalHub.Status.ReadyReplicas).To(Equal(int32(0)))
		})

		It("should update status to Ready when deployment is ready", func() {
			By("Setting deployment status to ready")
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 1
			err := k8sClient.Status().Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Updating EvalHub status")
			err = reconciler.updateStatus(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Checking updated status")
			updatedEvalHub := &evalhubv1alpha1.EvalHub{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())

			Expect(updatedEvalHub.Status.Phase).To(Equal("Ready"))
			Expect(updatedEvalHub.Status.Ready).To(Equal(corev1.ConditionTrue))
			Expect(updatedEvalHub.Status.Replicas).To(Equal(int32(1)))
			Expect(updatedEvalHub.Status.ReadyReplicas).To(Equal(int32(1)))

			expectedURL := fmt.Sprintf("https://%s.%s.svc.cluster.local:%d", evalHubName, testNamespace, 8443)
			Expect(updatedEvalHub.Status.URL).To(Equal(expectedURL))
		})

		It("should handle partial readiness correctly", func() {
			By("Setting deployment status to partially ready")
			replicas := int32(3)
			evalHub.Spec.Replicas = &replicas
			err := k8sClient.Update(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			deployment.Status.Replicas = 3
			deployment.Status.ReadyReplicas = 2
			err = k8sClient.Status().Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Updating EvalHub status")
			err = reconciler.updateStatus(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Checking updated status")
			updatedEvalHub := &evalhubv1alpha1.EvalHub{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())

			Expect(updatedEvalHub.Status.Phase).To(Equal("Pending"))
			Expect(updatedEvalHub.Status.Ready).To(Equal(corev1.ConditionFalse))
			Expect(updatedEvalHub.Status.Replicas).To(Equal(int32(3)))
			Expect(updatedEvalHub.Status.ReadyReplicas).To(Equal(int32(2)))
		})

		It("should update status when deployment becomes ready", func() {
			By("Starting with deployment not ready")
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 0
			err := k8sClient.Status().Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			err = reconciler.updateStatus(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying initial status is Pending")
			updatedEvalHub := &evalhubv1alpha1.EvalHub{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedEvalHub.Status.Phase).To(Equal("Pending"))

			By("Making deployment ready")
			deployment.Status.ReadyReplicas = 1
			err = k8sClient.Status().Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			err = reconciler.updateStatus(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status is now Ready")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedEvalHub.Status.Phase).To(Equal("Ready"))
			Expect(updatedEvalHub.Status.Ready).To(Equal(corev1.ConditionTrue))
		})

		It("should handle missing deployment", func() {
			By("Deleting deployment")
			err := k8sClient.Delete(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for deployment to be deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      evalHubName,
					Namespace: testNamespace,
				}, &appsv1.Deployment{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("Updating EvalHub status")
			err = reconciler.updateStatus(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Checking status reflects missing deployment")
			updatedEvalHub := &evalhubv1alpha1.EvalHub{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())

			Expect(updatedEvalHub.Status.Phase).To(Equal("Pending"))
			Expect(updatedEvalHub.Status.Ready).To(Equal(corev1.ConditionFalse))
		})

		It("should set correct URL when ready", func() {
			By("Setting deployment status to ready")
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 1
			err := k8sClient.Status().Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Updating EvalHub status")
			err = reconciler.updateStatus(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			By("Checking URL is set correctly")
			updatedEvalHub := &evalhubv1alpha1.EvalHub{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())

			expectedURL := fmt.Sprintf("https://%s.%s.svc.cluster.local:8443", evalHubName, testNamespace)
			Expect(updatedEvalHub.Status.URL).To(Equal(expectedURL))
		})
	})

	Context("When handling status update errors", func() {
		It("should return error when EvalHub doesn't exist", func() {
			By("Creating EvalHub that doesn't exist in cluster")
			nonExistentEvalHub := &evalhubv1alpha1.EvalHub{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent",
					Namespace: testNamespace,
				},
			}

			By("Attempting to update status")
			err := reconciler.updateStatus(ctx, nonExistentEvalHub)
			Expect(err).To(HaveOccurred())
		})

		It("should handle deployment access errors gracefully", func() {
			By("Updating status in non-existent namespace")
			badEvalHub := createEvalHubInstance("bad-status-evalhub", "non-existent-namespace")
			badReconciler, _ := setupReconciler("non-existent-namespace")

			err := badReconciler.updateStatus(ctx, badEvalHub)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Status conditions", func() {
		It("should set appropriate condition messages for different states", func() {
			By("Testing deployment not ready state")
			deployment.Status.Replicas = 2
			deployment.Status.ReadyReplicas = 1
			err := k8sClient.Status().Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			err = reconciler.updateStatus(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			updatedEvalHub := &evalhubv1alpha1.EvalHub{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())

			// Check condition message indicates waiting state
			conditions := updatedEvalHub.Status.Conditions
			Expect(conditions).NotTo(BeEmpty())

			var readyCondition *common.Condition
			for i := range conditions {
				if conditions[i].Type == "Ready" {
					readyCondition = &conditions[i]
					break
				}
			}
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(corev1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("DeploymentNotReady"))
			Expect(readyCondition.Message).To(ContainSubstring("Waiting for deployment to be ready"))
		})

		It("should set ready condition when all replicas are ready", func() {
			By("Setting deployment to fully ready")
			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 1
			err := k8sClient.Status().Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			err = reconciler.updateStatus(ctx, evalHub)
			Expect(err).NotTo(HaveOccurred())

			updatedEvalHub := &evalhubv1alpha1.EvalHub{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      evalHubName,
				Namespace: testNamespace,
			}, updatedEvalHub)
			Expect(err).NotTo(HaveOccurred())

			// Check condition message indicates ready state
			conditions := updatedEvalHub.Status.Conditions
			Expect(conditions).NotTo(BeEmpty())

			var readyCondition *common.Condition
			for i := range conditions {
				if conditions[i].Type == "Ready" {
					readyCondition = &conditions[i]
					break
				}
			}
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(corev1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal("DeploymentReady"))
			Expect(readyCondition.Message).To(Equal("All replicas are ready"))
		})
	})
})
