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
	"crypto/fips140"
	"os"
	"strings"
)

const kernelFIPSModePath = "/proc/sys/crypto/fips_enabled"

// fipsModeEnabled reports FIPS mode when either Go's crypto runtime or the
// host kernel reports it. The kernel check is needed for operator builds whose
// Go runtime is not configured to expose the host's FIPS mode itself.
func fipsModeEnabled() bool {
	return fips140.Enabled() || kernelFIPSModeEnabled(kernelFIPSModePath)
}

func kernelFIPSModeEnabled(path string) bool {
	contents, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(contents)) == "1"
}
