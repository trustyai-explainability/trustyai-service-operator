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

	odhLabels "github.com/opendatahub-io/odh-platform-utilities/pkg/metadata/labels"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/render/kustomize"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// manifestsTarget is the writable runtime copy of the manifests template.
	manifestsTarget = "/opt/manifests"

	paramsEnvFile = "params.env"
)

var (
	copyOnce sync.Once
	copyErr  error
)

// EnsureManifests copies templatePath to a writable location (once per process),
// then selects and prepares the overlay directory for the current platform and mode.
//
// The file copy is performed only once; overlay selection and params rewriting
// happen on every call so that mode changes (e.g. MCPGuardrailsMode toggled on
// the live CR) take effect without restarting the operator.
func EnsureManifests(templatePath string, mcpMode bool) (string, error) {
	copyOnce.Do(func() {
		// Clear contents without removing the directory itself — it may be an
		// EmptyDir mount point that the OS won't let us unlink.
		if err := clearDirContents(manifestsTarget); err != nil {
			copyErr = fmt.Errorf("clearing manifests target %s: %w", manifestsTarget, err)
			return
		}
		if err := os.MkdirAll(manifestsTarget, 0o755); err != nil {
			copyErr = fmt.Errorf("creating manifests target %s: %w", manifestsTarget, err)
			return
		}
		copyErr = copyDir(templatePath, manifestsTarget)
	})
	if copyErr != nil {
		return "", copyErr
	}

	overlay := selectOverlay(manifestsTarget, mcpMode)
	if err := applyParams(overlay); err != nil {
		return "", fmt.Errorf("applying image params to overlay %s: %w", overlay, err)
	}
	return overlay, nil
}

// selectOverlay returns the kustomize overlay directory path for the current
// platform and mode. MCPGuardrailsMode takes priority over ODH_PLATFORM_TYPE:
// when true it always selects overlays/mcp-guardrails regardless of platform.
// Otherwise the platform is derived from ODH_PLATFORM_TYPE; defaults to ODH.
func selectOverlay(manifestsDir string, mcpMode bool) string {
	if mcpMode {
		return filepath.Join(manifestsDir, "overlays/mcp-guardrails")
	}
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
func RenderManifests(ctx context.Context, templatePath, namespace string, mcpMode bool) ([]unstructured.Unstructured, error) {
	logger := log.FromContext(ctx)

	overlay, err := EnsureManifests(templatePath, mcpMode)
	if err != nil {
		return nil, fmt.Errorf("staging manifests: %w", err)
	}

	logger.Info("Rendering manifests", "overlay", overlay, "namespace", namespace)

	objs, err := kustomize.Render(overlay, nil, kustomize.WithNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("rendering kustomize overlay %s: %w", overlay, err)
	}

	applyPlatformLabels(objs)
	logger.Info("Rendered manifests", "count", len(objs))
	return objs, nil
}

// applyPlatformLabels stamps every rendered resource with the ODH platform
// labels required by MC-22: ManagedBy identifies the module operator that owns
// the resource; PlatformPartOf identifies the component (used by GC selectors).
func applyPlatformLabels(objs []unstructured.Unstructured) {
	for i := range objs {
		lbls := objs[i].GetLabels()
		if lbls == nil {
			lbls = make(map[string]string)
		}
		lbls[odhLabels.ManagedBy] = FieldManagerModule
		lbls[odhLabels.PlatformPartOf] = "trustyai"
		objs[i].SetLabels(lbls)
	}
}

func clearDirContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
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
