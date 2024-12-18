package guardrails

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	guardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/guardrails/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg        *rest.Config
	k8sClient  client.Client
	testEnv    *envtest.Environment
	ctx        context.Context
	reconciler *GuardrailsReconciler
	recorder   *record.FakeRecorder
)

const (
	defaultOrchestratorName = "example-guardrails-orchestrator"
	defaultNamespace        = "default"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Guardrails Suite")
}

const (
	timeout  = 10 * time.Second
	interval = 250 * time.Millisecond
)

func WaitFor(operation func() error, errMsg string) {
	Eventually(operation, timeout, interval).Should(Succeed(), errMsg)
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = guardrailsv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = routev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	recorder := k8sManager.GetEventRecorderFor("guardrails-controller")

	err = (&GuardrailsReconciler{
		Client:        k8sManager.GetClient(),
		Scheme:        k8sManager.GetScheme(),
		EventRecorder: recorder,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() error {
		return createNamespace(context.Background(), k8sManager.GetClient(), defaultNamespace)
	}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create namespace")

	err = k8sClient.Create(context.Background(), &guardrailsv1alpha1.GuardrailsOrchestrator{})
})

func createNamespace(ctx context.Context, k8sClient client.Client, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	err := k8sClient.Create(ctx, ns)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create namespace: %w", err)
	}
	return nil
}

func makeDeploymentReady(ctx context.Context, k8sClient client.Client, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {
	deployment := &appsv1.Deployment{}
	deploymentName := types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}

	if err := k8sClient.Get(ctx, deploymentName, deployment); err != nil {
		return err
	}

	deployment.Status.Conditions = []appsv1.DeploymentCondition{
		{
			Type:    appsv1.DeploymentAvailable,
			Status:  corev1.ConditionTrue,
			Reason:  "DeploymentReady",
			Message: "Deployment is ready",
		},
		{
			Type:    appsv1.DeploymentProgressing,
			Status:  corev1.ConditionTrue,
			Reason:  "NewReplicaSetAvailable",
			Message: "ReplicaSet is progressing",
		},
	}

	if deployment.Spec.Replicas == nil {
		deployment.Status.Replicas = *deployment.Spec.Replicas
		deployment.Status.ReadyReplicas = *deployment.Spec.Replicas
		deployment.Status.AvailableReplicas = *deployment.Spec.Replicas
	}

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orchestrator.Name,
			Namespace: orchestrator.Namespace,
			Labels:    deployment.Spec.Selector.MatchLabels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  OrchestratorName,
					Image: "",
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 8033,
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionTrue,
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: orchestrator.Name,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
					Ready:        true,
					RestartCount: 0,
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, &pod); err != nil {
		return err
	}

	if err := k8sClient.Status().Update(ctx, deployment); err != nil {
		return err
	}

	return nil
}

func makerouteReady(ctx context.Context, k8sClient client.Client, orchestrator *guardrailsv1alpha1.GuardrailsOrchestrator) error {
	route := &routev1.Route{}
	routeName := types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}

	if err := k8sClient.Get(ctx, routeName, route); err != nil {
		return err
	}

	route.Status.Ingress = []routev1.RouteIngress{
		{
			Conditions: []routev1.RouteIngressCondition{
				{
					Type:   routev1.RouteAdmitted,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	return k8sClient.Status().Update(ctx, route)
}

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
