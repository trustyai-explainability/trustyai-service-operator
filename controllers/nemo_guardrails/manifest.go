/*
Copyright 2023.

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

package nemo_guardrails

import (
	"context"

	nemoguardrailsv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/nemo_guardrails/v1alpha1"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/utils"
)

// buildManifestURL returns the in-cluster URL of the NeMo-Guardrails fork's
// capability manifest endpoint for the given CR, using the same scheme and
// default port as the reconciled Service (see service.tmpl.yaml).
func buildManifestURL(nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails) string {
	base := utils.GenerateNonTLSServiceURL(nemoGuardrails.Name, nemoGuardrails.Namespace)
	if utils.RequiresAuth(nemoGuardrails) {
		base = utils.GenerateTLSServiceURL(nemoGuardrails.Name, nemoGuardrails.Namespace)
	}
	return base + manifestPath
}

// ensureManifestAnnotation sets the manifest discoverability annotation on
// the CR if it's missing or stale, so agents in the cluster can discover the
// manifest endpoint from the CR rather than relying on hardcoded
// configuration. This is best-effort metadata only: it does not require the
// manifest endpoint to actually be served by the deployed image, so it stays
// backwards-compatible with older NeMo-Guardrails deployments.
func (r *NemoGuardrailsReconciler) ensureManifestAnnotation(ctx context.Context, nemoGuardrails *nemoguardrailsv1alpha1.NemoGuardrails) error {
	url := buildManifestURL(nemoGuardrails)
	if nemoGuardrails.Annotations[manifestAnnotationKey] == url {
		return nil
	}

	if nemoGuardrails.Annotations == nil {
		nemoGuardrails.Annotations = map[string]string{}
	}
	nemoGuardrails.Annotations[manifestAnnotationKey] = url
	return r.Update(ctx, nemoGuardrails)
}
