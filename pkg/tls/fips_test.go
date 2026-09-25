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
	"os"
	"path/filepath"
	"testing"
)

func TestKernelFIPSModeEnabled(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "enabled", content: "1\n", want: true},
		{name: "disabled", content: "0\n", want: false},
		{name: "unexpected value", content: "true", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fips_enabled")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := kernelFIPSModeEnabled(path); got != tt.want {
				t.Errorf("kernelFIPSModeEnabled() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("missing file is disabled", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing")
		if kernelFIPSModeEnabled(path) {
			t.Error("kernelFIPSModeEnabled() = true for a missing file, want false")
		}
	})
}
