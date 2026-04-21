/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// evalHubTenantNamespaces tracks namespace names that carry evalhub.trustyai.opendatahub.io/tenant.
// It is shared by EvalHubEvaluationJobFailureReconciler (Namespace watch) and
// EvalHubEvaluationFailedKueueWorkloadsReconciler (predicates / Reconcile).
type evalHubTenantNamespaces struct {
	names sync.Map // string -> struct{}
}

func newEvalHubTenantNamespaces() *evalHubTenantNamespaces {
	return &evalHubTenantNamespaces{}
}

func (t *evalHubTenantNamespaces) Add(name string) {
	t.names.Store(name, struct{}{})
}

func (t *evalHubTenantNamespaces) Remove(name string) {
	t.names.Delete(name)
}

func (t *evalHubTenantNamespaces) IsTenant(name string) bool {
	_, ok := t.names.Load(name)
	return ok
}

// bootstrap loads tenant namespaces from the API before the manager cache starts.
func (t *evalHubTenantNamespaces) bootstrap(ctx context.Context, apiReader client.Reader) error {
	nsList := &corev1.NamespaceList{}
	if err := apiReader.List(ctx, nsList, client.HasLabels{tenantLabel}); err != nil {
		return err
	}
	var toDelete []string
	t.names.Range(func(key, _ interface{}) bool {
		toDelete = append(toDelete, key.(string))
		return true
	})
	for _, k := range toDelete {
		t.names.Delete(k)
	}
	for i := range nsList.Items {
		t.names.Store(nsList.Items[i].Name, struct{}{})
	}
	return nil
}
