package tas

import (
	"context"
	"fmt"
	"reflect"

	trustyaiv1 "github.com/trustyai-explainability/trustyai-service-operator/api/tas/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var tasTLSEnvNames = map[string]bool{
	"TLS_POLICY": true, "TLS_REQUIRED": true, "TLS_CERT_FILE": true,
	"TLS_KEY_FILE": true, "TLS_MIN_VERSION": true, "TLS_CIPHERS": true,
	"TLS_CURVES": true, "HTTP_PORT": true, "HEALTH_PORT": true,
}

// ensureTASTLSDeployment upgrades old native-TLS pods and refreshes both proxies
// when the effective cluster policy changes. Changes to a PodTemplate roll pods;
// API-server defaulted fields must not cause a rollout on every reconciliation.
func (r *TrustyAIServiceReconciler) ensureTASTLSDeployment(ctx context.Context, owner *trustyaiv1.TrustyAIService, current, desired *appsv1.Deployment) error {
	if !metav1.IsControlledBy(current, owner) {
		return fmt.Errorf("refusing to change TLS on unowned Deployment %s/%s", current.Namespace, current.Name)
	}
	before := current.DeepCopy()
	if err := applyTASTLSDeployment(current, desired); err != nil {
		return err
	}
	if reflect.DeepEqual(before.Spec.Template, current.Spec.Template) {
		return nil
	}
	return r.Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

func applyTASTLSDeployment(current, desired *appsv1.Deployment) error {
	app, wantedApp := serviceContainer(current), serviceContainer(desired)
	if app == nil || wantedApp == nil {
		return fmt.Errorf("TAS service container missing while configuring TLS")
	}
	wanted := map[string]corev1.EnvVar{}
	for _, env := range wantedApp.Env {
		if tasTLSEnvNames[env.Name] {
			wanted[env.Name] = env
		}
	}
	seen := map[string]bool{}
	envs := make([]corev1.EnvVar, 0, len(app.Env))
	for _, env := range app.Env {
		if !tasTLSEnvNames[env.Name] {
			envs = append(envs, env)
			continue
		}
		if replacement, ok := wanted[env.Name]; ok && !seen[env.Name] {
			envs = append(envs, replacement)
			seen[env.Name] = true
		}
	}
	for _, env := range wantedApp.Env {
		if tasTLSEnvNames[env.Name] && !seen[env.Name] {
			envs = append(envs, env)
			seen[env.Name] = true
		}
	}
	app.Env = envs
	mounts := app.VolumeMounts[:0]
	for _, mount := range app.VolumeMounts {
		if mount.Name != current.Name+"-internal" && mount.MountPath != "/etc/tls/internal" {
			mounts = append(mounts, mount)
		}
	}
	app.VolumeMounts = mounts

	for _, desiredProxy := range desired.Spec.Template.Spec.Containers {
		if desiredProxy.Name != KubeRBACProxyName && desiredProxy.Name != "internal-tls-proxy" {
			continue
		}
		found := false
		for i := range current.Spec.Template.Spec.Containers {
			proxy := &current.Spec.Template.Spec.Containers[i]
			if proxy.Name != desiredProxy.Name {
				continue
			}
			found = true
			if proxy.Name == KubeRBACProxyName {
				// The external proxy retains its existing certificates, image, resources
				// and probes; command-line TLS/auth settings are operator owned.
				proxy.Args = append([]string(nil), desiredProxy.Args...)
			} else if !apiequality.Semantic.DeepDerivative(desiredProxy, *proxy) {
				*proxy = *desiredProxy.DeepCopy()
			}
			break
		}
		if !found {
			current.Spec.Template.Spec.Containers = append(current.Spec.Template.Spec.Containers, *desiredProxy.DeepCopy())
		}
	}
	return nil
}
