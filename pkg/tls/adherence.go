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
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

type profileState struct {
	profile   *configv1.TLSSecurityProfile
	adherence string
}

// readTLSProfileState reads both fields from one APIServer response. The
// dynamic client preserves tlsAdherence on clusters whose generated OpenShift
// API type predates that field; older clusters simply use NoOpinion.
func readTLSProfileState(ctx context.Context, c dynamic.Interface) (profileState, error) {
	apiServer, err := c.Resource(configv1.GroupVersion.WithResource("apiservers")).Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return profileState{}, err
	}

	state := profileState{adherence: TLSAdherenceNoOpinion}
	profile, found, err := unstructured.NestedMap(apiServer.Object, "spec", "tlsSecurityProfile")
	if err != nil {
		return profileState{}, fmt.Errorf("reading APIServer spec.tlsSecurityProfile: %w", err)
	}
	if found {
		state.profile = &configv1.TLSSecurityProfile{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(profile, state.profile); err != nil {
			return profileState{}, fmt.Errorf("decoding APIServer spec.tlsSecurityProfile: %w", err)
		}
	}

	state.adherence, found, err = unstructured.NestedString(apiServer.Object, "spec", "tlsAdherence")
	if err != nil {
		return profileState{}, fmt.Errorf("reading APIServer spec.tlsAdherence: %w", err)
	}
	if !found {
		state.adherence = TLSAdherenceNoOpinion
	}
	return state, nil
}
