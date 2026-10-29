package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	pkgtls "github.com/trustyai-explainability/trustyai-service-operator/pkg/tls"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestTLSManagerListeners(t *testing.T) {
	for _, adherence := range []string{"", pkgtls.TLSAdherenceLegacyAdheringComponentsOnly, pkgtls.TLSAdherenceStrictAllComponents} {
		t.Run(adherence, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"apiVersion":"config.openshift.io/v1","kind":"APIServer","spec":{"tlsSecurityProfile":{"type":"Modern"},"tlsAdherence":%q}}`, adherence)
			}))
			defer server.Close()
			resolved, err := pkgtls.Resolve(context.Background(), &rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			for _, enabled := range []bool{false, true} {
				options := ctrl.Options{Metrics: metricsserver.Options{SecureServing: true, CertDir: "/serving-certs", BindAddress: ":8443"}}
				configureManagerTLS(&options, resolved, enabled)
				if !options.Metrics.SecureServing || options.Metrics.CertDir != "/serving-certs" || options.Metrics.BindAddress != ":8443" {
					t.Fatal("TLS wiring changed metrics serving/certificate settings")
				}
				check := func(opts []func(*tls.Config)) {
					t.Helper()
					cfg := &tls.Config{}
					for _, opt := range opts {
						opt(cfg)
					}
					if cfg.MinVersion != tls.VersionTLS13 || len(cfg.CipherSuites) != 0 || !slices.Equal(cfg.NextProtos, []string{"h2", "http/1.1"}) {
						t.Fatalf("listener lost Modern profile/TLS 1.3 cipher handling/ALPN: %#v", cfg)
					}
				}
				check(options.Metrics.TLSOpts)
				if enabled {
					ws, ok := options.WebhookServer.(*webhook.DefaultServer)
					if !ok || ws.Options.Port != 9443 {
						t.Fatal("missing configured webhook server")
					}
					check(ws.Options.TLSOpts)
					// The slices must not alias either each other or the result.
					options.Metrics.TLSOpts[0] = func(*tls.Config) {}
					check(ws.Options.TLSOpts)
					check(resolved.TLSOpts)
				} else if options.WebhookServer != nil {
					t.Fatal("enabled an unused webhook server")
				}
				wantProxyMinimum := string(configv1.VersionTLS12)
				if adherence == pkgtls.TLSAdherenceStrictAllComponents {
					wantProxyMinimum = string(configv1.VersionTLS13)
				}
				if resolved.ProxyArgs.MinVersion != wantProxyMinimum {
					t.Fatalf("operand adherence changed: %#v", resolved.ProxyArgs)
				}
			}
		})
	}
}

func TestTLSManagerListenersUseValidatedDegradedBootstrap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	resolved, err := pkgtls.Resolve(ctx, &rest.Config{Host: server.URL})
	if err != nil || !resolved.APIAvailable || resolved.AdherenceFetched {
		t.Fatalf("fallback must enable recovery without claiming fetched adherence: %#v, %v", resolved, err)
	}
	options := ctrl.Options{Metrics: metricsserver.Options{SecureServing: true}}
	configureManagerTLS(&options, resolved, true)
	ws := options.WebhookServer.(*webhook.DefaultServer)
	for _, opts := range [][]func(*tls.Config){options.Metrics.TLSOpts, ws.Options.TLSOpts} {
		cfg := &tls.Config{}
		for _, opt := range opts {
			opt(cfg)
		}
		if cfg.MinVersion != tls.VersionTLS12 || len(cfg.CipherSuites) == 0 || !slices.Equal(cfg.NextProtos, []string{"h2", "http/1.1"}) {
			t.Fatalf("listener lost validated Intermediate profile/ALPN: %#v", cfg)
		}
	}
}

func TestTLSAPIServerScheme(t *testing.T) {
	for _, kind := range []string{"APIServer", "APIServerList"} {
		if _, err := scheme.New(schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: kind}); err != nil {
			t.Fatalf("bootstrap/watch scheme missing %s: %v", kind, err)
		}
	}
}

func TestTLSProfileRBACSources(t *testing.T) {
	for _, path := range []string{
		"config/rbac-base/tls_profile_role.yaml",
		"trustyai-operator-module/config/rbac/role.yaml",
		"trustyai-operator-module/config/manifests-template/rbac-base/tls_profile_role.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", path))
			if err != nil {
				t.Fatal(err)
			}
			if len(tlsReaderRoles(t, string(data))) == 0 {
				t.Fatal("missing get/list/watch on apiservers")
			}
		})
	}
}

// Set KUSTOMIZE_BINARY to exercise the generated install paths as well as sources.
// CI runs this after make kustomize; ordinary unit tests need no external tools.
func TestTLSProfileRBACRendered(t *testing.T) {
	binary := os.Getenv("KUSTOMIZE_BINARY")
	if binary == "" {
		t.Skip("set KUSTOMIZE_BINARY to validate rendered installation RBAC")
	}
	for _, path := range []string{
		"config/base", "config/overlays/odh", "config/overlays/rhoai",
		"config/overlays/odh-kueue", "config/overlays/mcp-guardrails",
		"config/overlays/testing", "config/overlays/evalhub-only", "config/overlays/lmes",
		"trustyai-operator-module/config/default",
	} {
		t.Run(path, func(t *testing.T) {
			data, err := exec.Command(binary, "build", filepath.Join("..", path)).CombinedOutput()
			if err != nil {
				t.Fatalf("rendering %s: %v\n%s", path, err, data)
			}
			roles := tlsReaderRoles(t, string(data))
			if len(roles) == 0 {
				t.Fatal("rendered install lost TLS profile reader permissions")
			}
			bound := false
			decodeTLSRBAC(t, string(data), func(obj tlsRBACObject) {
				if obj.Kind == "ClusterRoleBinding" && obj.RoleRef.Kind == "ClusterRole" && slices.Contains(roles, obj.RoleRef.Name) {
					for _, subject := range obj.Subjects {
						if subject.Kind == "ServiceAccount" {
							bound = true
						}
					}
				}
			})
			if !bound {
				t.Fatal("TLS profile reader role is not bound to a ServiceAccount")
			}
		})
	}
}

type tlsRBACObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Rules             []rbacv1.PolicyRule `json:"rules"`
	RoleRef           rbacv1.RoleRef      `json:"roleRef"`
	Subjects          []rbacv1.Subject    `json:"subjects"`
}

func decodeTLSRBAC(t *testing.T, data string, visit func(tlsRBACObject)) {
	t.Helper()
	decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(data), 4096)
	for {
		var obj tlsRBACObject
		if err := decoder.Decode(&obj); err == io.EOF {
			return
		} else if err != nil {
			t.Fatal(err)
		}
		visit(obj)
	}
}

func tlsReaderRoles(t *testing.T, data string) []string {
	t.Helper()
	var names []string
	decodeTLSRBAC(t, data, func(obj tlsRBACObject) {
		if obj.Kind != "ClusterRole" {
			return
		}
		verbs := map[string]bool{}
		for _, rule := range obj.Rules {
			if slices.Contains(rule.APIGroups, "config.openshift.io") && slices.Contains(rule.Resources, "apiservers") {
				for _, verb := range rule.Verbs {
					verbs[verb] = true
				}
			}
		}
		if verbs["get"] && verbs["list"] && verbs["watch"] {
			names = append(names, obj.Name)
		}
	})
	return names
}
