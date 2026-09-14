package trustyaimodule

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/opendatahub-io/odh-platform-utilities/pkg/render/kustomize"
	platformv1alpha1 "github.com/trustyai-explainability/trustyai-operator-module/pkg/apis/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	paramsEnvFile = "params.env"
)

// manifestsTarget returns the writable runtime copy of the manifests template.
// The environment override keeps unit/integration tests non-root friendly while
// retaining the image's established /opt/manifests location by default.
func manifestsTarget() string {
	if target := os.Getenv("TRUSTYAI_MANIFESTS_TARGET"); target != "" {
		return target
	}
	return "/opt/manifests"
}

var (
	stagingOnce   sync.Once
	stagingErr    error
	stagedOverlay string
)

// EnsureManifests copies templatePath to a writable location (once per
// process), selects the overlay directory based on ODH_PLATFORM_TYPE, and
// rewrites params.env with platform-injected image env vars.
//
// Subsequent calls return the cached overlay path without re-running.
func EnsureManifests(templatePath string) (string, error) {
	stagingOnce.Do(func() {
		overlay, err := stageManifests(templatePath, manifestsTarget())
		stagedOverlay = overlay
		stagingErr = err
	})
	return stagedOverlay, stagingErr
}

func stageManifests(src, dst string) (string, error) {
	if err := os.RemoveAll(dst); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("clearing manifests target %s: %w", dst, err)
	}
	if err := copyDir(src, dst); err != nil {
		return "", fmt.Errorf("copying manifests from %s to %s: %w", src, dst, err)
	}

	overlay := selectOverlay(dst)
	if err := applyParams(overlay); err != nil {
		return "", fmt.Errorf("applying image params to overlay %s: %w", overlay, err)
	}
	return overlay, nil
}

// selectOverlay returns the kustomize overlay directory path for the current
// platform, derived from the ODH_PLATFORM_TYPE environment variable.
// Defaults to the ODH overlay when the variable is absent or unrecognised.
func selectOverlay(manifestsDir string) string {
	platform := strings.ToLower(os.Getenv("ODH_PLATFORM_TYPE"))
	sub := "overlays/odh"
	if strings.Contains(platform, "rhoai") ||
		strings.Contains(platform, "self-managed") ||
		strings.Contains(platform, "cloud") {
		sub = "overlays/rhoai"
	}
	return filepath.Join(manifestsDir, sub)
}

// applyParams rewrites params.env inside overlayDir, replacing placeholder
// image values with platform-injected image env vars.
// It is a no-op when params.env does not exist.
func applyParams(overlayDir string) error {
	paramsPath := filepath.Join(overlayDir, paramsEnvFile)

	data, err := os.ReadFile(paramsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading params.env: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if envVar, ok := paramsEnvMap[key]; ok {
			if val := os.Getenv(envVar); val != "" {
				lines[i] = key + "=" + val
			}
		}
	}

	return os.WriteFile(paramsPath, []byte(strings.Join(lines, "\n")), 0o644)
}

// RenderManifests stages the manifests (once) and renders the selected
// Kustomize overlay into a list of unstructured resources, injecting
// namespace into all namespaced resources.
func RenderManifests(ctx context.Context, templatePath, namespace string) ([]unstructured.Unstructured, error) {
	logger := log.FromContext(ctx)

	overlay, err := EnsureManifests(templatePath)
	if err != nil {
		return nil, fmt.Errorf("staging manifests: %w", err)
	}

	logger.Info("Rendering manifests", "overlay", overlay, "namespace", namespace)

	objs, err := kustomize.Render(overlay, nil, kustomize.WithNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("rendering kustomize overlay %s: %w", overlay, err)
	}

	filtered := filterUnsupportedResources(objs)
	logger.Info("Rendered manifests", "count", len(filtered), "skipped", len(objs)-len(filtered))
	return filtered, nil
}

// filterUnsupportedResources removes resources that require cluster-admin
// authority to create. Kubernetes restricts ClusterRoles with an
// aggregationRule to cluster-admin, so a least-privilege module operator
// cannot safely manage those platform-level aggregate roles. The ordinary
// user/editor/viewer roles remain in the rendered set and are sufficient for
// the TrustyAI workload APIs.
func filterUnsupportedResources(objs []unstructured.Unstructured) []unstructured.Unstructured {
	filtered := make([]unstructured.Unstructured, 0, len(objs))
	for _, obj := range objs {
		if obj.GetKind() == "ClusterRole" {
			if _, found, _ := unstructured.NestedMap(obj.Object, "aggregationRule"); found {
				continue
			}
		}
		filtered = append(filtered, obj)
	}
	return filtered
}

// enabledServiceNames maps EnabledServices booleans to the canonical service
// names accepted by trustyai-service-operator's --enable-services flag
// (see controllers/<service>/constants.go ServiceName in the parent repo).
func enabledServiceNames(es platformv1alpha1.EnabledServices) []string {
	var names []string
	if es.TAS {
		names = append(names, "TAS")
	}
	if es.LMES {
		names = append(names, "LMES")
	}
	if es.EvalHub {
		names = append(names, "EVALHUB")
	}
	if es.GORCH {
		names = append(names, "GORCH")
	}
	if es.NemoGuardrails {
		names = append(names, "NEMO_GUARDRAILS")
	}
	if len(names) == 0 {
		names = []string{"TAS", "LMES", "EVALHUB", "GORCH", "NEMO_GUARDRAILS"}
	}
	return names
}

// injectEnabledServices sets the --enable-services argument on
// OperatorDeploymentName's ManagerContainerName container, derived from the
// module CR's spec.enabledServices.
func injectEnabledServices(objs []unstructured.Unstructured, es platformv1alpha1.EnabledServices) error {
	arg := "--enable-services=" + strings.Join(enabledServiceNames(es), ",")

	for i := range objs {
		obj := &objs[i]
		if obj.GetKind() != "Deployment" || obj.GetName() != OperatorDeploymentName {
			continue
		}

		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		if err != nil || !found {
			return fmt.Errorf("reading containers from Deployment %s: %w", obj.GetName(), err)
		}

		for j, c := range containers {
			container, ok := c.(map[string]interface{})
			if !ok || container["name"] != ManagerContainerName {
				continue
			}

			args, _, err := unstructured.NestedSlice(container, "args")
			if err != nil {
				return fmt.Errorf("reading args from %s: %w", ManagerContainerName, err)
			}
			filtered := make([]interface{}, 0, len(args)+1)
			for k := 0; k < len(args); k++ {
				value, ok := args[k].(string)
				if ok && value == "--enable-services" {
					if k+1 < len(args) {
						k++
					}
					continue
				}
				if ok && strings.HasPrefix(value, "--enable-services=") {
					continue
				}
				filtered = append(filtered, args[k])
			}
			container["args"] = append(filtered, arg)
			containers[j] = container
		}

		if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
			return fmt.Errorf("setting containers on Deployment %s: %w", obj.GetName(), err)
		}
	}

	return nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close() //nolint:errcheck

	_, err = io.Copy(out, in)
	return err
}
