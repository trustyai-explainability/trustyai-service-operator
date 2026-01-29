package lmes

import (
	lmesv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/lmes/v1alpha1"
)

// ProgressTriggeredChange checks if progress bars have changed
func ProgressTriggeredChange(a, b *lmesv1alpha1.ProgressBar) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Percent == b.Percent && a.Message == b.Message
}

// ProgressArrayTriggeredChange checks if progress bar arrays have changed
func ProgressArrayTriggeredChange(a, b []lmesv1alpha1.ProgressBar) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !ProgressTriggeredChange(&a[i], &b[i]) {
			return false
		}
	}
	return true
}
