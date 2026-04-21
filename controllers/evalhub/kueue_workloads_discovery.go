/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
)

const kueueWorkloadResourceName = "workloads"

// clusterSupportsKueueWorkloads returns true if the apiserver serves the Kueue Workload resource
// (kueue.x-k8s.io/v1beta1, resource workloads). Discovery or config errors are treated as “not available”
// so EvalHub can still start when Kueue is absent or discovery is temporarily unavailable.
func clusterSupportsKueueWorkloads(cfg *rest.Config) bool {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return false
	}
	rl, err := dc.ServerResourcesForGroupVersion(kueue.GroupVersion.String())
	if err != nil {
		return false
	}
	for _, r := range rl.APIResources {
		if r.Name == kueueWorkloadResourceName {
			return true
		}
	}
	return false
}
