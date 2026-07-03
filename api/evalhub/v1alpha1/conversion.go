package v1alpha1

import (
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	v1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts the v1alpha1 EvalHub to v1 EvalHub (hub version)
func (src *EvalHub) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1.EvalHub)

	dst.ObjectMeta = src.ObjectMeta

	// Spec
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.Env = deepCopyEnvVars(src.Spec.Env)
	dst.Spec.Providers = copyStrings(src.Spec.Providers)
	dst.Spec.Collections = copyStrings(src.Spec.Collections)

	if src.Spec.Database != nil {
		dst.Spec.Database = &v1.DatabaseSpec{
			Type:         src.Spec.Database.Type,
			Secret:       src.Spec.Database.Secret,
			MaxOpenConns: src.Spec.Database.MaxOpenConns,
			MaxIdleConns: src.Spec.Database.MaxIdleConns,
		}
	}

	if src.Spec.Otel != nil {
		if dst.Spec.Otel == nil {
			dst.Spec.Otel = &v1.OTELSpec{}
		}
		mergeOTELSpecToV1(src.Spec.Otel, dst.Spec.Otel)
	} else {
		dst.Spec.Otel = nil
	}

	if src.Spec.MCP != nil {
		dst.Spec.MCP = &v1.EvalHubMCPSpec{
			Enabled:          copyBoolPtr(src.Spec.MCP.Enabled),
			Replicas:         copyInt32Ptr(src.Spec.MCP.Replicas),
			Transport:        src.Spec.MCP.Transport,
			EvalHubTransport: src.Spec.MCP.EvalHubTransport,
			Env:              deepCopyEnvVars(src.Spec.MCP.Env),
			Resources:        *src.Spec.MCP.Resources.DeepCopy(),
			Image:            src.Spec.MCP.Image,
			AuthSecret:       src.Spec.MCP.AuthSecret,
		}
	}

	// Status
	dst.Status.Phase = src.Status.Phase
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.ReadyReplicas = src.Status.ReadyReplicas
	dst.Status.Ready = src.Status.Ready
	dst.Status.URL = src.Status.URL
	dst.Status.ActiveProviders = copyStrings(src.Status.ActiveProviders)
	dst.Status.ActiveCollections = copyStrings(src.Status.ActiveCollections)
	if src.Status.LastUpdateTime != nil {
		dst.Status.LastUpdateTime = src.Status.LastUpdateTime.DeepCopy()
	}

	dst.Status.Conditions = make([]common.Condition, len(src.Status.Conditions))
	for i, c := range src.Status.Conditions {
		dst.Status.Conditions[i] = common.Condition{
			Type:               c.Type,
			Status:             c.Status,
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime,
		}
	}

	if src.Status.MCP != nil {
		dst.Status.MCP = &v1.EvalHubMCPStatus{
			Phase: src.Status.MCP.Phase,
			Ready: src.Status.MCP.Ready,
			URL:   src.Status.MCP.URL,
		}
		dst.Status.MCP.Conditions = make([]metav1.Condition, len(src.Status.MCP.Conditions))
		copy(dst.Status.MCP.Conditions, src.Status.MCP.Conditions)
	}

	return nil
}

// ConvertFrom converts the v1 EvalHub (hub version) to v1alpha1 EvalHub
func (dst *EvalHub) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1.EvalHub)

	dst.ObjectMeta = src.ObjectMeta

	// Spec
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.Env = deepCopyEnvVars(src.Spec.Env)
	dst.Spec.Providers = copyStrings(src.Spec.Providers)
	dst.Spec.Collections = copyStrings(src.Spec.Collections)

	if src.Spec.Database != nil {
		dst.Spec.Database = &DatabaseSpec{
			Type:         src.Spec.Database.Type,
			Secret:       src.Spec.Database.Secret,
			MaxOpenConns: src.Spec.Database.MaxOpenConns,
			MaxIdleConns: src.Spec.Database.MaxIdleConns,
		}
	}

	if src.Spec.Otel != nil {
		dst.Spec.Otel = copyOTELSpecFromV1(src.Spec.Otel)
	}

	if src.Spec.MCP != nil {
		dst.Spec.MCP = &EvalHubMCPSpec{
			Enabled:          copyBoolPtr(src.Spec.MCP.Enabled),
			Replicas:         copyInt32Ptr(src.Spec.MCP.Replicas),
			Transport:        src.Spec.MCP.Transport,
			EvalHubTransport: src.Spec.MCP.EvalHubTransport,
			Env:              deepCopyEnvVars(src.Spec.MCP.Env),
			Resources:        *src.Spec.MCP.Resources.DeepCopy(),
			Image:            src.Spec.MCP.Image,
			AuthSecret:       src.Spec.MCP.AuthSecret,
		}
	}

	// Status
	dst.Status.Phase = src.Status.Phase
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.ReadyReplicas = src.Status.ReadyReplicas
	dst.Status.Ready = src.Status.Ready
	dst.Status.URL = src.Status.URL
	dst.Status.ActiveProviders = copyStrings(src.Status.ActiveProviders)
	dst.Status.ActiveCollections = copyStrings(src.Status.ActiveCollections)
	if src.Status.LastUpdateTime != nil {
		dst.Status.LastUpdateTime = src.Status.LastUpdateTime.DeepCopy()
	}

	dst.Status.Conditions = make([]common.Condition, len(src.Status.Conditions))
	for i, c := range src.Status.Conditions {
		dst.Status.Conditions[i] = common.Condition{
			Type:               c.Type,
			Status:             c.Status,
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime,
		}
	}

	if src.Status.MCP != nil {
		dst.Status.MCP = &EvalHubMCPStatus{
			Phase: src.Status.MCP.Phase,
			Ready: src.Status.MCP.Ready,
			URL:   src.Status.MCP.URL,
		}
		dst.Status.MCP.Conditions = make([]metav1.Condition, len(src.Status.MCP.Conditions))
		copy(dst.Status.MCP.Conditions, src.Status.MCP.Conditions)
	}

	return nil
}

func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

func deepCopyEnvVars(envs []corev1.EnvVar) []corev1.EnvVar {
	if envs == nil {
		return nil
	}
	out := make([]corev1.EnvVar, len(envs))
	for i := range envs {
		out[i] = *envs[i].DeepCopy()
	}
	return out
}

func copyBoolPtr(p *bool) *bool {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyInt32Ptr(p *int32) *int32 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func mergeOTELSpecToV1(src *OTELSpec, dst *v1.OTELSpec) {
	dst.ExporterType = src.ExporterType
	dst.ExporterEndpoint = src.ExporterEndpoint
	dst.ExporterInsecure = src.ExporterInsecure
	dst.SamplingRatio = src.SamplingRatio
	dst.EnableTracing = src.EnableTracing
	dst.EnableMetrics = src.EnableMetrics
	dst.EnableLogs = src.EnableLogs
}

func copyOTELSpecFromV1(src *v1.OTELSpec) *OTELSpec {
	if src == nil {
		return nil
	}
	dst := &OTELSpec{
		ExporterType:     src.ExporterType,
		ExporterEndpoint: src.ExporterEndpoint,
		ExporterInsecure: src.ExporterInsecure,
		SamplingRatio:    src.SamplingRatio,
		EnableTracing:    src.EnableTracing,
		EnableMetrics:    src.EnableMetrics,
		EnableLogs:       src.EnableLogs,
	}
	return dst
}
