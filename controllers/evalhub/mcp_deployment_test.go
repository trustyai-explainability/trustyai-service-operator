package evalhub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildMCPDeploymentSpec_envAndArgs(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, evalhubv1alpha1.AddToScheme(scheme))

	enabled := true
	evalHub := &evalhubv1alpha1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "eh", Namespace: "team-a"},
		Spec: evalhubv1alpha1.EvalHubSpec{
			MCP: &evalhubv1alpha1.EvalHubMCPSpec{
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

	r := &EvalHubReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(evalHub, secret).Build(),
		Scheme: scheme,
	}

	spec, err := r.buildMCPDeploymentSpec(context.Background(), evalHub)
	require.NoError(t, err)
	require.Len(t, spec.Template.Spec.Containers, 1)

	c := spec.Template.Spec.Containers[0]
	assert.Equal(t, []string{mcpBinaryPath}, c.Command)
	assert.Equal(t, []string{
		"--config", mcpConfigFilePath(),
		"--transport", "http",
		"--host", "0.0.0.0",
		"--port", "8443",
	}, c.Args)

	env := envVarMap(c.Env)
	assert.Equal(t, "http-sse", env["EVALHUB_TRANSPORT"])
	assert.Equal(t, "https://eh.team-a.svc.cluster.local:8443", env["EVALHUB_BASE_URL"])
	assert.Equal(t, "team-a", env["EVALHUB_TENANT"])
	assert.Equal(t, "false", env["EVALHUB_INSECURE"])
	assert.NotContains(t, env, "EVALHUB_AUTH_TOKEN")
	assert.NotContains(t, env, "EVALHUB_INSECURE_SKIP_VERIFY")

	tokenEnv := findEnv(c.Env, "EVALHUB_TOKEN")
	require.NotNil(t, tokenEnv)
	require.NotNil(t, tokenEnv.ValueFrom)
	require.NotNil(t, tokenEnv.ValueFrom.SecretKeyRef)
	assert.Equal(t, "mcp-token", tokenEnv.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "token", tokenEnv.ValueFrom.SecretKeyRef.Key)
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
