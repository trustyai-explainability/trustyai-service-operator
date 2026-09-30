package evalhub

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	evalhubv1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	yamlv3 "gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func int32Pointer(value int32) *int32 {
	return &value
}

func TestRenderCollectionConfigMapData(t *testing.T) {
	source := map[string]string{
		"collection.yaml": "id: collection-a\nname: Collection A\ntags:\n  - trusted\nbenchmarks:\n  - id: benchmark-a\n",
	}

	t.Run("applies a positive curation order without mutating source data", func(t *testing.T) {
		rendered, err := renderCollectionConfigMapData(source, "collection-a", int32Pointer(2))
		if err != nil {
			t.Fatalf("renderCollectionConfigMapData() error = %v", err)
		}
		if strings.Contains(source["collection.yaml"], "curation_order") {
			t.Fatal("source data was modified")
		}

		var collection struct {
			ID            string `yaml:"id"`
			Name          string `yaml:"name"`
			CurationOrder *int32 `yaml:"curation_order"`
		}
		if err := yamlv3.Unmarshal([]byte(rendered["collection.yaml"]), &collection); err != nil {
			t.Fatalf("unmarshal rendered YAML: %v", err)
		}
		if collection.ID != "collection-a" || collection.Name != "Collection A" {
			t.Fatalf("rendered collection identity = %#v", collection)
		}
		if collection.CurationOrder == nil || *collection.CurationOrder != 2 {
			t.Fatalf("curation_order = %v, want 2", collection.CurationOrder)
		}
	})

	t.Run("renders an explicit zero", func(t *testing.T) {
		rendered, err := renderCollectionConfigMapData(source, "collection-a", int32Pointer(0))
		if err != nil {
			t.Fatalf("renderCollectionConfigMapData() error = %v", err)
		}
		var collection struct {
			CurationOrder *int32 `yaml:"curation_order"`
		}
		if err := yamlv3.Unmarshal([]byte(rendered["collection.yaml"]), &collection); err != nil {
			t.Fatalf("unmarshal rendered YAML: %v", err)
		}
		if collection.CurationOrder == nil || *collection.CurationOrder != 0 {
			t.Fatalf("curation_order = %v, want explicit 0", collection.CurationOrder)
		}
	})

	t.Run("retains source data when the override is omitted", func(t *testing.T) {
		rendered, err := renderCollectionConfigMapData(source, "collection-a", nil)
		if err != nil {
			t.Fatalf("renderCollectionConfigMapData() error = %v", err)
		}
		if !reflect.DeepEqual(rendered, source) {
			t.Fatalf("rendered data = %#v, want %#v", rendered, source)
		}
	})

	t.Run("rejects missing and malformed collection YAML", func(t *testing.T) {
		_, err := renderCollectionConfigMapData(map[string]string{"other.yaml": "id: other\n"}, "collection-a", int32Pointer(1))
		if err == nil || !strings.Contains(err.Error(), "no collection YAML document") {
			t.Fatalf("missing collection error = %v", err)
		}

		_, err = renderCollectionConfigMapData(map[string]string{"collection.yaml": "id: [\n"}, "collection-a", int32Pointer(1))
		if err == nil || !strings.Contains(err.Error(), "parse data key") {
			t.Fatalf("malformed YAML error = %v", err)
		}
	})
}

func TestCollectionOverridesByName(t *testing.T) {
	order := int32(3)
	overrides, err := collectionOverridesByName([]string{"collection-a"}, []evalhubv1.SystemCollectionOverride{{
		Collection:    "collection-a",
		CurationOrder: &order,
	}})
	if err != nil {
		t.Fatalf("collectionOverridesByName() error = %v", err)
	}
	if got := overrides["collection-a"].CurationOrder; got == nil || *got != 3 {
		t.Fatalf("curation order = %v, want 3", got)
	}

	for _, tc := range []struct {
		name      string
		overrides []evalhubv1.SystemCollectionOverride
		want      string
	}{
		{name: "unselected", overrides: []evalhubv1.SystemCollectionOverride{{Collection: "other"}}, want: "does not match"},
		{name: "duplicate", overrides: []evalhubv1.SystemCollectionOverride{{Collection: "collection-a"}, {Collection: "collection-a"}}, want: "duplicate"},
		{name: "blank", overrides: []evalhubv1.SystemCollectionOverride{{Collection: " "}}, want: "must specify"},
		{name: "negative", overrides: []evalhubv1.SystemCollectionOverride{{Collection: "collection-a", CurationOrder: int32Pointer(-1)}}, want: "negative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := collectionOverridesByName([]string{"collection-a"}, tc.overrides)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestReconcileCollectionConfigMapsRejectsUnselectedOverrideWithoutCollections(t *testing.T) {
	instance := &evalhubv1.EvalHub{
		Spec: evalhubv1.EvalHubSpec{
			CollectionOverrides: []evalhubv1.SystemCollectionOverride{{Collection: "collection-a"}},
		},
	}
	reconciler := &EvalHubReconciler{}
	if _, err := reconciler.reconcileCollectionConfigMaps(context.Background(), instance); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want unselected collection override rejection", err)
	}
}

func TestReconcileCollectionConfigMapsAppliesSystemCollectionOverride(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := evalhubv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	const (
		operatorNamespace = "operator-ns"
		instanceNamespace = "instance-ns"
		collectionName    = "collection-a"
	)
	order := int32(2)
	source := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-collection",
			Namespace: operatorNamespace,
			Labels: map[string]string{
				collectionLabel:     "system",
				collectionNameLabel: collectionName,
			},
		},
		Data: map[string]string{"collection.yaml": "id: collection-a\nname: Collection A\nbenchmarks:\n  - id: benchmark-a\n"},
	}
	instance := &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "evalhub", Namespace: instanceNamespace},
		Spec: evalhubv1.EvalHubSpec{
			Collections: []string{collectionName},
			CollectionOverrides: []evalhubv1.SystemCollectionOverride{{
				Collection:    collectionName,
				CurationOrder: &order,
			}},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source).Build()
	reconciler := &EvalHubReconciler{Client: client, Scheme: scheme, Namespace: operatorNamespace}
	cmNames, err := reconciler.reconcileCollectionConfigMaps(context.Background(), instance)
	if err != nil {
		t.Fatalf("reconcileCollectionConfigMaps() error = %v", err)
	}
	if !reflect.DeepEqual(cmNames, []string{"evalhub-collection-collection-a"}) {
		t.Fatalf("ConfigMap names = %v", cmNames)
	}

	target := &corev1.ConfigMap{}
	if err := client.Get(context.Background(), types.NamespacedName{Name: cmNames[0], Namespace: instanceNamespace}, target); err != nil {
		t.Fatalf("get target ConfigMap: %v", err)
	}
	if strings.Contains(source.Data["collection.yaml"], "curation_order") {
		t.Fatal("source ConfigMap data was modified")
	}
	if !strings.Contains(target.Data["collection.yaml"], "curation_order: 2") {
		t.Fatalf("target ConfigMap did not contain rendered curation order:\n%s", target.Data["collection.yaml"])
	}
}

func TestReconcileCollectionConfigMapsDoesNotPartiallyUpdateOnInvalidOverride(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := evalhubv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	const (
		operatorNamespace = "operator-ns"
		instanceNamespace = "instance-ns"
	)
	order := int32(1)
	firstSource := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "first-source", Namespace: operatorNamespace, Labels: map[string]string{collectionLabel: "system", collectionNameLabel: "first"}},
		Data:       map[string]string{"first.yaml": "id: first\nname: First\n"},
	}
	secondSource := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "second-source", Namespace: operatorNamespace, Labels: map[string]string{collectionLabel: "system", collectionNameLabel: "second"}},
		Data:       map[string]string{"second.yaml": "id: [\n"},
	}
	existingTarget := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "evalhub-collection-first", Namespace: instanceNamespace},
		Data:       map[string]string{"first.yaml": "id: first\nname: Existing\n"},
	}
	instance := &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "evalhub", Namespace: instanceNamespace},
		Spec: evalhubv1.EvalHubSpec{
			Collections: []string{"first", "second"},
			CollectionOverrides: []evalhubv1.SystemCollectionOverride{
				{Collection: "first", CurationOrder: &order},
				{Collection: "second", CurationOrder: &order},
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(firstSource, secondSource, existingTarget).Build()
	reconciler := &EvalHubReconciler{Client: client, Scheme: scheme, Namespace: operatorNamespace}
	if _, err := reconciler.reconcileCollectionConfigMaps(context.Background(), instance); err == nil {
		t.Fatal("expected invalid YAML error")
	}

	target := &corev1.ConfigMap{}
	if err := client.Get(context.Background(), types.NamespacedName{Name: existingTarget.Name, Namespace: instanceNamespace}, target); err != nil {
		t.Fatalf("get existing target ConfigMap: %v", err)
	}
	if target.Data["first.yaml"] != existingTarget.Data["first.yaml"] {
		t.Fatalf("first target was partially updated: %q", target.Data["first.yaml"])
	}
}

func TestReconcileCollectionConfigMapsRejectsOverrideForTenantFallback(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := evalhubv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	const (
		operatorNamespace = "operator-ns"
		instanceNamespace = "instance-ns"
		collectionName    = "tenant-collection"
	)
	tenantSource := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-source",
			Namespace: instanceNamespace,
			Labels: map[string]string{
				collectionLabel:     collectionTenantValue,
				collectionNameLabel: collectionName,
			},
		},
		Data: map[string]string{"collection.yaml": "id: tenant-collection\nname: Tenant Collection\n"},
	}
	instance := &evalhubv1.EvalHub{
		ObjectMeta: metav1.ObjectMeta{Name: "evalhub", Namespace: instanceNamespace},
		Spec: evalhubv1.EvalHubSpec{
			Tenancy:     evalhubv1.TenancySingle,
			Collections: []string{collectionName},
			CollectionOverrides: []evalhubv1.SystemCollectionOverride{{
				Collection: collectionName,
			}},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tenantSource).Build()
	reconciler := &EvalHubReconciler{Client: client, Scheme: scheme, Namespace: operatorNamespace}
	if _, err := reconciler.reconcileCollectionConfigMaps(context.Background(), instance); err == nil || !strings.Contains(err.Error(), "operator-packaged system collection") {
		t.Fatalf("error = %v, want system collection rejection", err)
	}
}

var _ = Describe("EvalHub system collection ConfigMap propagation", func() {
	const collectionName = "collection-a"

	var (
		operatorNamespace string
		instanceNamespace string
		operatorNS        *corev1.Namespace
		instanceNS        *corev1.Namespace
		instance          *evalhubv1.EvalHub
		reconciler        *EvalHubReconciler
	)

	BeforeEach(func() {
		suffix := time.Now().UnixNano()
		operatorNamespace = fmt.Sprintf("evalhub-collection-source-%d", suffix)
		instanceNamespace = fmt.Sprintf("evalhub-collection-target-%d", suffix)
		operatorNS = createNamespace(operatorNamespace)
		instanceNS = createNamespace(instanceNamespace)
		Expect(k8sClient.Create(ctx, operatorNS)).To(Succeed())
		Expect(k8sClient.Create(ctx, instanceNS)).To(Succeed())

		instance = createEvalHubInstance("collection-propagation", instanceNamespace)
		instance.Spec.Collections = []string{collectionName}
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		reconciler = &EvalHubReconciler{
			Client:    k8sClient,
			Scheme:    scheme.Scheme,
			Namespace: operatorNamespace,
		}
	})

	AfterEach(func() {
		deleteNamespace(instanceNS)
		deleteNamespace(operatorNS)
	})

	It("propagates source curation_order changes on a later reconciliation", func() {
		source := systemCollectionConfigMap(operatorNamespace, collectionName, "Initial Collection", 1)
		Expect(k8sClient.Create(ctx, source)).To(Succeed())

		_, err := reconciler.reconcileCollectionConfigMaps(ctx, instance)
		Expect(err).NotTo(HaveOccurred())
		Expect(readCollectionConfig(instanceNamespace, instance.Name, collectionName)).To(Equal(collectionConfig{
			ID: "collection-a", Name: "Initial Collection", CurationOrder: 1,
		}))

		source.Data["collection.yaml"] = "id: collection-a\nname: Updated Collection\ncuration_order: 7\n"
		Expect(k8sClient.Update(ctx, source)).To(Succeed())

		_, err = reconciler.reconcileCollectionConfigMaps(ctx, instance)
		Expect(err).NotTo(HaveOccurred())
		Expect(readCollectionConfig(instanceNamespace, instance.Name, collectionName)).To(Equal(collectionConfig{
			ID: "collection-a", Name: "Updated Collection", CurationOrder: 7,
		}))
	})

	It("updates the target when the curation_order override in the EvalHub CR changes", func() {
		source := systemCollectionConfigMap(operatorNamespace, collectionName, "Packaged Collection", 1)
		Expect(k8sClient.Create(ctx, source)).To(Succeed())

		instance.Spec.CollectionOverrides = []evalhubv1.SystemCollectionOverride{{
			Collection:    collectionName,
			CurationOrder: int32Pointer(2),
		}}
		Expect(k8sClient.Update(ctx, instance)).To(Succeed())

		persisted := &evalhubv1.EvalHub{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, persisted)).To(Succeed())
		_, err := reconciler.reconcileCollectionConfigMaps(ctx, persisted)
		Expect(err).NotTo(HaveOccurred())
		Expect(readCollectionConfig(instanceNamespace, instance.Name, collectionName)).To(Equal(collectionConfig{
			ID: "collection-a", Name: "Packaged Collection", CurationOrder: 2,
		}))

		persisted.Spec.CollectionOverrides[0].CurationOrder = int32Pointer(4)
		Expect(k8sClient.Update(ctx, persisted)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, persisted)).To(Succeed())

		_, err = reconciler.reconcileCollectionConfigMaps(ctx, persisted)
		Expect(err).NotTo(HaveOccurred())
		Expect(readCollectionConfig(instanceNamespace, instance.Name, collectionName)).To(Equal(collectionConfig{
			ID: "collection-a", Name: "Packaged Collection", CurationOrder: 4,
		}))
	})
})

type collectionConfig struct {
	ID            string `yaml:"id"`
	Name          string `yaml:"name"`
	CurationOrder int32  `yaml:"curation_order"`
}

func systemCollectionConfigMap(namespace, collectionName, name string, curationOrder int32) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "system-" + collectionName,
			Namespace: namespace,
			Labels: map[string]string{
				collectionLabel:     "system",
				collectionNameLabel: collectionName,
			},
		},
		Data: map[string]string{
			"collection.yaml": fmt.Sprintf("id: %s\nname: %s\ncuration_order: %d\n", collectionName, name, curationOrder),
		},
	}
}

func readCollectionConfig(namespace, instanceName, collectionName string) collectionConfig {
	target := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Name:      instanceName + "-collection-" + collectionName,
		Namespace: namespace,
	}, target)).To(Succeed())

	var collection collectionConfig
	Expect(yamlv3.Unmarshal([]byte(target.Data["collection.yaml"]), &collection)).To(Succeed())
	return collection
}
