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

package lmes

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// restoreOptions snapshots the package-level Options and restores it when the
// test finishes, so configuration tests cannot leak into the controller suite.
func restoreOptions(t *testing.T) {
	t.Helper()
	original := *Options
	t.Cleanup(func() { *Options = original })
}

func Test_pollResult_alwaysSchedulesAPositiveRequeue(t *testing.T) {
	restoreOptions(t)

	tests := []struct {
		name     string
		interval time.Duration
		expected time.Duration
	}{
		{name: "configured interval is preserved", interval: 3 * time.Second, expected: 3 * time.Second},
		{name: "default interval is preserved", interval: DefaultPodCheckingInterval, expected: DefaultPodCheckingInterval},
		{name: "zero interval falls back to the default", interval: 0, expected: DefaultPodCheckingInterval},
		{name: "negative interval falls back to the default", interval: -5 * time.Second, expected: DefaultPodCheckingInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Options.PodCheckingInterval = tt.interval
			result := pollResult()
			assert.Greater(t, result.RequeueAfter, time.Duration(0),
				"a poll must always requeue, otherwise a running job is never polled again")
			assert.Equal(t, tt.expected, result.RequeueAfter)
		})
	}
}

func Test_constructOptionsFromConfigMap_intervalValidation(t *testing.T) {
	restoreOptions(t)

	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{name: "positive interval is applied", value: "7s", expected: 7 * time.Second},
		{name: "zero interval keeps the default", value: "0s", expected: DefaultPodCheckingInterval},
		{name: "negative interval keeps the default", value: "-5s", expected: DefaultPodCheckingInterval},
		{name: "unparseable interval keeps the default", value: "soon", expected: DefaultPodCheckingInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Options.PodCheckingInterval = DefaultPodCheckingInterval
			log := logr.Discard()
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "lmes-config", Namespace: "system"},
				Data:       map[string]string{PodCheckingIntervalKey: tt.value},
			}
			assert.NoError(t, constructOptionsFromConfigMap(&log, cm))
			assert.Equal(t, tt.expected, Options.PodCheckingInterval)
		})
	}
}
