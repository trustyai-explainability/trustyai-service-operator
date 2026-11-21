package tas

import (
	"context"
	"strings"
	"testing"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Removal runs from the finalizer, on Deployments this operator may never have
// configured: one created after the last successful pass, or one whose
// MM_PAYLOAD_PROCESSORS was edited away. There is nothing to remove in those
// cases, and the pass has to say so rather than dereference a nil env var, which
// panicked the reconciler and stuck the finalizer.
func TestPatchEnvVarsForDeploymentsOnlyRemovesWhatIsThere(t *testing.T) {
	const sinkURL = "https://sink.example/consumer/kserve/v2"

	newReconciler := func(t *testing.T, deployment *appsv1.Deployment) *TrustyAIServiceReconciler {
		t.Helper()
		s := runtime.NewScheme()
		if err := appsv1.AddToScheme(s); err != nil {
			t.Fatalf("add appsv1 to scheme: %v", err)
		}
		if err := corev1.AddToScheme(s); err != nil {
			t.Fatalf("add corev1 to scheme: %v", err)
		}
		return &TrustyAIServiceReconciler{
			Client:        fake.NewClientBuilder().WithScheme(s).WithObjects(deployment).Build(),
			EventRecorder: record.NewFakeRecorder(10),
		}
	}

	instance := &trustyaiopendatahubiov1.TrustyAIService{
		ObjectMeta: metav1.ObjectMeta{Name: "trustyai-service", Namespace: "env-vars-ns"},
	}

	deploymentWithEnv := func(env ...corev1.EnvVar) *appsv1.Deployment {
		replicas := int32(1)
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "modelmesh-deployment", Namespace: "env-vars-ns"},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: mmContainerName, Env: env}},
					},
				},
			},
			Status: appsv1.DeploymentStatus{ReadyReplicas: 1},
		}
	}

	t.Run("removal from a Deployment without the variable is a no-op", func(t *testing.T) {
		deployment := deploymentWithEnv(corev1.EnvVar{Name: "OTHER", Value: "kept"})
		reconciler := newReconciler(t, deployment)

		shouldContinue, err := reconciler.patchEnvVarsForDeployments(
			context.Background(), instance, []appsv1.Deployment{*deployment}, payloadProcessorName, sinkURL, true)
		if err != nil {
			t.Fatalf("removal without the variable returned an error: %v", err)
		}
		if !shouldContinue {
			t.Fatal("removal without the variable must not stop the reconcile")
		}

		saved := &appsv1.Deployment{}
		if err := reconciler.Client.Get(context.Background(),
			client.ObjectKeyFromObject(deployment), saved); err != nil {
			t.Fatalf("get Deployment: %v", err)
		}
		for _, env := range saved.Spec.Template.Spec.Containers[0].Env {
			if env.Name == payloadProcessorName {
				t.Fatalf("removal added %s to a Deployment that did not have it: %q", payloadProcessorName, env.Value)
			}
			if env.Name == tlsKeyCertPathName {
				t.Fatalf("removal added %s to a Deployment that did not have it: %q", tlsKeyCertPathName, env.Value)
			}
		}
		if len(saved.Spec.Template.Spec.Volumes) != 0 {
			t.Fatalf("removal added the serving-certificate volume: %v", saved.Spec.Template.Spec.Volumes)
		}
		if len(saved.Spec.Template.Spec.Containers[0].VolumeMounts) != 0 {
			t.Fatalf("removal added the serving-certificate mount: %v", saved.Spec.Template.Spec.Containers[0].VolumeMounts)
		}
	})

	// Removal runs as the instance's TLS Secret is being deleted. Mounting it here
	// left ModelMesh Pods created after the uninstall unable to start, so removal
	// must take the material back instead of publishing it.
	t.Run("removal deletes the serving-certificate material it added", func(t *testing.T) {
		deployment := deploymentWithEnv(
			corev1.EnvVar{Name: payloadProcessorName, Value: sinkURL},
			corev1.EnvVar{Name: tlsKeyCertPathName, Value: tlsMountPath + "/tls.crt"},
		)
		deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
			{Name: instance.Name + "-internal", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: instance.Name + "-internal"}}},
			{Name: "user-volume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		}
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: instance.Name + "-internal", MountPath: tlsMountPath, ReadOnly: true},
			{Name: "user-volume", MountPath: "/user"},
			{Name: "user-same-name", MountPath: tlsMountPath},
		}
		reconciler := newReconciler(t, deployment)

		if _, err := reconciler.patchEnvVarsForDeployments(
			context.Background(), instance, []appsv1.Deployment{*deployment}, payloadProcessorName, sinkURL, true); err != nil {
			t.Fatalf("removal returned an error: %v", err)
		}

		saved := &appsv1.Deployment{}
		if err := reconciler.Client.Get(context.Background(),
			client.ObjectKeyFromObject(deployment), saved); err != nil {
			t.Fatalf("get Deployment: %v", err)
		}
		for _, volume := range saved.Spec.Template.Spec.Volumes {
			if volume.Name == instance.Name+"-internal" {
				t.Fatalf("removal left the serving-certificate volume in place")
			}
		}
		if len(saved.Spec.Template.Spec.Volumes) != 1 || saved.Spec.Template.Spec.Volumes[0].Name != "user-volume" {
			t.Fatalf("removal dropped a volume this operator does not own: %v", saved.Spec.Template.Spec.Volumes)
		}
		for _, mount := range saved.Spec.Template.Spec.Containers[0].VolumeMounts {
			if mount.Name == instance.Name+"-internal" {
				t.Fatalf("removal left the serving-certificate mount in place")
			}
		}
		if len(saved.Spec.Template.Spec.Containers[0].VolumeMounts) != 2 {
			t.Fatalf("removal dropped a mount this operator does not own: %v", saved.Spec.Template.Spec.Containers[0].VolumeMounts)
		}
		for _, env := range saved.Spec.Template.Spec.Containers[0].Env {
			if env.Name == tlsKeyCertPathName {
				t.Fatalf("removal left %s in place: %q", tlsKeyCertPathName, env.Value)
			}
		}
	})

	t.Run("configuration still mounts the serving certificate", func(t *testing.T) {
		deployment := deploymentWithEnv(corev1.EnvVar{Name: "OTHER", Value: "kept"})
		reconciler := newReconciler(t, deployment)

		if _, err := reconciler.patchEnvVarsForDeployments(
			context.Background(), instance, []appsv1.Deployment{*deployment}, payloadProcessorName, sinkURL, false); err != nil {
			t.Fatalf("configuration returned an error: %v", err)
		}

		saved := &appsv1.Deployment{}
		if err := reconciler.Client.Get(context.Background(),
			client.ObjectKeyFromObject(deployment), saved); err != nil {
			t.Fatalf("get Deployment: %v", err)
		}
		if len(saved.Spec.Template.Spec.Volumes) != 1 || saved.Spec.Template.Spec.Volumes[0].Name != instance.Name+"-internal" {
			t.Fatalf("configuration did not mount the serving certificate: %v", saved.Spec.Template.Spec.Volumes)
		}
		if len(saved.Spec.Template.Spec.Containers[0].VolumeMounts) != 1 || saved.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath != tlsMountPath {
			t.Fatalf("configuration did not mount the serving certificate on the container: %v", saved.Spec.Template.Spec.Containers[0].VolumeMounts)
		}
		var sink, certPath string
		for _, env := range saved.Spec.Template.Spec.Containers[0].Env {
			switch env.Name {
			case payloadProcessorName:
				sink = env.Value
			case tlsKeyCertPathName:
				certPath = env.Value
			}
		}
		if sink != sinkURL {
			t.Fatalf("%s = %q, want %q", payloadProcessorName, sink, sinkURL)
		}
		if certPath != tlsMountPath+"/tls.crt" {
			t.Fatalf("%s = %q, want %q", tlsKeyCertPathName, certPath, tlsMountPath+"/tls.crt")
		}
	})

	t.Run("removal drops the sink and keeps other values", func(t *testing.T) {
		deployment := deploymentWithEnv(
			corev1.EnvVar{Name: payloadProcessorName, Value: "https://other.example/consumer/kserve/v2 " + sinkURL},
			corev1.EnvVar{Name: "OTHER", Value: "kept"},
		)
		reconciler := newReconciler(t, deployment)

		if _, err := reconciler.patchEnvVarsForDeployments(
			context.Background(), instance, []appsv1.Deployment{*deployment}, payloadProcessorName, sinkURL, true); err != nil {
			t.Fatalf("removal with the variable returned an error: %v", err)
		}

		saved := &appsv1.Deployment{}
		if err := reconciler.Client.Get(context.Background(),
			client.ObjectKeyFromObject(deployment), saved); err != nil {
			t.Fatalf("get Deployment: %v", err)
		}
		for _, env := range saved.Spec.Template.Spec.Containers[0].Env {
			if env.Name != payloadProcessorName {
				continue
			}
			if strings.Contains(env.Value, sinkURL) {
				t.Fatalf("removal left the sink in place: %q", env.Value)
			}
			if env.Value != "https://other.example/consumer/kserve/v2" {
				t.Fatalf("removal dropped a sink this operator does not own: %q", env.Value)
			}
		}
	})
}
