package tas

import (
	"context"
	"fmt"
	"sync"
	"time"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	trustyaiopendatahubiov1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// recordingCache wraps a cache and records every kind an informer was requested
// for, which is the only way to observe the controller's source set from a test.
type recordingCache struct {
	cache.Cache
	scheme    *runtime.Scheme
	mu        sync.Mutex
	requested []schema.GroupVersionKind
}

func (c *recordingCache) record(gvk schema.GroupVersionKind) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requested = append(c.requested, gvk)
}

func (c *recordingCache) GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind, opts ...cache.InformerGetOption) (cache.Informer, error) {
	c.record(gvk)
	return c.Cache.GetInformerForKind(ctx, gvk, opts...)
}

func (c *recordingCache) GetInformer(ctx context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error) {
	// A typed client reaches the cache with an empty TypeMeta; the kind is
	// resolved from the scheme, which is what the source ends up watching.
	if gvk, err := apiutil.GVKForObject(obj, c.scheme); err == nil {
		c.record(gvk)
	} else {
		c.record(obj.GetObjectKind().GroupVersionKind())
	}
	return c.Cache.GetInformer(ctx, obj, opts...)
}

func (c *recordingCache) requestedGroups() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	groups := make([]string, 0, len(c.requested))
	for _, gvk := range c.requested {
		groups = append(groups, gvk.Group)
	}
	return groups
}

// The serving CRD is another component's API and its presence is not a
// precondition for this operator. Registering a source for it made the manager
// exit on a cluster without serving, because the informer could never resolve
// and the controller missed its cache-sync deadline, which is the restart loop
// the change removes. Convergence is the reconcile requeue instead, so no
// informer may be requested for serving.kserve.io.
var _ = Describe("TrustyAIService controller sources", func() {

	newScheme := func() *runtime.Scheme {
		s := runtime.NewScheme()
		Expect(corev1.AddToScheme(s)).To(Succeed())
		Expect(appsv1.AddToScheme(s)).To(Succeed())
		Expect(networkingv1.AddToScheme(s)).To(Succeed())
		Expect(trustyaiopendatahubiov1.AddToScheme(s)).To(Succeed())
		// Registered deliberately: the assertion is about what the controller
		// asks the cache for, not about what the scheme can resolve.
		Expect(kservev1beta1.AddToScheme(s)).To(Succeed())
		return s
	}

	It("registers no informer for serving.kserve.io", func() {
		var recorded *recordingCache
		scheme := newScheme()

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
			// The suite's own manager already owns the trustyaiservice
			// controller name; a second registration in the same process is not
			// a duplicate metric source.
			Controller: config.Controller{SkipNameValidation: ptr.To(true)},
			NewCache: func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
				base, err := cache.New(config, opts)
				if err != nil {
					return nil, err
				}
				recorded = &recordingCache{Cache: base, scheme: scheme}
				return recorded, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect((&TrustyAIServiceReconciler{
			Client:        mgr.GetClient(),
			APIReader:     mgr.GetAPIReader(),
			Scheme:        mgr.GetScheme(),
			EventRecorder: record.NewFakeRecorder(10),
		}).SetupWithManager(mgr)).To(Succeed())

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// A controller asks the cache for its sources while it starts, which is
		// not ordered against the manager's own cache sync: reading the recorded
		// set once after sync can legitimately see nothing yet and pass without
		// having observed the controller at all. Both invariants are therefore
		// held for the whole liveness window, so a request that arrives late is
		// still caught.
		var (
			startMu   sync.Mutex
			startDone bool
			startErr  error
		)
		go func() {
			err := mgr.Start(ctx)
			startMu.Lock()
			startDone, startErr = true, err
			startMu.Unlock()
		}()

		startedAndServing := func() string {
			startMu.Lock()
			done, err := startDone, startErr
			startMu.Unlock()
			if done {
				return fmt.Sprintf("the manager returned during the window: %v", err)
			}
			for _, group := range recorded.requestedGroups() {
				if group == "serving.kserve.io" {
					return "an informer was requested for serving.kserve.io"
				}
			}
			return "running, no serving informer"
		}

		Consistently(startedAndServing, 3*time.Second, 100*time.Millisecond).
			Should(Equal("running, no serving informer"),
				"the controller must not request a serving informer, and the manager must stay up")

		// Non-vacuity: the controller did ask for its own resource, so the
		// recorded set was observed rather than merely empty.
		Expect(recorded).NotTo(BeNil())
		Expect(recorded.requestedGroups()).To(ContainElement("trustyai.opendatahub.io"),
			"no informer was requested at all, so the absence of a serving one proves nothing")
	})
})
