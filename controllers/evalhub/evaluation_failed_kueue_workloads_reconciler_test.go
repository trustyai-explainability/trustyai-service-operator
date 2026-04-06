package evalhub

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
)

func TestWorkloadFirstFalseCondition(t *testing.T) {
	t.Parallel()
	wl := &kueue.Workload{
		Status: kueue.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:    kueue.WorkloadQuotaReserved,
					Status:  metav1.ConditionFalse,
					Reason:  "Inadmissible",
					Message: "ClusterQueue foo is stopped",
				},
			},
		},
	}
	c, ok := workloadFirstFalseCondition(wl)
	if !ok || c == nil {
		t.Fatalf("expected False condition")
	}
	if c.Message != "ClusterQueue foo is stopped" {
		t.Fatalf("message: got %q", c.Message)
	}

	wlOther := &kueue.Workload{
		Status: kueue.WorkloadStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Custom",
					Status: metav1.ConditionFalse,
					Reason: "NotInadmissible",
				},
			},
		},
	}
	if c2, ok2 := workloadFirstFalseCondition(wlOther); !ok2 || c2.Type != "Custom" || c2.Reason != "NotInadmissible" {
		t.Fatalf("expected match on False with any reason: ok=%v cond=%v", ok2, c2)
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
	if _, ok := workloadFirstFalseCondition(wl2); ok {
		t.Fatalf("expected no match for QuotaReserved=True")
	}
}

func TestJobOwnerFromWorkload(t *testing.T) {
	t.Parallel()
	uid := types.UID("deadbeef")
	wl := &kueue.Workload{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "v1", Kind: "Pod", Name: "ignore", UID: "1"},
				{APIVersion: batchv1.SchemeGroupVersion.String(), Kind: "Job", Name: "eval-job-1", UID: uid},
			},
		},
	}
	name, gotUID, ok := jobOwnerFromWorkload(wl)
	if !ok || name != "eval-job-1" || gotUID != uid {
		t.Fatalf("got name=%q uid=%q ok=%v", name, gotUID, ok)
	}

	wl2 := &kueue.Workload{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "batch/v1", Kind: "Job", Name: "legacy-api", UID: "2"},
			},
		},
	}
	name2, _, ok2 := jobOwnerFromWorkload(wl2)
	if !ok2 || name2 != "legacy-api" {
		t.Fatalf("batch/v1 apiVersion: got name=%q ok=%v", name2, ok2)
	}

	if _, _, ok := jobOwnerFromWorkload(&kueue.Workload{}); ok {
		t.Fatalf("expected no owner")
	}
}

func TestIsEvalHubEvaluationJob(t *testing.T) {
	t.Parallel()
	j := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				evalHubAppLabel:       evalHubAppValue,
				evalHubComponentLabel: evalHubComponentValue,
			},
		},
	}
	if !isEvalHubEvaluationJob(j) {
		t.Fatal("expected eval hub job")
	}
	j2 := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{evalHubAppLabel: evalHubAppValue}}}
	if isEvalHubEvaluationJob(j2) {
		t.Fatal("expected not eval hub job")
	}
}
