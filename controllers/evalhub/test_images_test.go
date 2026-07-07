package evalhub

const (
	// testEvalHubImage and testKubeRBACProxyImage are the primary images used in the main
	// Ginkgo integration test ConfigMap (createConfigMap in suite_test.go).
	testEvalHubImage       = "quay.io/ruimvieira/eval-hub:test"
	testKubeRBACProxyImage = "quay.io/opendatahub/odh-kube-rbac-proxy:odh-stable"

	// testReconcilerEvalHubImage and testReconcilerKubeRBACProxyImage are used by setupReconciler
	// and the unit tests that build their own fake client.
	testReconcilerEvalHubImage       = "quay.io/evalhub/evalhub:test"
	testReconcilerKubeRBACProxyImage = "quay.io/openshift/origin-kube-rbac-proxy:4.19"

	// testBuildEvalHubImage and testBuildKubeRBACProxyImage are placeholder images used in
	// build_test.go and mcp_deployment_test.go to verify version-specific resolution.
	testBuildEvalHubImage       = "quay.io/test/eval-hub:v1.2.3"
	testBuildKubeRBACProxyImage = "quay.io/test/kube-rbac-proxy:v1"

	// testCustomEvalHubImage is used in build_test.go to test spec.image override behaviour.
	testCustomEvalHubImage = "quay.io/test/eval-hub:custom"

	// testEvalHubLatestImage and testKubeRBACProxyLatestImage are used in deployment_test.go
	// and unit_test.go for custom-image-override tests.
	testEvalHubLatestImage       = "quay.io/test/eval-hub:latest"
	testKubeRBACProxyLatestImage = "quay.io/test/kube-rbac-proxy:latest"

	// testMCPEvalHubImage is used in mcp_deployment_test.go (note: repo is "evalhub", not "eval-hub").
	testMCPEvalHubImage = "quay.io/test/evalhub:latest"
)
