/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster_guardrail_policy

import (
	"context"
	"encoding/json"
	"fmt"

	clusterguardrailpolicyv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/cluster_guardrail_policy/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ippPluginConfig struct {
	NemoURL        string `json:"nemoURL"`
	Endpoint       string `json:"endpoint"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Enabled        bool   `json:"enabled"`
}

// reconcileIPPConfig creates or updates ConfigMaps for the IPP ext_proc plugins.
func (r *ClusterGuardrailPolicyReconciler) reconcileIPPConfig(ctx context.Context, instance *clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy) error {
	logger := log.FromContext(ctx)
	guardrails := instance.Spec.Guardrails
	nemoName := fmt.Sprintf("%s-nemo", instance.Name)
	nemoURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:8000", nemoName, instance.Spec.TargetNamespace)
	var refs []clusterguardrailpolicyv1alpha1.ManagedResourceRef

	if guardrails.InputGuard != nil {
		cmName := fmt.Sprintf("%s-request-guard-config", instance.Name)
		enabled := guardrails.InputGuard.Enabled == nil || *guardrails.InputGuard.Enabled
		timeout := 360
		if guardrails.InputGuard.TimeoutSeconds != nil {
			timeout = *guardrails.InputGuard.TimeoutSeconds
		}
		if guardrails.InputGuard.NemoURL != "" {
			nemoURL = guardrails.InputGuard.NemoURL
		}
		cfg := ippPluginConfig{
			NemoURL:        nemoURL,
			Endpoint:       "/v1/guardrail/checks",
			TimeoutSeconds: timeout,
			Enabled:        enabled,
		}
		if err := r.reconcileIPPConfigMap(ctx, instance, cmName, cfg); err != nil {
			return err
		}
		logger.Info("Reconciled IPP request guard ConfigMap", "name", cmName)
		refs = append(refs, clusterguardrailpolicyv1alpha1.ManagedResourceRef{
			Name:      cmName,
			Namespace: instance.Spec.TargetNamespace,
			Ready:     true,
		})
	}

	if guardrails.OutputGuard != nil {
		cmName := fmt.Sprintf("%s-response-guard-config", instance.Name)
		outputNemoURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:8000", nemoName, instance.Spec.TargetNamespace)
		enabled := guardrails.OutputGuard.Enabled == nil || *guardrails.OutputGuard.Enabled
		timeout := 360
		if guardrails.OutputGuard.TimeoutSeconds != nil {
			timeout = *guardrails.OutputGuard.TimeoutSeconds
		}
		if guardrails.OutputGuard.NemoURL != "" {
			outputNemoURL = guardrails.OutputGuard.NemoURL
		}
		cfg := ippPluginConfig{
			NemoURL:        outputNemoURL,
			Endpoint:       "/v1/guardrail/checks",
			TimeoutSeconds: timeout,
			Enabled:        enabled,
		}
		if err := r.reconcileIPPConfigMap(ctx, instance, cmName, cfg); err != nil {
			return err
		}
		logger.Info("Reconciled IPP response guard ConfigMap", "name", cmName)
		refs = append(refs, clusterguardrailpolicyv1alpha1.ManagedResourceRef{
			Name:      cmName,
			Namespace: instance.Spec.TargetNamespace,
			Ready:     true,
		})
	}

	instance.Status.IPPConfigRefs = refs
	return nil
}

// reconcileIPPConfigMap creates or updates a single IPP plugin ConfigMap.
func (r *ClusterGuardrailPolicyReconciler) reconcileIPPConfigMap(ctx context.Context, instance *clusterguardrailpolicyv1alpha1.ClusterGuardrailPolicy, cmName string, cfg ippPluginConfig) error {
	namespace := instance.Spec.TargetNamespace

	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal IPP config: %w", err)
	}

	existing := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: namespace}, existing)

	if err != nil && errors.IsNotFound(err) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: namespace,
				Labels: map[string]string{
					managedByLabel: instance.Name,
				},
				Annotations: map[string]string{
					managedByAnnotation: instance.Name,
				},
			},
			Data: map[string]string{
				"config.json": string(configJSON),
			},
		}
		return r.Create(ctx, cm)
	} else if err != nil {
		return err
	}

	existing.Data = map[string]string{
		"config.json": string(configJSON),
	}
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels[managedByLabel] = instance.Name
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations[managedByAnnotation] = instance.Name
	return r.Update(ctx, existing)
}
