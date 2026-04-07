/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"context"
	"errors"
	"fmt"
	"strings"

	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrMissingEvalHubLabels is returned by evalHubBaseURLFromJob when required evalhub_instance_* labels
// on the Job are absent or only one of the pair is set (malformed / incomplete).
var ErrMissingEvalHubLabels = errors.New("missing or incomplete evalhub instance labels on Job")

// evalHubBaseURLFromJob resolves the EvalHub API base URL from Job labels evalhub_instance_name and
// evalhub_instance_namespace (set by eval-hub k8s runtime). Both labels are required.
func evalHubBaseURLFromJob(ctx context.Context, c client.Client, job *batchv1.Job) (string, error) {
	var name, namespace string
	if job.Labels != nil {
		name = strings.TrimSpace(job.Labels[evalHubInstanceNameLabel])
		namespace = strings.TrimSpace(job.Labels[evalHubInstanceNamespaceLabel])
	}
	if name != "" && namespace != "" {
		return evalHubBaseURLFromCR(ctx, c, name, namespace)
	}
	if name != "" || namespace != "" {
		return "", fmt.Errorf("%w: need both %q and %q (got evalhub_instance_name=%q evalhub_instance_namespace=%q)",
			ErrMissingEvalHubLabels, evalHubInstanceNameLabel, evalHubInstanceNamespaceLabel, name, namespace)
	}
	return "", fmt.Errorf("%w: missing required evalhub instance labels %q and %q",
		ErrMissingEvalHubLabels, evalHubInstanceNameLabel, evalHubInstanceNamespaceLabel)
}

func evalHubBaseURLFromCR(ctx context.Context, c client.Client, name, namespace string) (string, error) {
	var eh evalhubv1alpha1.EvalHub
	key := client.ObjectKey{Namespace: namespace, Name: name}
	if err := c.Get(ctx, key, &eh); err != nil {
		return "", fmt.Errorf("get EvalHub %s/%s: %w", namespace, name, err)
	}
	if !eh.IsReady() {
		return "", fmt.Errorf("EvalHub %s/%s not Ready", namespace, name)
	}
	url := strings.TrimSpace(eh.Status.URL)
	if url == "" {
		return "", fmt.Errorf("EvalHub %s/%s has no URL", namespace, name)
	}
	return strings.TrimSuffix(url, "/"), nil
}
