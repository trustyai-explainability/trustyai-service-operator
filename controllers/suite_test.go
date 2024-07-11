/*
Copyright 2023.

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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	rbacv1 "k8s.io/api/rbac/v1"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	trustyaiopendatahubiov1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg        *rest.Config
	k8sClient  client.Client
	testEnv    *envtest.Environment
	ctx        context.Context
	cancel     context.CancelFunc
	reconciler *TrustyAIServiceReconciler
	recorder   *record.FakeRecorder
)

const (
	defaultServiceName               = "example-trustyai-service"
	defaultDatabaseConfigurationName = defaultServiceName + "-db-credentials"
	operatorNamespace                = "system"
)

const (
	defaultTimeout = time.Second * 10
	defaultPolling = time.Millisecond * 250
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

// WaitFor is a function that takes a function which returns an error, and an error message.
// It will repeatedly call the provided function until it succeeds or the timeout is reached.
func WaitFor(operation func() error, errorMsg string) {
	Eventually(operation, defaultTimeout, defaultPolling).Should(Succeed(), errorMsg)
}

// createDefaultPVCCustomResource creates a TrustyAIService instance with default values and PVC backend
func createDefaultPVCCustomResource(namespaceCurrent string) *trustyaiopendatahubiov1alpha1.TrustyAIService {
	service := trustyaiopendatahubiov1alpha1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultServiceName,
			Namespace: namespaceCurrent,
			UID:       types.UID(uuid.New().String()),
		},
		Spec: trustyaiopendatahubiov1alpha1.TrustyAIServiceSpec{
			Storage: trustyaiopendatahubiov1alpha1.StorageSpec{
				Format: STORAGE_PVC,
				Folder: "/data",
				Size:   "1Gi",
			},
			Data: trustyaiopendatahubiov1alpha1.DataSpec{
				Filename: "data.csv",
				Format:   "CSV",
			},
			Metrics: trustyaiopendatahubiov1alpha1.MetricsSpec{
				Schedule: "5s",
			},
		},
	}
	return &service
}

// createDefaultDBCustomResource creates a TrustyAIService instance with default values and DB backend
func createDefaultDBCustomResource(namespaceCurrent string) *trustyaiopendatahubiov1alpha1.TrustyAIService {
	service := trustyaiopendatahubiov1alpha1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultServiceName,
			Namespace: namespaceCurrent,
			UID:       types.UID(uuid.New().String()),
		},
		Spec: trustyaiopendatahubiov1alpha1.TrustyAIServiceSpec{
			Storage: trustyaiopendatahubiov1alpha1.StorageSpec{
				Format:                 STORAGE_DATABASE,
				DatabaseConfigurations: defaultDatabaseConfigurationName,
			},
			Metrics: trustyaiopendatahubiov1alpha1.MetricsSpec{
				Schedule: "5s",
			},
		},
	}
	return &service
}

// createDefaultMigrationCustomResource creates a TrustyAIService instance with default values and both PVC and DB backend
func createDefaultMigrationCustomResource(namespaceCurrent string) *trustyaiopendatahubiov1alpha1.TrustyAIService {
	service := trustyaiopendatahubiov1alpha1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultServiceName,
			Namespace: namespaceCurrent,
			UID:       types.UID(uuid.New().String()),
		},
		Spec: trustyaiopendatahubiov1alpha1.TrustyAIServiceSpec{
			Storage: trustyaiopendatahubiov1alpha1.StorageSpec{
				Format:                 STORAGE_DATABASE,
				DatabaseConfigurations: defaultDatabaseConfigurationName,
				Folder:                 "/data",
				Size:                   "1Gi",
			},
			Data: trustyaiopendatahubiov1alpha1.DataSpec{
				Filename: "data.csv",
				Format:   "CSV",
			},
			Metrics: trustyaiopendatahubiov1alpha1.MetricsSpec{
				Schedule: "5s",
			},
		},
	}
	return &service
}

// createNamespace creates a new namespace
func createNamespace(ctx context.Context, k8sClient client.Client, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	err := k8sClient.Create(ctx, ns)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Handle the case where the namespace already exists
			return nil
		}
		// Handle other errors
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	return nil
}

// createConfigMap creates a configuration in the specified namespace
func createConfigMap(namespace string, oauthImage string, trustyaiServiceImage string) *corev1.ConfigMap {
	// Define the ConfigMap with the necessary data
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageConfigMap,
			Namespace: namespace,
		},
		Data: map[string]string{
			configMapOAuthProxyImageKey: oauthImage,
			configMapServiceImageKey:    trustyaiServiceImage,
		},
	}
}

// createSecret creates a secret in the specified namespace
func createSecret(namespace string, secretName string, data map[string]string) *corev1.Secret {
	// Convert the data map values from string to byte array
	byteData := make(map[string][]byte)
	for key, value := range data {
		byteData[key] = []byte(value)
	}

	// Define the Secret with the necessary data
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: byteData,
	}
}

func createDatabaseConfiguration(namespace string, name string, dbKind string) *corev1.Secret {
	return createSecret(namespace, name, map[string]string{
		"databaseKind":     dbKind,
		"databaseUsername": "foo",
		"databasePassword": "bar",
		"databaseService":  "mariadb-service",
		"databasePort":     "3306",
	})
}

// createTrustedCABundleConfigMap creates a ConfigMap in the specified namespace
// with the label to inject the trusted CA bundle by OpenShift
func createTrustedCABundleConfigMap(namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      caBundleName,
			Namespace: namespace,
			Labels: map[string]string{
				caBundleAnnotation: "true",
			},
		},
		Data: map[string]string{},
	}
}

func createMockPV(ctx context.Context, k8sClient client.Client, pvName string, size string) error {
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
		},
		Spec: corev1.PersistentVolumeSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(size),
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/tmp/" + pvName,
				},
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
		},
	}

	err := k8sClient.Create(ctx, pv)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// PV already exists
			return nil
		}
		// Other errors
		return fmt.Errorf("failed to create PV: %w", err)
	}

	return nil
}

func createTestPVC(ctx context.Context, k8sClient client.Client, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-pvc",
			Namespace: instance.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "trustyai.opendatahub.io/v1alpha1",
					Kind:               "TrustyAIService",
					Name:               instance.Name,
					UID:                instance.UID,
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				},
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
			StorageClassName: pointer.String("standard"),
			VolumeMode:       (*corev1.PersistentVolumeMode)(pointer.String("Filesystem")),
		},
	}

	if err := k8sClient.Create(ctx, pvc); err != nil {
		return fmt.Errorf("failed to create PVC: %v", err)
	}

	return nil
}

// createInferenceService Function to create the InferenceService
func createInferenceService(name string, namespace string) *kservev1beta1.InferenceService {
	return &kservev1beta1.InferenceService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: kservev1beta1.InferenceServiceSpec{
			Predictor: kservev1beta1.PredictorSpec{
				Model: &kservev1beta1.ModelSpec{
					ModelFormat: kservev1beta1.ModelFormat{
						Name: "sklearn",
					},
				},
			},
		},
	}
}

func makePVCReady(ctx context.Context, k8sClient client.Client, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	pvc := &corev1.PersistentVolumeClaim{}
	pvcName := types.NamespacedName{
		Name:      generatePVCName(instance),
		Namespace: instance.Namespace,
	}
	if err := k8sClient.Get(ctx, pvcName, pvc); err != nil {
		return err
	}

	pvc.Status.Phase = corev1.ClaimBound
	return k8sClient.Status().Update(ctx, pvc)
}

func makeDeploymentReady(ctx context.Context, k8sClient client.Client, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	deployment := &appsv1.Deployment{}
	deploymentName := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}

	if err := k8sClient.Get(ctx, deploymentName, deployment); err != nil {
		return err
	}

	deployment.Status.Conditions = []appsv1.DeploymentCondition{
		{
			Type:    appsv1.DeploymentAvailable,
			Status:  corev1.ConditionTrue,
			Reason:  "DeploymentReady",
			Message: "The deployment is ready",
		},
	}

	if deployment.Spec.Replicas != nil {
		deployment.Status.ReadyReplicas = 1
		deployment.Status.Replicas = 1
	}

	return k8sClient.Update(ctx, deployment)
}

func makeRouteReady(ctx context.Context, k8sClient client.Client, instance *trustyaiopendatahubiov1alpha1.TrustyAIService) error {
	route := &routev1.Route{}
	routeName := types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}

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

func checkServiceAccountAnnotations(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, k8sClient client.Client) {
	serviceAccount := &corev1.ServiceAccount{}
	serviceAccountName := generateServiceAccountName(instance)
	err := k8sClient.Get(ctx, types.NamespacedName{Name: serviceAccountName, Namespace: instance.Namespace}, serviceAccount)
	Expect(err).ToNot(HaveOccurred())
	Expect(serviceAccount).ToNot(BeNil())

	// Build the OAuth redirect reference
	oauthRedirectRef := struct {
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
		Reference  struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"reference"`
	}{
		Kind:       "OAuthRedirectReference",
		APIVersion: "v1",
		Reference: struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		}{
			Kind: "Route",
			Name: instance.Name,
		},
	}

	oauthRedirectRefJSON, err := json.Marshal(oauthRedirectRef)
	Expect(err).ToNot(HaveOccurred())

	annotation := serviceAccount.Annotations["serviceaccounts.openshift.io/oauth-redirectreference.primary"]
	Expect(annotation).To(Equal(string(oauthRedirectRefJSON)))
}

func checkClusterRoleBinding(ctx context.Context, instance *trustyaiopendatahubiov1alpha1.TrustyAIService, k8sClient client.Client) {
	clusterRoleBindingName := instance.Name + "-" + instance.Namespace + "-proxy-rolebinding"

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterRoleBindingName, Namespace: instance.Namespace}, clusterRoleBinding)
	Expect(err).ToNot(HaveOccurred())
	Expect(clusterRoleBinding).ToNot(BeNil())
	Expect(clusterRoleBinding.Name).To(Equal(clusterRoleBindingName))

	Expect(clusterRoleBinding.RoleRef.Kind).To(Equal("ClusterRole"))
	Expect(clusterRoleBinding.RoleRef.Name).To(Equal("trustyai-service-operator-proxy-role"))
	Expect(clusterRoleBinding.Subjects).To(ContainElement(rbacv1.Subject{
		Kind:      "ServiceAccount",
		Name:      generateServiceAccountName(instance),
		Namespace: instance.Namespace,
	}))
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases"),
			filepath.Join("..", "config", "prometheus"),
			filepath.Join("..", "tests", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = trustyaiopendatahubiov1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// Add Monitoring
	err = monitoringv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// Add InferenceServices
	err = kservev1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = kservev1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// Add Routes
	err = routev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	recorder := k8sManager.GetEventRecorderFor("trustyai-service-operator")

	err = (&TrustyAIServiceReconciler{
		Client:        k8sManager.GetClient(),
		Scheme:        k8sManager.GetScheme(),
		EventRecorder: recorder,
		Namespace:     operatorNamespace,
	}).SetupWithManager(k8sManager)
	Expect(err).NotTo(HaveOccurred())

	Eventually(func() error {
		return createNamespace(ctx, k8sClient, operatorNamespace)
	}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create namespace")
	Eventually(func() error {
		return createMockPV(ctx, k8sClient, "mypv", "100Gi")
	}, time.Second*10, time.Millisecond*250).Should(Succeed(), "failed to create PV")

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
