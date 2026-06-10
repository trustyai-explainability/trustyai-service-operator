package v1alpha1

import (
	"github.com/trustyai-explainability/trustyai-service-operator/api/common"
	v1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts the v1alpha1 EvalHub to v1 EvalHub (hub version)
func (src *EvalHub) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1.EvalHub)

	dst.ObjectMeta = src.ObjectMeta

	// Spec
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.Env = src.Spec.Env
	dst.Spec.Providers = src.Spec.Providers
	dst.Spec.Collections = src.Spec.Collections

	if src.Spec.Database != nil {
		dst.Spec.Database = &v1.DatabaseSpec{
			Type:         src.Spec.Database.Type,
			Secret:       src.Spec.Database.Secret,
			MaxOpenConns: src.Spec.Database.MaxOpenConns,
			MaxIdleConns: src.Spec.Database.MaxIdleConns,
		}
	}

	if src.Spec.Otel != nil {
		dst.Spec.Otel = &v1.OTELSpec{
			ExporterType:     src.Spec.Otel.ExporterType,
			ExporterEndpoint: src.Spec.Otel.ExporterEndpoint,
			ExporterInsecure: src.Spec.Otel.ExporterInsecure,
			SamplingRatio:    src.Spec.Otel.SamplingRatio,
			EnableTracing:    src.Spec.Otel.EnableTracing,
			EnableMetrics:    src.Spec.Otel.EnableMetrics,
			EnableLogs:       src.Spec.Otel.EnableLogs,
		}
	}

	if src.Spec.MCP != nil {
		dst.Spec.MCP = &v1.EvalHubMCPSpec{
			Enabled:          src.Spec.MCP.Enabled,
			Replicas:         src.Spec.MCP.Replicas,
			Transport:        src.Spec.MCP.Transport,
			EvalHubTransport: src.Spec.MCP.EvalHubTransport,
			Env:              src.Spec.MCP.Env,
			Resources:        src.Spec.MCP.Resources,
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
	dst.Status.ActiveProviders = src.Status.ActiveProviders
	dst.Status.ActiveCollections = src.Status.ActiveCollections
	dst.Status.LastUpdateTime = src.Status.LastUpdateTime.DeepCopy()

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
	dst.Spec.Env = src.Spec.Env
	dst.Spec.Providers = src.Spec.Providers
	dst.Spec.Collections = src.Spec.Collections

	if src.Spec.Database != nil {
		dst.Spec.Database = &DatabaseSpec{
			Type:         src.Spec.Database.Type,
			Secret:       src.Spec.Database.Secret,
			MaxOpenConns: src.Spec.Database.MaxOpenConns,
			MaxIdleConns: src.Spec.Database.MaxIdleConns,
		}
	}

	if src.Spec.Otel != nil {
		dst.Spec.Otel = &OTELSpec{
			ExporterType:     src.Spec.Otel.ExporterType,
			ExporterEndpoint: src.Spec.Otel.ExporterEndpoint,
			ExporterInsecure: src.Spec.Otel.ExporterInsecure,
			SamplingRatio:    src.Spec.Otel.SamplingRatio,
			EnableTracing:    src.Spec.Otel.EnableTracing,
			EnableMetrics:    src.Spec.Otel.EnableMetrics,
			EnableLogs:       src.Spec.Otel.EnableLogs,
		}
	}

	if src.Spec.MCP != nil {
		dst.Spec.MCP = &EvalHubMCPSpec{
			Enabled:          src.Spec.MCP.Enabled,
			Replicas:         src.Spec.MCP.Replicas,
			Transport:        src.Spec.MCP.Transport,
			EvalHubTransport: src.Spec.MCP.EvalHubTransport,
			Env:              src.Spec.MCP.Env,
			Resources:        src.Spec.MCP.Resources,
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
	dst.Status.ActiveProviders = src.Status.ActiveProviders
	dst.Status.ActiveCollections = src.Status.ActiveCollections
	dst.Status.LastUpdateTime = src.Status.LastUpdateTime.DeepCopy()

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
