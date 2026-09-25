/*
Copyright 2026.

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

package tls

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestReadTLSProfileState(t *testing.T) {
	tests := []struct {
		name        string
		adherence   string
		profileType string
		want        string
	}{
		{name: "strict and profile are read from APIServer spec", adherence: TLSAdherenceStrictAllComponents, profileType: string(configv1.TLSProfileModernType), want: TLSAdherenceStrictAllComponents},
		{name: "legacy is read from APIServer spec", adherence: TLSAdherenceLegacyAdheringComponentsOnly, want: TLSAdherenceLegacyAdheringComponentsOnly},
		{name: "missing field means no opinion", want: TLSAdherenceNoOpinion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiServer := &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "config.openshift.io/v1",
				"kind":       "APIServer",
				"metadata":   map[string]interface{}{"name": "cluster"},
				"spec":       map[string]interface{}{},
			}}
			apiServer.SetGroupVersionKind(configv1.GroupVersion.WithKind("APIServer"))
			spec := apiServer.Object["spec"].(map[string]interface{})
			if tt.adherence != "" {
				spec["tlsAdherence"] = tt.adherence
			}
			if tt.profileType != "" {
				spec["tlsSecurityProfile"] = map[string]interface{}{"type": tt.profileType}
			}

			scheme := runtime.NewScheme()
			c := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
				configv1.GroupVersion.WithResource("apiservers"): "APIServerList",
			}, apiServer)
			got, err := readTLSProfileState(context.Background(), c)
			if err != nil {
				t.Fatalf("readTLSProfileState() error = %v", err)
			}
			if got.adherence != tt.want {
				t.Errorf("readTLSProfileState() adherence = %q, want %q", got.adherence, tt.want)
			}
			if tt.profileType != "" && (got.profile == nil || string(got.profile.Type) != tt.profileType) {
				t.Errorf("readTLSProfileState() profile = %#v, want type %q", got.profile, tt.profileType)
			}
		})
	}
}
