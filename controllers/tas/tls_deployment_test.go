package tas

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	pkgtls "github.com/trustyai-explainability/trustyai-service-operator/pkg/tls"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTASTLSDeploymentUpgradeAndPolicyRefresh(t *testing.T) {
	ctx := context.Background()
	scheme, owner := tasNetworkPolicyFixture(t)
	owner.Spec.Storage.Format = STORAGE_DATABASE
	owner.Spec.Storage.DatabaseConfigurations = "db"
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, createConfigMap(operatorNamespace, testKubeRBACProxyImage, testTrustAIServiceImage)).Build()
	r := &TrustyAIServiceReconciler{Client: c, Scheme: scheme, Namespace: operatorNamespace, EventRecorder: record.NewFakeRecorder(100)}
	previous := pkgtls.CurrentProxyTLSArguments()
	defer pkgtls.SetProxyTLSArguments(previous)
	legacy, err := r.createDeploymentObject(ctx, owner, testTrustAIServiceImage, CustomCertificatesBundle{})
	if err != nil {
		t.Fatal(err)
	}
	// Model an existing native-TLS deployment, including user-owned settings.
	legacy.Spec.Template.Spec.Containers = legacy.Spec.Template.Spec.Containers[:2]
	app := serviceContainer(legacy)
	app.VolumeMounts = append(app.VolumeMounts, corev1.VolumeMount{Name: owner.Name + "-internal", MountPath: "/etc/tls/internal"})
	app.Env = []corev1.EnvVar{{Name: "KEEP_ALIVE", Value: "99"}, {Name: "TLS_REQUIRED", Value: "true"}, {Name: "TLS_CURVES", Value: "unsupported"}}
	app.Image = "custom-service:keep"
	legacy.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = "keep"
	legacy.Spec.Template.Spec.Containers[1].Args = []string{"--tls-min-version=VersionTLS12"}
	if err := c.Create(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	selector := legacy.Spec.Selector.DeepCopy()

	for _, adherence := range []string{pkgtls.TLSAdherenceStrictAllComponents, pkgtls.TLSAdherenceLegacyAdheringComponentsOnly, "", "FutureStrict"} {
		t.Run(adherence, func(t *testing.T) {
			profile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
			var groups []string
			if adherence == "FutureStrict" {
				profile = &configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType, Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{MinTLSVersion: configv1.VersionTLS12, Ciphers: []string{"ECDHE-RSA-AES128-GCM-SHA256"}}}}
				groups = []string{"secp256r1", "secp384r1"}
			}
			args, err := pkgtls.ResolveProxyTLSArguments(profile, adherence, groups, false)
			if err != nil {
				t.Fatal(err)
			}
			pkgtls.SetProxyTLSArguments(args)
			if err := r.ensureDeployment(ctx, owner, CustomCertificatesBundle{}, false); err != nil {
				t.Fatal(err)
			}
			current := &appsv1.Deployment{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(owner), current); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(current.Spec.Selector, selector) || current.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] != "keep" {
				t.Fatal("TLS refresh changed selector or user annotations")
			}
			app := serviceContainer(current)
			if app.Image != "custom-service:keep" {
				t.Fatal("TLS refresh replaced user service image")
			}
			env := map[string]string{}
			for _, e := range app.Env {
				env[e.Name] = e.Value
			}
			if env["KEEP_ALIVE"] != "99" || env["TLS_REQUIRED"] != "false" || env["TLS_POLICY"] != "system" || env["TLS_CURVES"] != "" || !strings.Contains(env["TLS_CERT_FILE"], "no-native-tls") {
				t.Fatalf("wrong application policy: %#v", env)
			}
			for _, mount := range app.VolumeMounts {
				if mount.Name == owner.Name+"-internal" || mount.MountPath == "/etc/tls/internal" {
					t.Fatal("native app still has serving keys")
				}
			}
			if len(current.Spec.Template.Spec.Containers) != 3 {
				t.Fatal("missing internal terminator")
			}
			for _, proxy := range current.Spec.Template.Spec.Containers[1:] {
				for _, arg := range args.Args {
					if !slices.Contains(proxy.Args, arg) {
						t.Fatalf("%s missing %s", proxy.Name, arg)
					}
				}
				if proxy.Name == "internal-tls-proxy" {
					if !slices.Contains(proxy.Args, "--upstream=http://127.0.0.1:8080/") || !slices.Contains(proxy.Args, "--ignore-paths=/,/consumer/kserve/v2,/q/health/live,/q/health/ready") {
						t.Fatal("internal proxy changed consumer/auth boundary")
					}
					if len(proxy.VolumeMounts) != 2 || proxy.VolumeMounts[0].Name != owner.Name+"-internal" || !proxy.VolumeMounts[0].ReadOnly {
						t.Fatal("internal proxy must use the internal Service certificate")
					}
				} else {
					for _, arg := range proxy.Args {
						if strings.HasPrefix(arg, "--ignore-paths") {
							t.Fatal("external API authentication bypass")
						}
					}
				}
			}
			// Reconcile again: even API-defaulted probe settings must not churn pods.
			current.Spec.Template.Spec.Containers[2].ReadinessProbe.TimeoutSeconds = 1
			current.Spec.Template.Spec.Containers[2].ReadinessProbe.SuccessThreshold = 1
			if err := c.Update(ctx, current); err != nil {
				t.Fatal(err)
			}
			version := current.ResourceVersion
			if err := r.ensureDeployment(ctx, owner, CustomCertificatesBundle{}, false); err != nil {
				t.Fatal(err)
			}
			after := &appsv1.Deployment{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(owner), after); err != nil {
				t.Fatal(err)
			}
			if after.ResourceVersion != version {
				t.Fatal("unchanged/defaulted TLS configuration caused another update")
			}
		})
	}
}

func TestTASTLSDeploymentRejectsForeignOwner(t *testing.T) {
	ctx := context.Background()
	scheme, owner := tasNetworkPolicyFixture(t)
	current := &appsv1.Deployment{}
	current.Name, current.Namespace = owner.Name, owner.Namespace
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).Build()
	r := &TrustyAIServiceReconciler{Client: c, Scheme: scheme}
	if err := r.ensureTASTLSDeployment(ctx, owner, current, current.DeepCopy()); err == nil {
		t.Fatal("unowned Deployment must not be changed")
	}
}

func TestTASTLSMonitorUsesVerifiedExternalProxy(t *testing.T) {
	monitor, err := createLocalServiceMonitorObject(context.Background(), "test-namespace", "tas")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := monitor.Spec.Endpoints[0]
	if endpoint.Port != "https" || endpoint.Scheme != "https" || endpoint.TLSConfig == nil || endpoint.TLSConfig.ServerName == nil || *endpoint.TLSConfig.ServerName != "tas-tls.test-namespace.svc" || endpoint.TLSConfig.CA.ConfigMap == nil || endpoint.TLSConfig.CA.ConfigMap.Name != "tas-metrics-ca-bundle" || endpoint.TLSConfig.InsecureSkipVerify != nil && *endpoint.TLSConfig.InsecureSkipVerify {
		t.Fatalf("unverified metrics endpoint: %#v", endpoint)
	}
	if endpoint.BearerTokenSecret.Name != "tas-metrics-reader-token" {
		t.Fatal("metrics authentication was removed")
	}
}
