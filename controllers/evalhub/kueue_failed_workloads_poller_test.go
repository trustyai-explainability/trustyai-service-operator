package evalhub

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
)

func TestWorkloadInadmissibleQuotaCondition(t *testing.T) {
	t.Parallel()
	wl := &kueue.Workload{
		Status: kueue.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:    kueue.WorkloadQuotaReserved,
					Status:  metav1.ConditionFalse,
					Reason:  kueueWorkloadReasonInadmissible,
					Message: "ClusterQueue foo is stopped",
				},
			},
		},
	}
	c, ok := workloadInadmissibleQuotaCondition(wl)
	if !ok || c == nil {
		t.Fatalf("expected QuotaReserved=False Inadmissible condition")
	}
	if c.Message != "ClusterQueue foo is stopped" {
		t.Fatalf("message: got %q", c.Message)
	}

	wl2 := &kueue.Workload{
		Status: kueue.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:   kueue.WorkloadQuotaReserved,
					Status: metav1.ConditionTrue,
					Reason: "QuotaReserved",
				},
			},
		},
	}
	if _, ok := workloadInadmissibleQuotaCondition(wl2); ok {
		t.Fatalf("expected no match for QuotaReserved=True")
	}
}

func TestEvalHubTemplateLabels(t *testing.T) {
	t.Parallel()
	wl := &kueue.Workload{
		Spec: kueue.WorkloadSpec{
			PodSets: []kueue.PodSet{
				{
					Name: "main",
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								evalHubAppLabel:       evalHubAppValue,
								evalHubComponentLabel: evalHubComponentValue,
								evalHubJobIDLabel:     "job-1",
							},
						},
					},
					Count: 1,
				},
			},
		},
	}
	lbl := evalHubTemplateLabels(wl)
	if lbl == nil || lbl[evalHubJobIDLabel] != "job-1" {
		t.Fatalf("labels: %#v", lbl)
	}
}
