/*
Copyright 2026 The TrustyAI Authors.

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

package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

func TestResourceClaimsRoundTrip(t *testing.T) {
	job := &LMEvalJob{Spec: LMEvalJobSpec{Pod: &LMEvalPodSpec{
		ResourceClaims: []corev1.PodResourceClaim{
			{Name: "gpu", ResourceClaimTemplateName: ptr.To("gpu-template")},
			{Name: "existing", ResourceClaimName: ptr.To("existing-claim")},
		},
		Container: &LMEvalContainer{Resources: &corev1.ResourceRequirements{
			Claims: []corev1.ResourceClaim{{Name: "gpu", Request: "device"}},
		}},
		SideCars: []corev1.Container{{Name: "model", Resources: corev1.ResourceRequirements{
			Claims: []corev1.ResourceClaim{{Name: "existing"}},
		}}},
	}}}
	for _, codec := range []struct {
		name      string
		marshal   func(interface{}) ([]byte, error)
		unmarshal func([]byte, interface{}) error
	}{
		{"JSON", json.Marshal, json.Unmarshal},
		{"YAML", yaml.Marshal, func(data []byte, value interface{}) error { return yaml.Unmarshal(data, value) }},
	} {
		t.Run(codec.name, func(t *testing.T) {
			data, err := codec.marshal(job)
			require.NoError(t, err)
			var decoded LMEvalJob
			require.NoError(t, codec.unmarshal(data, &decoded))
			require.Equal(t, job.Spec.Pod, decoded.Spec.Pod)
		})
	}

	copy := job.DeepCopy()
	copy.Spec.Pod.ResourceClaims[0].Name = "changed"
	*copy.Spec.Pod.ResourceClaims[0].ResourceClaimTemplateName = "changed-template"
	*copy.Spec.Pod.ResourceClaims[1].ResourceClaimName = "changed-claim"
	copy.Spec.Pod.Container.Resources.Claims[0].Request = "changed-request"
	copy.Spec.Pod.SideCars[0].Resources.Claims[0].Name = "changed-sidecar"
	require.Equal(t, "gpu", job.Spec.Pod.ResourceClaims[0].Name)
	require.Equal(t, "gpu-template", *job.Spec.Pod.ResourceClaims[0].ResourceClaimTemplateName)
	require.Equal(t, "existing-claim", *job.Spec.Pod.ResourceClaims[1].ResourceClaimName)
	require.Equal(t, "device", job.Spec.Pod.Container.Resources.Claims[0].Request)
	require.Equal(t, "existing", job.Spec.Pod.SideCars[0].Resources.Claims[0].Name)
}

func TestResourceClaimsOptional(t *testing.T) {
	var pod *LMEvalPodSpec
	require.Nil(t, pod.GetResourceClaims())
	for _, pod := range []*LMEvalPodSpec{nil, {}, {ResourceClaims: []corev1.PodResourceClaim{}}} {
		job := &LMEvalJob{Spec: LMEvalJobSpec{Pod: pod}}
		data, err := json.Marshal(job)
		require.NoError(t, err)
		require.NotContains(t, string(data), "resourceClaims")
	}
}
