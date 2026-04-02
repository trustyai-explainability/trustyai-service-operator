/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package evalhub

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	evalhubv1alpha1 "github.com/trustyai-explainability/trustyai-service-operator/api/evalhub/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
)

// Kueue sets WorkloadQuotaReserved to False with Reason "Inadmissible" when the workload cannot be
// admitted (see kueue pkg/workload UnsetQuotaReservationWithCondition). Kubernetes metav1 conditions
// use Status True/False/Unknown — there is no "Failed" status; "Failed" here means QuotaReserved=False.
const kueueWorkloadReasonInadmissible = "Inadmissible"

// Legacy annotation from the former kueue_inadmissible_poller; still honored so workloads are not
// reported twice after upgrade.
const annotationKueueFailedWorkloadEventReportedLegacy = "trustyai.opendatahub.io/evalhub-kueue-inadmissible-reported"

// After a successful POST to EvalHub, the Workload is annotated so we do not report the same
// failure on every poll interval.
const annotationKueueFailedWorkloadEventReported = "trustyai.opendatahub.io/evalhub-kueue-failed-workload-reported"

//+kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;patch
//+kubebuilder:rbac:groups=trustyai.opendatahub.io,resources=evalhubs,verbs=get
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get

// kueueFailedWorkloadsPoller periodically lists Kueue Workloads and notifies EvalHub when an EvalHub
// evaluation workload has QuotaReserved=False / Reason=Inadmissible. It runs as a manager Runnable
// (separate goroutine) and does not block the EvalHub reconciler or other controllers.
type kueueFailedWorkloadsPoller struct {
	Client                client.Client
	RESTConfig            *rest.Config
	OperatorNamespace     string
	OperatorConfigMapName string
}

var _ manager.Runnable = (*kueueFailedWorkloadsPoller)(nil)

func registerKueueFailedWorkloadsPoller(mgr manager.Manager, operatorNS, operatorConfigMapName string) error {
	p := &kueueFailedWorkloadsPoller{
		Client:                mgr.GetClient(),
		RESTConfig:            rest.CopyConfig(mgr.GetConfig()),
		OperatorNamespace:     operatorNS,
		OperatorConfigMapName: operatorConfigMapName,
	}
	return mgr.Add(p)
}

// Start runs until ctx is cancelled. It waits readInterval(ctx) between ticks.
func (p *kueueFailedWorkloadsPoller) Start(ctx context.Context) error {
	logger := ctrl.Log.WithName("evalhub-kueue-failed-workloads-poller")
	ctx = log.IntoContext(ctx, logger)
	logger.Info("started Kueue failed workloads poller")

	for {
		p.runTick(ctx, logger)
		interval := p.readPollInterval(ctx, logger)
		select {
		case <-ctx.Done():
			logger.Info("stopping Kueue failed workloads poller")
			return nil
		case <-time.After(interval):
		}
	}
}

func (p *kueueFailedWorkloadsPoller) readPollInterval(ctx context.Context, logger logr.Logger) time.Duration {
	if p.OperatorNamespace == "" || p.OperatorConfigMapName == "" {
		return defaultKueueFailedWorkloadsPollInterval
	}
	var cm corev1.ConfigMap
	key := types.NamespacedName{Namespace: p.OperatorNamespace, Name: p.OperatorConfigMapName}
	if err := p.Client.Get(ctx, key, &cm); err != nil {
		logger.V(1).Info("could not read operator ConfigMap for poll interval, using default",
			"configMap", key, "error", err.Error(), "defaultSeconds", int(defaultKueueFailedWorkloadsPollInterval.Seconds()))
		return defaultKueueFailedWorkloadsPollInterval
	}
	raw := strings.TrimSpace(cm.Data[configMapEvalHubKueueFailedWorkloadsPollIntervalKey])
	if raw == "" {
		raw = strings.TrimSpace(cm.Data[configMapEvalHubKueueFailedWorkloadsPollIntervalKeyLegacy])
	}
	if raw == "" {
		return defaultKueueFailedWorkloadsPollInterval
	}
	sec, err := strconv.Atoi(raw)
	if err != nil || sec < 1 {
		logger.Info("invalid eval hub Kueue failed-workloads poll interval in ConfigMap, using default",
			"keys", []string{configMapEvalHubKueueFailedWorkloadsPollIntervalKey, configMapEvalHubKueueFailedWorkloadsPollIntervalKeyLegacy},
			"value", raw,
			"defaultSeconds", int(defaultKueueFailedWorkloadsPollInterval.Seconds()))
		return defaultKueueFailedWorkloadsPollInterval
	}
	return time.Duration(sec) * time.Second
}

func workloadFailedEventAlreadyReported(wl *kueue.Workload) bool {
	if wl.Annotations == nil {
		return false
	}
	return wl.Annotations[annotationKueueFailedWorkloadEventReported] == "true" ||
		wl.Annotations[annotationKueueFailedWorkloadEventReportedLegacy] == "true"
}

func (p *kueueFailedWorkloadsPoller) runTick(ctx context.Context, logger logr.Logger) {
	list := &kueue.WorkloadList{}
	if err := p.Client.List(ctx, list); err != nil {
		logger.Error(err, "list Kueue workloads for failed-workloads sync")
		return
	}
	for i := range list.Items {
		wl := &list.Items[i]
		if workloadFailedEventAlreadyReported(wl) {
			continue
		}
		cond, ok := workloadInadmissibleQuotaCondition(wl)
		if !ok {
			continue
		}
		tplLabels := evalHubTemplateLabels(wl)
		if tplLabels == nil {
			continue
		}
		jobID := strings.TrimSpace(tplLabels[evalHubJobIDLabel])
		providerID := tplLabels[evalHubProviderIDLabel]
		benchmarkID := tplLabels[evalHubBenchmarkIDLabel]
		if jobID == "" || providerID == "" || benchmarkID == "" {
			logger.V(1).Info("skip failed-workload sync: missing eval job labels on pod template",
				"workload", client.ObjectKeyFromObject(wl))
			continue
		}
		benchmarkIndex := benchmarkIndexFromTemplateLabels(tplLabels)

		baseURL, err := p.evalHubBaseURLFromTemplateLabels(ctx, tplLabels)
		if err != nil {
			logger.V(1).Info("skip failed-workload sync: cannot resolve EvalHub URL",
				"workload", client.ObjectKeyFromObject(wl), "error", err.Error())
			continue
		}

		msg := strings.TrimSpace(cond.Message)
		if msg == "" {
			msg = fmt.Sprintf("Kueue workload inadmissible (reason=%s)", cond.Reason)
		}

		if err := postEvalHubBenchmarkFailed(ctx, p.RESTConfig, baseURL, wl.Namespace, jobID, providerID, benchmarkID, benchmarkIndex, msg, messageCodeKueueInadmissible); err != nil {
			logger.Error(err, "post EvalHub events for Kueue failed evaluation workload",
				"workload", client.ObjectKeyFromObject(wl), "evalJobID", jobID)
			continue
		}

		if err := p.annotateWorkloadReported(ctx, wl); err != nil {
			logger.Error(err, "patch workload after EvalHub failed-workload event (will retry next poll)",
				"workload", client.ObjectKeyFromObject(wl))
			continue
		}

		logger.Info("posted EvalHub failed status for Kueue failed evaluation workload",
			"evalhub_watcher", "kueue_failed_workloads",
			"workload", wl.Name, "workloadNamespace", wl.Namespace,
			"evalJobID", jobID, "providerID", providerID, "benchmarkID", benchmarkID)
	}
}

func workloadInadmissibleQuotaCondition(wl *kueue.Workload) (*metav1.Condition, bool) {
	c := apimeta.FindStatusCondition(wl.Status.Conditions, kueue.WorkloadQuotaReserved)
	if c == nil {
		return nil, false
	}
	if c.Status != metav1.ConditionFalse || c.Reason != kueueWorkloadReasonInadmissible {
		return nil, false
	}
	return c, true
}

func evalHubTemplateLabels(wl *kueue.Workload) map[string]string {
	for i := range wl.Spec.PodSets {
		lbl := wl.Spec.PodSets[i].Template.Labels
		if lbl == nil {
			continue
		}
		if lbl[evalHubAppLabel] == evalHubAppValue && lbl[evalHubComponentLabel] == evalHubComponentValue {
			return lbl
		}
	}
	return nil
}

func benchmarkIndexFromTemplateLabels(labels map[string]string) int {
	raw := strings.TrimSpace(labels[evalHubBenchmarkIndexLabel])
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (p *kueueFailedWorkloadsPoller) evalHubBaseURLFromTemplateLabels(ctx context.Context, labels map[string]string) (string, error) {
	name := strings.TrimSpace(labels[evalHubInstanceNameLabel])
	namespace := strings.TrimSpace(labels[evalHubInstanceNamespaceLabel])
	if name == "" || namespace == "" {
		return "", fmt.Errorf("missing %s or %s on workload pod template",
			evalHubInstanceNameLabel, evalHubInstanceNamespaceLabel)
	}
	var eh evalhubv1alpha1.EvalHub
	if err := p.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &eh); err != nil {
		return "", fmt.Errorf("get EvalHub %s/%s: %w", namespace, name, err)
	}
	if !eh.IsReady() {
		return "", fmt.Errorf("EvalHub %s/%s not Ready", namespace, name)
	}
	url := strings.TrimSpace(eh.Status.URL)
	if url == "" {
		return "", fmt.Errorf("EvalHub %s/%s has no status URL", namespace, name)
	}
	return strings.TrimSuffix(url, "/"), nil
}

func (p *kueueFailedWorkloadsPoller) annotateWorkloadReported(ctx context.Context, wl *kueue.Workload) error {
	patchBase := wl.DeepCopy()
	if wl.Annotations == nil {
		wl.Annotations = map[string]string{}
	}
	wl.Annotations[annotationKueueFailedWorkloadEventReported] = "true"
	return p.Client.Patch(ctx, wl, client.MergeFrom(patchBase))
}
