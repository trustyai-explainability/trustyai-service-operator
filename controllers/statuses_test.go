package controllers

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func checkCondition(conditions []trustyaiopendatahubiov1alpha1.Condition, conditionType string, expectedStatus corev1.ConditionStatus, allowMissing bool) (*trustyaiopendatahubiov1alpha1.Condition, bool, error) {
	for _, cond := range conditions {
		if cond.Type == conditionType {
			isExpectedStatus := cond.Status == expectedStatus
			return &cond, isExpectedStatus, nil
		}
	}
	if allowMissing {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("%s condition not found", conditionType)
}

func setupAndTestStatusNoComponent(instance *trustyaiopendatahubiov1alpha1.TrustyAIService, namespace string) {
	WaitFor(func() error {
		return createNamespace(ctx, k8sClient, namespace)
	}, "failed to create namespace")

	// Call the reconcileStatuses function
	_, _ = reconciler.reconcileStatuses(ctx, instance)

	readyCondition, statusMatch, err := checkCondition(instance.Status.Conditions, "Ready", corev1.ConditionTrue, true)
	Expect(err).NotTo(HaveOccurred(), "Error checking Ready condition")
	if readyCondition != nil {
		Expect(statusMatch).To(Equal(corev1.ConditionFalse), "Ready condition should be true")
	}

	availableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeAvailable, corev1.ConditionFalse, true)
	Expect(err).NotTo(HaveOccurred(), "Error checking Available condition")
	if availableCondition != nil {
		Expect(statusMatch).To(Equal(corev1.ConditionFalse), "Available condition should be false")
	}
}

var _ = Describe("Status and condition tests", func() {

	BeforeEach(func() {
		k8sClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		recorder = record.NewFakeRecorder(10)
		reconciler = &TrustyAIServiceReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: recorder,
		}
		ctx = context.Background()
	})

	Context("When no component exists", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("Should not be available in PVC-mode", func() {
			namespace := "statuses-test-namespace-1-pvc"
			instance = createDefaultPVCCustomResource(namespace)
			setupAndTestStatusNoComponent(instance, namespace)
		})
		It("Should not be available in DB-mode", func() {
			namespace := "statuses-test-namespace-1-db"
			instance = createDefaultDBCustomResource(namespace)
			setupAndTestStatusNoComponent(instance, namespace)
		})
		It("Should not be available in migration-mode", func() {
			namespace := "statuses-test-namespace-1-migration"
			instance = createDefaultMigrationCustomResource(namespace)
			setupAndTestStatusNoComponent(instance, namespace)
		})

	})

	Context("When route, deployment and PVC component, but not inference service, exist", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("Should be available in PVC-mode", func() {
			namespace := "statuses-test-namespace-2-pvc"
			instance = createDefaultPVCCustomResource(namespace)
			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")
			caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

			WaitFor(func() error {
				return reconciler.reconcileRouteAuth(instance, ctx, reconciler.createRouteObject)
			}, "failed to create route")
			WaitFor(func() error {
				return makeRouteReady(ctx, k8sClient, instance)
			}, "failed to make route ready")
			WaitFor(func() error {
				return reconciler.ensurePVC(ctx, instance)
			}, "failed to create PVC")
			WaitFor(func() error {
				return makePVCReady(ctx, k8sClient, instance)
			}, "failed to bind PVC")
			WaitFor(func() error {
				return reconciler.ensureDeployment(ctx, instance, caBundle, false)
			}, "failed to create deployment")
			WaitFor(func() error {
				return makeDeploymentReady(ctx, k8sClient, instance)
			}, "failed to make deployment ready")
			WaitFor(func() error {
				return k8sClient.Create(ctx, instance)
			}, "failed to create TrustyAIService")

			// Call the reconcileStatuses function
			WaitFor(func() error {
				_, err := reconciler.reconcileStatuses(ctx, instance)
				return err
			}, "failed to update statuses")

			// Fetch the updated instance
			WaitFor(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      instance.Name,
					Namespace: instance.Namespace,
				}, instance)
			}, "failed to get updated instance")

			readyCondition, statusMatch, err := checkCondition(instance.Status.Conditions, "Ready", corev1.ConditionTrue, true)
			Expect(err).NotTo(HaveOccurred(), "Error checking Ready condition")
			if readyCondition != nil {
				Expect(statusMatch).To(Equal(corev1.ConditionTrue), "Ready condition should be true")
			}

			availableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking Available condition")
			Expect(availableCondition).NotTo(BeNil(), "Available condition should not be null")
			Expect(statusMatch).To(Equal(true), "Ready condition should be true")

			routeAvailableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeRouteAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking RouteAvailable condition")
			Expect(routeAvailableCondition).NotTo(BeNil(), "RouteAvailable condition should not be null")
			Expect(statusMatch).To(Equal(true), "RouteAvailable condition should be true")

			pvcAvailableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypePVCAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking PVCAvailable condition")
			Expect(pvcAvailableCondition).NotTo(BeNil(), "PVCAvailable condition should not be null")
			Expect(statusMatch).To(Equal(true), "PVCAvailable condition should be true")

			ISPresentCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeInferenceServicesPresent, corev1.ConditionFalse, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking InferenceServicePresent condition")
			Expect(ISPresentCondition).NotTo(BeNil(), "InferenceServicePresent condition should not be null")
			Expect(statusMatch).To(Equal(true), "InferenceServicePresent condition should be false")
		})
		It("Should be available in DB-mode", func() {
			namespace := "statuses-test-namespace-2-db"
			instance = createDefaultDBCustomResource(namespace)
			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")
			caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

			WaitFor(func() error {
				return reconciler.reconcileRouteAuth(instance, ctx, reconciler.createRouteObject)
			}, "failed to create route")
			WaitFor(func() error {
				return makeRouteReady(ctx, k8sClient, instance)
			}, "failed to make route ready")
			WaitFor(func() error {
				return reconciler.ensureDeployment(ctx, instance, caBundle, false)
			}, "failed to create deployment")
			WaitFor(func() error {
				return makeDeploymentReady(ctx, k8sClient, instance)
			}, "failed to make deployment ready")
			WaitFor(func() error {
				return k8sClient.Create(ctx, instance)
			}, "failed to create TrustyAIService")

			// Call the reconcileStatuses function
			WaitFor(func() error {
				_, err := reconciler.reconcileStatuses(ctx, instance)
				return err
			}, "failed to update statuses")

			// Fetch the updated instance
			WaitFor(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      instance.Name,
					Namespace: instance.Namespace,
				}, instance)
			}, "failed to get updated instance")

			readyCondition, statusMatch, err := checkCondition(instance.Status.Conditions, "Ready", corev1.ConditionTrue, true)
			Expect(err).NotTo(HaveOccurred(), "Error checking Ready condition")
			if readyCondition != nil {
				Expect(statusMatch).To(Equal(corev1.ConditionTrue), "Ready condition should be true")
			}

			availableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking Available condition")
			Expect(availableCondition).NotTo(BeNil(), "Available condition should not be null")
			Expect(statusMatch).To(Equal(true), "Ready condition should be true")

			routeAvailableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeRouteAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking RouteAvailable condition")
			Expect(routeAvailableCondition).NotTo(BeNil(), "RouteAvailable condition should not be null")
			Expect(statusMatch).To(Equal(true), "RouteAvailable condition should be true")

			pvcAvailableCondition, _, err := checkCondition(instance.Status.Conditions, StatusTypePVCAvailable, corev1.ConditionTrue, false)
			Expect(err).To(HaveOccurred(), "Error checking PVCAvailable condition")
			Expect(pvcAvailableCondition).To(BeNil(), "PVCAvailable condition should be null")

			ISPresentCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeInferenceServicesPresent, corev1.ConditionFalse, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking InferenceServicePresent condition")
			Expect(ISPresentCondition).NotTo(BeNil(), "InferenceServicePresent condition should not be null")
			Expect(statusMatch).To(Equal(true), "InferenceServicePresent condition should be false")

		})
		It("Should be available in migration-mode", func() {
			namespace := "statuses-test-namespace-2-migration"
			instance = createDefaultMigrationCustomResource(namespace)
			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")
			caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

			WaitFor(func() error {
				return reconciler.reconcileRouteAuth(instance, ctx, reconciler.createRouteObject)
			}, "failed to create route")
			WaitFor(func() error {
				return makeRouteReady(ctx, k8sClient, instance)
			}, "failed to make route ready")
			WaitFor(func() error {
				return reconciler.ensurePVC(ctx, instance)
			}, "failed to create PVC")
			WaitFor(func() error {
				return makePVCReady(ctx, k8sClient, instance)
			}, "failed to bind PVC")
			WaitFor(func() error {
				return reconciler.ensureDeployment(ctx, instance, caBundle, false)
			}, "failed to create deployment")
			WaitFor(func() error {
				return makeDeploymentReady(ctx, k8sClient, instance)
			}, "failed to make deployment ready")
			WaitFor(func() error {
				return k8sClient.Create(ctx, instance)
			}, "failed to create TrustyAIService")

			// Call the reconcileStatuses function
			WaitFor(func() error {
				_, err := reconciler.reconcileStatuses(ctx, instance)
				return err
			}, "failed to update statuses")

			// Fetch the updated instance
			WaitFor(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      instance.Name,
					Namespace: instance.Namespace,
				}, instance)
			}, "failed to get updated instance")

			readyCondition, statusMatch, err := checkCondition(instance.Status.Conditions, "Ready", corev1.ConditionTrue, true)
			Expect(err).NotTo(HaveOccurred(), "Error checking Ready condition")
			if readyCondition != nil {
				Expect(statusMatch).To(Equal(corev1.ConditionTrue), "Ready condition should be true")
			}

			availableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking Available condition")
			Expect(availableCondition).NotTo(BeNil(), "Available condition should not be null")
			Expect(statusMatch).To(Equal(true), "Ready condition should be true")

			routeAvailableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeRouteAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking RouteAvailable condition")
			Expect(routeAvailableCondition).NotTo(BeNil(), "RouteAvailable condition should not be null")
			Expect(statusMatch).To(Equal(true), "RouteAvailable condition should be true")

			pvcAvailableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypePVCAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking PVCAvailable condition")
			Expect(pvcAvailableCondition).NotTo(BeNil(), "PVCAvailable condition should not be null")
			Expect(statusMatch).To(Equal(true), "PVCAvailable condition should be true")

			ISPresentCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeInferenceServicesPresent, corev1.ConditionFalse, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking InferenceServicePresent condition")
			Expect(ISPresentCondition).NotTo(BeNil(), "InferenceServicePresent condition should not be null")
			Expect(statusMatch).To(Equal(true), "InferenceServicePresent condition should be false")

		})
	})

	Context("When route, deployment, PVC and inference service components exist", func() {
		var instance *trustyaiopendatahubiov1alpha1.TrustyAIService
		It("Should be available", func() {
			namespace := "statuses-test-namespace-2"
			instance = createDefaultPVCCustomResource(namespace)
			WaitFor(func() error {
				return createNamespace(ctx, k8sClient, namespace)
			}, "failed to create namespace")
			caBundle := reconciler.GetCustomCertificatesBundle(ctx, instance)

			WaitFor(func() error {
				return reconciler.reconcileRouteAuth(instance, ctx, reconciler.createRouteObject)
			}, "failed to create route")
			WaitFor(func() error {
				return makeRouteReady(ctx, k8sClient, instance)
			}, "failed to make route ready")
			WaitFor(func() error {
				return reconciler.ensurePVC(ctx, instance)
			}, "failed to create PVC")
			WaitFor(func() error {
				return makePVCReady(ctx, k8sClient, instance)
			}, "failed to bind PVC")
			WaitFor(func() error {
				return reconciler.ensureDeployment(ctx, instance, caBundle, false)
			}, "failed to create deployment")
			WaitFor(func() error {
				return makeDeploymentReady(ctx, k8sClient, instance)
			}, "failed to make deployment ready")

			inferenceService := createInferenceService("my-model", namespace)
			WaitFor(func() error {
				return k8sClient.Create(ctx, inferenceService)
			}, "failed to create InferenceService")

			Expect(reconciler.patchKServe(ctx, instance, *inferenceService, namespace, instance.Name, false)).ToNot(HaveOccurred())

			WaitFor(func() error {
				return k8sClient.Create(ctx, instance)
			}, "failed to create TrustyAIService")

			// Call the reconcileStatuses function
			WaitFor(func() error {
				_, err := reconciler.reconcileStatuses(ctx, instance)
				return err
			}, "failed to update statuses")

			// Fetch the updated instance
			WaitFor(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      instance.Name,
					Namespace: instance.Namespace,
				}, instance)
			}, "failed to get updated instance")

			readyCondition, statusMatch, err := checkCondition(instance.Status.Conditions, "Ready", corev1.ConditionTrue, true)
			Expect(err).NotTo(HaveOccurred(), "Error checking Ready condition")
			if readyCondition != nil {
				Expect(statusMatch).To(Equal(corev1.ConditionTrue), "Ready condition should be true")
			}

			availableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking Available condition")
			Expect(availableCondition).NotTo(BeNil(), "Available condition should not be null")
			Expect(statusMatch).To(Equal(true), "Ready condition should be true")

			routeAvailableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeRouteAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking RouteAvailable condition")
			Expect(routeAvailableCondition).NotTo(BeNil(), "RouteAvailable condition should not be null")
			Expect(statusMatch).To(Equal(true), "RouteAvailable condition should be true")

			pvcAvailableCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypePVCAvailable, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking PVCAvailable condition")
			Expect(pvcAvailableCondition).NotTo(BeNil(), "PVCAvailable condition should not be null")
			Expect(statusMatch).To(Equal(true), "PVCAvailable condition should be true")

			ISPresentCondition, statusMatch, err := checkCondition(instance.Status.Conditions, StatusTypeInferenceServicesPresent, corev1.ConditionTrue, false)
			Expect(err).NotTo(HaveOccurred(), "Error checking InferenceServicePresent condition")
			Expect(ISPresentCondition).NotTo(BeNil(), "InferenceServicePresent condition should not be null")
			Expect(statusMatch).To(Equal(true), "InferenceServicePresent condition should be true")

		})
	})

})
