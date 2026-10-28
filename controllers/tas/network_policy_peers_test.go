package tas

import (
	"encoding/json"
	"os"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Fixtures contain Pod-template metadata extracted from kustomize builds of
// manifests/{odh,rhoai} at opendatahub-io/odh-dashboard commit
// 9070a32842752a08e0b18ecbd2ad1c72c535e0fc. Dashboard's controller renders both
// variants with WithNamespace(r.ApplicationsNamespace); its backend proxies TAS
// requests to <name>-tls.<project>.svc:443, which targets Pod port 8443.
func TestTASNetworkPolicyDashboardPeers(t *testing.T) {
	owner := testTASNetworkPolicyOwner()
	desired, err := buildTASNetworkPolicy(owner, operatorNamespace, false)
	if err != nil {
		t.Fatal(err)
	}
	allows := func(namespace string, podLabels map[string]string, port int32) bool {
		for _, rule := range desired.Policy.Spec.Ingress {
			portMatches := false
			for _, p := range rule.Ports {
				if p.Port != nil && p.Port.IntVal == port {
					portMatches = true
				}
			}
			if !portMatches {
				continue
			}
			for _, peer := range rule.From {
				if peer.NamespaceSelector == nil {
					if namespace != owner.Namespace {
						continue
					}
				} else {
					selector, err := metav1.LabelSelectorAsSelector(peer.NamespaceSelector)
					if err != nil {
						t.Fatal(err)
					}
					if !selector.Matches(labels.Set{"kubernetes.io/metadata.name": namespace}) {
						continue
					}
				}
				if peer.PodSelector != nil {
					selector, err := metav1.LabelSelectorAsSelector(peer.PodSelector)
					if err != nil {
						t.Fatal(err)
					}
					if !selector.Matches(labels.Set(podLabels)) {
						continue
					}
				}
				return true
			}
		}
		return false
	}
	for _, variant := range []string{"odh", "rhoai"} {
		t.Run(variant, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + variant + "-dashboard.json")
			if err != nil {
				t.Fatal(err)
			}
			var deployment appsv1.Deployment
			if err := json.Unmarshal(data, &deployment); err != nil {
				t.Fatal(err)
			}
			podLabels := deployment.Spec.Template.Labels
			if !allows(operatorNamespace, podLabels, 8443) {
				t.Error("Dashboard cannot reach the authenticated TAS listener")
			}
			if allows("other-applications", podLabels, 8443) {
				t.Error("Dashboard labels grant access from a foreign namespace")
			}
			for _, port := range []int32{8080, 4443, 9443} {
				if allows(operatorNamespace, podLabels, port) {
					t.Errorf("Dashboard unexpectedly admitted on port %d", port)
				}
			}
			for _, key := range []string{"deployment", "app.kubernetes.io/part-of"} {
				wrongLabels := deployment.Spec.Template.DeepCopy().Labels
				wrongLabels[key] = "unrelated"
				if allows(operatorNamespace, wrongLabels, 8443) {
					t.Errorf("peer does not require %s", key)
				}
			}
		})
	}
}
