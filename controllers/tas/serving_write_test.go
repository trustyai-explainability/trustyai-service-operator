package tas

import (
	"context"
	goerrors "errors"
	"strings"
	"testing"

	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const servingWriteTestNamespace = "serving-ns"

// testServingContract is the contract derived from the same fixture the
// environment test suite installs, so the write path is exercised with the
// version and resource the CRD actually declares.
func testServingContract() servingContract {
	return deriveServingContract(servingCRDFixture())
}

func testServingObject(name string, annotations map[string]string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "serving.kserve.io/v1beta1",
		"kind":       "InferenceService",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": servingWriteTestNamespace,
		},
		"spec": map[string]interface{}{
			"predictor": map[string]interface{}{
				"model": map[string]interface{}{"modelFormat": map[string]interface{}{"name": "sklearn"}},
			},
		},
	}}
	if len(annotations) > 0 {
		object.SetAnnotations(annotations)
	}
	return object
}

func newServingWriteReconciler(t *testing.T, objects ...runtime.Object) (*TrustyAIServiceReconciler, *dynamicfake.FakeDynamicClient) {
	t.Helper()

	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			testServingContract().gvr: "InferenceServiceList",
		}, objects...)

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("register core types: %v", err)
	}
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("register apiextensions types: %v", err)
	}

	return &TrustyAIServiceReconciler{
		Client:        fake.NewClientBuilder().WithScheme(scheme).Build(),
		Dynamic:       dynamicClient,
		Scheme:        scheme,
		Namespace:     operatorNamespace,
		EventRecorder: record.NewFakeRecorder(20),
	}, dynamicClient
}

func testServingInstance() *trustyaiopendatahubiov1.TrustyAIService {
	return &trustyaiopendatahubiov1.TrustyAIService{ObjectMeta: metav1.ObjectMeta{
		Name:      "trustyai-service",
		Namespace: servingWriteTestNamespace,
	}}
}

func fetchServingObject(t *testing.T, dynamicClient *dynamicfake.FakeDynamicClient, name string) *unstructured.Unstructured {
	t.Helper()
	object, err := dynamicClient.Resource(testServingContract().gvr).Namespace(servingWriteTestNamespace).
		Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get InferenceService %q: %v", name, err)
	}
	return object
}

// Payload capture is one field and three annotations, and the write has to keep
// three properties: it sets them, it does nothing when they already match, and it
// clears rather than empties on removal.
func TestPatchServingLoggerSetsTheConfiguredSink(t *testing.T) {
	ctx := context.Background()
	infService := testServingObject("my-model", nil)
	reconciler, dynamicClient := newServingWriteReconciler(t, infService)

	if err := reconciler.patchServingLogger(ctx, testServingInstance(), testServingContract(), infService, "trustyai-service", false, true); err != nil {
		t.Fatalf("patch payload capture: %v", err)
	}

	patched := fetchServingObject(t, dynamicClient, "my-model")
	logger, found, err := unstructured.NestedMap(patched.Object, predictorLoggerFieldPathParts...)
	if err != nil || !found {
		t.Fatalf("the logger was not set: found=%v err=%v", found, err)
	}
	wantURL := utils.GenerateHTTPSKServeLoggerURL("trustyai-service", servingWriteTestNamespace)
	if got := servingLoggerURLOf(logger); got != wantURL {
		t.Fatalf("logger url = %q, want %q", got, wantURL)
	}
	if mode, _, _ := unstructured.NestedString(logger, "mode"); mode != "all" {
		t.Fatalf("logger mode = %q, want all", mode)
	}
}

func TestPatchServingLoggerIsIdempotent(t *testing.T) {
	ctx := context.Background()
	infService := testServingObject("my-model", nil)
	reconciler, dynamicClient := newServingWriteReconciler(t, infService)

	if err := reconciler.patchServingLogger(ctx, testServingInstance(), testServingContract(), infService, "trustyai-service", false, true); err != nil {
		t.Fatalf("first patch: %v", err)
	}
	afterFirst := fetchServingObject(t, dynamicClient, "my-model").GetResourceVersion()

	if err := reconciler.patchServingLogger(ctx, testServingInstance(), testServingContract(), infService, "trustyai-service", false, true); err != nil {
		t.Fatalf("second patch: %v", err)
	}
	afterSecond := fetchServingObject(t, dynamicClient, "my-model").GetResourceVersion()

	if afterFirst != afterSecond {
		t.Fatalf("an unchanged logger was written again: resourceVersion %q then %q", afterFirst, afterSecond)
	}
}

func TestPatchServingLoggerRemovesTheFieldAndTheAnnotations(t *testing.T) {
	ctx := context.Background()
	wantURL := utils.GenerateKServeLoggerURL("trustyai-service", servingWriteTestNamespace)

	configured := testServingObject("my-model", map[string]string{
		"internal.serving.kserve.io/logger":          "true",
		"internal.serving.kserve.io/logger-mode":     "all",
		"internal.serving.kserve.io/logger-sink-url": wantURL,
		"unrelated": "kept",
	})
	if err := unstructured.SetNestedMap(configured.Object, map[string]interface{}{
		"mode": "all", "url": wantURL,
	}, predictorLoggerFieldPathParts...); err != nil {
		t.Fatalf("seed the logger: %v", err)
	}

	reconciler, dynamicClient := newServingWriteReconciler(t, configured)
	if err := reconciler.patchServingLogger(ctx, testServingInstance(), testServingContract(), configured, "trustyai-service", true, false); err != nil {
		t.Fatalf("remove payload capture: %v", err)
	}

	removed := fetchServingObject(t, dynamicClient, "my-model")
	if _, found, _ := unstructured.NestedMap(removed.Object, predictorLoggerFieldPathParts...); found {
		t.Fatalf("the logger field must be absent after removal, not empty: %v", removed.Object)
	}
	for _, key := range servingLoggerAnnotationKeys {
		if _, present := removed.GetAnnotations()[key]; present {
			t.Fatalf("annotation %q survived removal", key)
		}
	}
	if removed.GetAnnotations()["unrelated"] != "kept" {
		t.Fatalf("removal dropped an annotation this operator does not own: %v", removed.GetAnnotations())
	}
}

func TestPatchServingLoggerDoesNotWriteWhenRemovalHasNothingToRemove(t *testing.T) {
	ctx := context.Background()
	infService := testServingObject("my-model", nil)
	reconciler, dynamicClient := newServingWriteReconciler(t, infService)

	before := fetchServingObject(t, dynamicClient, "my-model").GetResourceVersion()
	if err := reconciler.patchServingLogger(ctx, testServingInstance(), testServingContract(), infService, "trustyai-service", true, false); err != nil {
		t.Fatalf("removal on an unconfigured object: %v", err)
	}
	after := fetchServingObject(t, dynamicClient, "my-model").GetResourceVersion()

	if before != after {
		t.Fatalf("removal wrote an object it had nothing to remove from: %q then %q", before, after)
	}
}

// odh-model-controller and the serving controllers write the same objects, so a
// blind update loses the race and the logger never lands. The write retries.
func TestPatchServingLoggerRetriesOnConflict(t *testing.T) {
	ctx := context.Background()
	infService := testServingObject("my-model", nil)
	reconciler, dynamicClient := newServingWriteReconciler(t, infService)

	conflicts := 0
	dynamicClient.PrependReactor("update", "inferenceservices", func(k8stesting.Action) (bool, runtime.Object, error) {
		if conflicts == 0 {
			conflicts++
			return true, nil, errors.NewConflict(schema.GroupResource{Group: "serving.kserve.io", Resource: "inferenceservices"}, "my-model", nil)
		}
		return false, nil, nil
	})

	if err := reconciler.patchServingLogger(ctx, testServingInstance(), testServingContract(), infService, "trustyai-service", false, true); err != nil {
		t.Fatalf("patch under conflict: %v", err)
	}
	if conflicts != 1 {
		t.Fatalf("expected one conflict before the retry, saw %d", conflicts)
	}
	if logger, found, _ := unstructured.NestedMap(fetchServingObject(t, dynamicClient, "my-model").Object, predictorLoggerFieldPathParts...); !found || servingLoggerURLOf(logger) == "" {
		t.Fatalf("the logger was not written after the retry")
	}
}

// A validation or authorization rejection is a state for that object: retrying
// every requeue cannot fix it, so it is reported instead of being returned as a
// transient error the controller would spin on.
func TestPatchServingLoggerClassifiesRejections(t *testing.T) {
	ctx := context.Background()
	infService := testServingObject("my-model", nil)
	reconciler, dynamicClient := newServingWriteReconciler(t, infService)

	dynamicClient.PrependReactor("update", "inferenceservices", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.NewForbidden(schema.GroupResource{Group: "serving.kserve.io", Resource: "inferenceservices"}, "my-model", nil)
	})

	err := reconciler.patchServingLogger(ctx, testServingInstance(), testServingContract(), infService, "trustyai-service", false, true)
	var rejected *servingObjectRejectedError
	if !goerrors.As(err, &rejected) {
		t.Fatalf("expected a per-object rejection, got %v", err)
	}
	if rejected.namespace != servingWriteTestNamespace || rejected.name != "my-model" {
		t.Fatalf("the rejection does not name the object: %#v", rejected)
	}
}

// The service-mesh objects are only ensured for an object that asks for the Istio
// sidecar, and a missing CRD is logged rather than failing the reconcile: the
// logger write is still correct without them.
func TestPatchServingLoggerConfiguresOnlyAnnotatedObjectsForTheMesh(t *testing.T) {
	ctx := context.Background()
	annotated := testServingObject("my-model", map[string]string{istioInjectAnnotation: "true"})
	reconciler, dynamicClient := newServingWriteReconciler(t, annotated)

	if err := reconciler.patchServingLogger(ctx, testServingInstance(), testServingContract(), annotated, "trustyai-service", false, true); err != nil {
		t.Fatalf("patch an annotated object without the mesh CRDs: %v", err)
	}
	if logger, found, _ := unstructured.NestedMap(fetchServingObject(t, dynamicClient, "my-model").Object, predictorLoggerFieldPathParts...); !found || servingLoggerURLOf(logger) == "" {
		t.Fatalf("the logger must still be configured when the mesh CRDs are absent")
	}
}

// The two deployed shapes do not share a sink URL: Raw/Standard predictors can
// present the service certificate, Serverless/Knative ones cannot.
func TestServingLoggerURLDiffersByDeploymentShape(t *testing.T) {
	httpsURL := servingLoggerURL("trustyai-service", servingWriteTestNamespace, true)
	plainURL := servingLoggerURL("trustyai-service", servingWriteTestNamespace, false)

	if httpsURL == plainURL {
		t.Fatalf("both deployment shapes got the same sink URL %q", httpsURL)
	}
	if !strings.HasPrefix(httpsURL, "https://") {
		t.Fatalf("the Raw/Standard sink must be the TLS endpoint, got %q", httpsURL)
	}
	if strings.HasPrefix(plainURL, "https://") {
		t.Fatalf("the Serverless/Knative sink must not require TLS, got %q", plainURL)
	}
}
