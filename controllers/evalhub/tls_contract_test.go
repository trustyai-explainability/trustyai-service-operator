package evalhub

import (
	"context"
	"fmt"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/require"
	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	pkgtls "github.com/trustyai-explainability/trustyai-service-operator/pkg/tls"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

func managedTLSFixture(t *testing.T) (*EvalHubReconciler, *evalhubv1.EvalHub) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, evalhubv1.AddToScheme(scheme))
	enabled := true
	instance := &evalhubv1.EvalHub{ObjectMeta: metav1.ObjectMeta{Name: "tls-contract", Namespace: "team-a", UID: types.UID("tls-owner"), Finalizers: []string{evalhubv1.FinalizerName}}, Spec: evalhubv1.EvalHubSpec{Database: &evalhubv1.DatabaseSpec{Type: "sqlite"}, MCP: &evalhubv1.EvalHubMCPSpec{Enabled: &enabled}}}
	instance.Status.Phase = "Pending"
	operatorCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: "op-system"}, Data: map[string]string{configMapEvalHubImageKey: testBuildEvalHubImage, configMapKubeRBACProxyImageKey: testBuildKubeRBACProxyImage}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(instance).WithObjects(instance, operatorCM).Build()
	return &EvalHubReconciler{Client: c, Scheme: scheme, Namespace: "op-system", OperatorConfigMapName: configMapName, EventRecorder: record.NewFakeRecorder(20)}, instance
}

func TestManagedTLSRejectsNativeOverrides(t *testing.T) {
	for _, key := range []string{"TLS_REQUIRED", "TLS_MIN_VERSION", "TLS_CIPHERS", "TLS_CURVES", "TLS_CERT_FILE", "TLS_KEY_FILE", "EVALHUB_TLS_CERT_FILE", "EVALHUB_TLS_KEY_FILE"} {
		for _, mcp := range []bool{false, true} {
			for _, reference := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/mcp=%v/reference=%v", key, mcp, reference), func(t *testing.T) {
					r, instance := managedTLSFixture(t)
					override := corev1.EnvVar{Name: key, Value: "requested-native-TLS"}
					if reference {
						override.Value = ""
						override.ValueFrom = &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tls-settings"}, Key: "value"}}
					}
					path := "spec.env"
					if mcp {
						instance.Spec.MCP.Env = []corev1.EnvVar{override}
						path = "spec.mcp.env"
					} else {
						instance.Spec.Env = []corev1.EnvVar{override}
					}
					require.ErrorContains(t, validateManagedTLS(instance), path)
					_, err := r.buildDeploymentSpec(context.Background(), instance, nil, nil, nil, nil)
					require.ErrorContains(t, err, key)
					_, err = r.buildMCPDeploymentSpec(context.Background(), instance)
					require.ErrorContains(t, err, key)
				})
			}
		}
	}
}

func TestManagedTLSOnlyChecksEnabledMCPAndPreservesClientControls(t *testing.T) {
	_, instance := managedTLSFixture(t)
	instance.Spec.Env = []corev1.EnvVar{{Name: "MLFLOW_CA_CERT_PATH", Value: "/client-ca.pem"}}
	instance.Spec.MCP.Env = []corev1.EnvVar{{Name: "TLS_REQUIRED", Value: "false"}}
	*instance.Spec.MCP.Enabled = false
	require.NoError(t, validateManagedTLS(instance))
	*instance.Spec.MCP.Enabled = true
	require.ErrorContains(t, validateManagedTLS(instance), "spec.mcp.env")
	instance.Spec.MCP.Env = []corev1.EnvVar{{Name: "EVALHUB_CA_CERT_PATH", Value: "/client-ca.pem"}}
	require.NoError(t, validateManagedTLS(instance))
}

func TestManagedTLSValidationReportsStatusWithoutChangingDeployments(t *testing.T) {
	r, instance := managedTLSFixture(t)
	ctx := context.Background()
	require.NoError(t, r.reconcileDeployment(ctx, instance, nil, nil, nil, nil))
	require.NoError(t, r.reconcileMCPDeployment(ctx, instance))
	before := &appsv1.DeploymentList{}
	require.NoError(t, r.List(ctx, before))
	instance.Spec.MCP.Env = []corev1.EnvVar{{Name: "TLS_REQUIRED", Value: "true"}}
	require.NoError(t, r.Update(context.Background(), instance))
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(instance)})
	require.ErrorContains(t, err, "spec.mcp.env")
	require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(instance), instance))
	require.Equal(t, "Error", instance.Status.Phase)
	found := false
	for _, condition := range instance.Status.Conditions {
		if condition.Reason == "UnsupportedNativeTLS" {
			found = true
			require.Equal(t, corev1.ConditionFalse, condition.Status)
		}
	}
	require.True(t, found)
	deployments := &appsv1.DeploymentList{}
	require.NoError(t, r.List(context.Background(), deployments))
	require.Equal(t, before.Items, deployments.Items, "invalid native-TLS overrides must leave both existing workloads unchanged")
}

func assertManagedHTTP(t *testing.T, spec appsv1.DeploymentSpec, appName, cert, key string, expected pkgtls.ProxyTLSArguments) {
	t.Helper()
	app := findContainerByName(spec.Template.Spec.Containers, appName)
	require.NotNil(t, app)
	env := map[string]corev1.EnvVar{}
	for _, variable := range app.Env {
		env[variable.Name] = variable
	}
	require.Equal(t, "false", env["TLS_REQUIRED"].Value)
	require.Contains(t, env, cert)
	require.Contains(t, env, key)
	require.Empty(t, env[cert].Value)
	require.Nil(t, env[cert].ValueFrom)
	require.Empty(t, env[key].Value)
	require.Nil(t, env[key].ValueFrom)
	for _, key := range []string{"TLS_MIN_VERSION", "TLS_CIPHERS", "TLS_CURVES"} {
		require.NotContains(t, env, key)
	}
	for _, volume := range spec.Template.Spec.Volumes {
		if volume.Secret != nil && (volume.Secret.SecretName == "tls-contract-tls" || volume.Secret.SecretName == "tls-contract-mcp-tls") {
			for _, mount := range app.VolumeMounts {
				require.NotEqual(t, volume.Name, mount.Name, "serving keys must only be mounted in proxy")
			}
		}
	}
	proxy := findContainerByName(spec.Template.Spec.Containers, kubeRBACProxyContainerName)
	require.NotNil(t, proxy)
	require.Subset(t, proxy.Args, expected.Args)
}

func TestManagedTLSProfilesAndHTTPConfig(t *testing.T) {
	previous := pkgtls.CurrentProxyTLSArguments()
	t.Cleanup(func() { pkgtls.SetProxyTLSArguments(previous) })
	for _, tt := range []struct {
		name, adherence string
		profile         *configv1.TLSSecurityProfile
		groups          []string
	}{
		{name: "Intermediate"},
		{name: "Legacy Modern", adherence: pkgtls.TLSAdherenceLegacyAdheringComponentsOnly, profile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}},
		{name: "Strict Modern", adherence: pkgtls.TLSAdherenceStrictAllComponents, profile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}},
		{name: "Strict Custom", adherence: pkgtls.TLSAdherenceStrictAllComponents, profile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType, Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{MinTLSVersion: configv1.VersionTLS12, Ciphers: []string{"ECDHE-RSA-AES128-GCM-SHA256"}}}}, groups: []string{"secp256r1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			expected, err := pkgtls.ResolveProxyTLSArguments(tt.profile, tt.adherence, tt.groups, false)
			require.NoError(t, err)
			pkgtls.SetProxyTLSArguments(expected)
			r, instance := managedTLSFixture(t)
			instance.Spec.Env = []corev1.EnvVar{{Name: "EVAL_HUB_CONFIG_DIR", Value: "/unsupported/config"}, {Name: "CUSTOM_SETTING", Value: "kept"}}
			api, err := r.buildDeploymentSpec(context.Background(), instance, nil, nil, nil, nil)
			require.NoError(t, err)
			assertManagedHTTP(t, api, containerName, "TLS_CERT_FILE", "TLS_KEY_FILE", expected)
			require.Equal(t, configDirPath, envVarMap(api.Template.Spec.Containers[0].Env)["EVAL_HUB_CONFIG_DIR"])
			require.Equal(t, "kept", envVarMap(api.Template.Spec.Containers[0].Env)["CUSTOM_SETTING"])
			for _, transport := range []string{"http", "http-sse"} {
				instance.Spec.MCP.Transport = transport
				mcp, err := r.buildMCPDeploymentSpec(context.Background(), instance)
				require.NoError(t, err)
				assertManagedHTTP(t, mcp, mcpContainerName, "EVALHUB_TLS_CERT_FILE", "EVALHUB_TLS_KEY_FILE", expected)
			}
			data, err := r.generateConfigData(context.Background(), instance)
			require.NoError(t, err)
			var apiConfig map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(data["config.yaml"]), &apiConfig))
			service := apiConfig["service"].(map[string]any)
			require.Equal(t, "127.0.0.1", service["host"])
			require.Equal(t, "", service["tls_cert_file"])
			require.Equal(t, "", service["tls_key_file"])
			mappings := apiConfig["env_mappings"].(map[string]any)
			require.NotContains(t, mappings, "TLS_CERT_FILE")
			require.NotContains(t, mappings, "TLS_KEY_FILE")
			mcpData, err := r.generateMCPConfigData(instance)
			require.NoError(t, err)
			var mcpConfig map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(mcpData[mcpConfigFileName]), &mcpConfig))
			require.Equal(t, "", mcpConfig["tls_cert_file"])
			require.Equal(t, "", mcpConfig["tls_key_file"])
		})
	}
}

func TestManagedTLSPolicyChangesRefreshExistingDeployments(t *testing.T) {
	previous := pkgtls.CurrentProxyTLSArguments()
	t.Cleanup(func() { pkgtls.SetProxyTLSArguments(previous) })
	r, instance := managedTLSFixture(t)
	ctx := context.Background()
	custom := func(cipher string) *configv1.TLSSecurityProfile {
		return &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType, Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{MinTLSVersion: configv1.VersionTLS12, Ciphers: []string{cipher}}}}
	}
	modern := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	policies := []struct {
		profile   *configv1.TLSSecurityProfile
		adherence string
		groups    []string
		changed   bool
	}{
		{nil, pkgtls.TLSAdherenceNoOpinion, nil, false},
		{modern, pkgtls.TLSAdherenceLegacyAdheringComponentsOnly, nil, false},
		{modern, pkgtls.TLSAdherenceStrictAllComponents, nil, true},
		{custom("ECDHE-RSA-AES128-GCM-SHA256"), pkgtls.TLSAdherenceStrictAllComponents, []string{"secp256r1"}, true},
		{custom("ECDHE-RSA-AES256-GCM-SHA384"), pkgtls.TLSAdherenceStrictAllComponents, []string{"secp256r1"}, true},
		{custom("ECDHE-RSA-AES256-GCM-SHA384"), pkgtls.TLSAdherenceStrictAllComponents, []string{"secp384r1"}, true},
		{custom("ECDHE-RSA-AES256-GCM-SHA384"), "future-strict", []string{"secp384r1"}, false},
	}
	old := map[string]corev1.PodTemplateSpec{}
	for index, policy := range policies {
		args, err := pkgtls.ResolveProxyTLSArguments(policy.profile, policy.adherence, policy.groups, false)
		require.NoError(t, err)
		pkgtls.SetProxyTLSArguments(args)
		require.NoError(t, r.reconcileDeployment(ctx, instance, nil, nil, nil, nil))
		require.NoError(t, r.reconcileMCPDeployment(ctx, instance))
		for _, name := range []string{instance.Name, mcpDeploymentName(instance)} {
			deployment := &appsv1.Deployment{}
			require.NoError(t, r.Get(ctx, client.ObjectKey{Name: name, Namespace: instance.Namespace}, deployment))
			if index > 0 {
				if policy.changed {
					require.NotEqual(t, old[name], deployment.Spec.Template, "effective policy changes must roll pods")
				} else {
					require.Equal(t, old[name], deployment.Spec.Template, "equivalent effective policy must not change Pod template")
				}
			}
			old[name] = *deployment.Spec.Template.DeepCopy()
		}
	}
	require.NoError(t, r.reconcileDeployment(ctx, instance, nil, nil, nil, nil))
	require.NoError(t, r.reconcileMCPDeployment(ctx, instance))
	for name, template := range old {
		deployment := &appsv1.Deployment{}
		require.NoError(t, r.Get(ctx, client.ObjectKey{Name: name, Namespace: instance.Namespace}, deployment))
		require.Equal(t, template, deployment.Spec.Template, "unchanged policy must not change Pod template")
	}
}
