package tas

import (
	"context"
	"errors"
	"strings"
	"testing"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	pkgtls "github.com/trustyai-explainability/trustyai-service-operator/pkg/tls"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// A cluster TLS profile the kube-rbac-proxy sidecars cannot serve fails the
// Deployment render. The failure has to land on the resource, otherwise an
// instance that was Ready keeps claiming readiness while its Deployment is
// never updated, and the condition has to disappear once the profile works
// again.
func TestReconcileDeploymentReportsProxyTLSFailureAndRecovers(t *testing.T) {
	ctx := context.Background()
	scheme, owner := tasNetworkPolicyFixture(t)
	owner.Spec.Storage.Format = STORAGE_DATABASE
	owner.Spec.Storage.DatabaseConfigurations = "db"
	recorder := record.NewFakeRecorder(10)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(owner).
		WithObjects(owner, createConfigMap(operatorNamespace, testKubeRBACProxyImage, testTrustAIServiceImage)).
		Build()
	r := &TrustyAIServiceReconciler{Client: c, Scheme: scheme, Namespace: operatorNamespace, EventRecorder: recorder}

	previous, previousErr := pkgtls.CurrentProxyTLSArguments(), pkgtls.CurrentProxyTLSArgumentsError()
	t.Cleanup(func() { pkgtls.SetProxyTLSArguments(previous, previousErr) })
	pkgtls.SetProxyTLSArguments(pkgtls.ProxyTLSArguments{}, errors.New("cannot serve HTTP/2 with the selected TLS 1.2 cipher list"))

	err := r.reconcileDeployment(ctx, owner, CustomCertificatesBundle{}, false)
	if err == nil || !strings.Contains(err.Error(), pkgtls.ProxyTLSProfileUnsupportedMessage) {
		t.Fatalf("expected the proxy TLS failure to fail the render, got %v", err)
	}

	current := &trustyaiopendatahubiov1.TrustyAIService{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(owner), current); err != nil {
		t.Fatal(err)
	}
	if current.Status.Ready != corev1.ConditionFalse || current.Status.Phase != PhaseNotReady {
		t.Fatalf("render failure left a stale readiness: ready=%s phase=%q", current.Status.Ready, current.Status.Phase)
	}
	ready, _, err := checkCondition(current.Status.Conditions, ConditionTypeReady, corev1.ConditionFalse, false)
	if err != nil || ready == nil || ready.Reason != pkgtls.ReasonProxyTLSProfileUnsupported {
		t.Fatalf("Ready condition does not report the TLS profile: %#v (%v)", ready, err)
	}
	provisioning, _, err := checkCondition(current.Status.Conditions, ConditionTypeProvisioningSucceeded, corev1.ConditionFalse, false)
	if err != nil || provisioning == nil || provisioning.Reason != "ProvisioningBlocked" {
		t.Fatalf("ProvisioningSucceeded condition does not report the blocked provisioning: %#v (%v)", provisioning, err)
	}
	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, pkgtls.ReasonProxyTLSProfileUnsupported) {
			t.Fatalf("warning event does not name the TLS profile reason: %q", event)
		}
	default:
		t.Fatal("expected a warning event for the unusable proxy TLS profile")
	}

	// Recovery: a usable profile renders again and the status pass replaces the
	// TLS reason with ordinary readiness reporting.
	pkgtls.SetProxyTLSArguments(previous, nil)
	if err := r.reconcileDeployment(ctx, owner, CustomCertificatesBundle{}, false); err != nil {
		t.Fatalf("usable profile must render the Deployment: %v", err)
	}
	if _, err := r.reconcileStatuses(ctx, current); err != nil {
		t.Fatalf("status reconciliation failed: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(owner), current); err != nil {
		t.Fatal(err)
	}
	ready, _, err = checkCondition(current.Status.Conditions, ConditionTypeReady, corev1.ConditionFalse, false)
	if err != nil || ready == nil || ready.Reason == pkgtls.ReasonProxyTLSProfileUnsupported {
		t.Fatalf("TLS profile reason survived recovery: %#v (%v)", ready, err)
	}
}
