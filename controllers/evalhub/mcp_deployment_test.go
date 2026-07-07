package evalhub

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildMCPDeploymentSpec_envAndArgs(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, evalhubv1.AddToScheme(scheme))

	enabled := true
	evalHub := &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "eh", Namespace: "team-a"},
		Spec: evalhubv1.EvalHubSpec{
			MCP: &evalhubv1.EvalHubMCPSpec{
				Enabled:          &enabled,
				Transport:        "http",
				EvalHubTransport: "http-sse",
				AuthSecret:       "mcp-token",
			},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mcp-token", Namespace: "team-a"},
		Data:       map[string][]byte{"token": []byte("secret")},
	}
	operatorCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: "op-system"},
		Data: map[string]string{
			configMapEvalHubImageKey:       testMCPEvalHubImage,
			configMapKubeRBACProxyImageKey: testBuildKubeRBACProxyImage,
		},
	}

	r := &EvalHubReconciler{
		Client:                fake.NewClientBuilder().WithScheme(scheme).WithObjects(evalHub, secret, operatorCM).Build(),
		Scheme:                scheme,
		Namespace:             "op-system",
		OperatorConfigMapName: configMapName,
	}

	spec, err := r.buildMCPDeploymentSpec(context.Background(), evalHub)
	require.NoError(t, err)
	require.Len(t, spec.Template.Spec.Containers, 2)

	mcpC := spec.Template.Spec.Containers[0]
	assert.Equal(t, mcpContainerName, mcpC.Name)
	assert.Equal(t, []string{mcpBinaryPath}, mcpC.Command)
	assert.Equal(t, []string{
		"--config", mcpConfigFilePath(),
		"--transport", "http",
		"--host", "127.0.0.1",
		"--port", "8445",
		"--auth-type", "rbac-proxy",
	}, mcpC.Args)
	assert.Nil(t, mcpC.LivenessProbe)
	assert.Nil(t, mcpC.ReadinessProbe)

	env := envVarMap(mcpC.Env)
	assert.Equal(t, "http-sse", env["EVALHUB_TRANSPORT"])
	assert.Equal(t, "https://eh.team-a.svc.cluster.local:8443", env["EVALHUB_BASE_URL"])
	assert.Equal(t, "team-a", env["EVALHUB_TENANT"])
	assert.Equal(t, "false", env["EVALHUB_INSECURE"])
	assert.NotContains(t, env, "EVALHUB_TLS_CERT_FILE")

	tokenEnv := findEnv(mcpC.Env, "EVALHUB_TOKEN")
	require.NotNil(t, tokenEnv)
	require.NotNil(t, tokenEnv.ValueFrom.SecretKeyRef)
	assert.Equal(t, "mcp-token", tokenEnv.ValueFrom.SecretKeyRef.Name)

	krp := spec.Template.Spec.Containers[1]
	assert.Equal(t, kubeRBACProxyContainerName, krp.Name)
	assert.Contains(t, strings.Join(krp.Args, " "), "--upstream=http://127.0.0.1:8445/")
	assert.Contains(t, strings.Join(krp.Args, " "), "--ignore-paths="+mcpHealthPath)
	assert.NotNil(t, krp.ReadinessProbe)
	assert.Equal(t, mcpServicePort, int(krp.ReadinessProbe.HTTPGet.Port.IntValue()))
}

func envVarMap(env []corev1.EnvVar) map[string]string {
	out := make(map[string]string, len(env))
	for _, e := range env {
		if e.Value != "" {
			out[e.Name] = e.Value
		}
	}
	return out
}

func findEnv(env []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return &env[i]
		}
	}
	return nil
}
