// Package v1alpha1 tests use stdlib testing, not Ginkgo/Gomega, because
// odh-platform-utilities' validation.ValidatePlatformObject takes a concrete
// *testing.T (Ginkgo's GinkgoT() returns an incompatible interface type).
package v1alpha1

import (
	"testing"

	common "github.com/opendatahub-io/odh-platform-utilities/api/common"
	"github.com/opendatahub-io/odh-platform-utilities/api/common/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTrustyAIPlatformObjectConformance(t *testing.T) {
	obj := &TrustyAI{
		ObjectMeta: metav1.ObjectMeta{
			Name:       TrustyAIInstanceName,
			Generation: 1,
		},
		Spec: TrustyAISpec{
			ManagementSpec: common.ManagementSpec{
				ManagementState: common.Managed,
			},
		},
	}

	validation.ValidatePlatformObject(t, obj)
}

func TestGetStatus(t *testing.T) {
	obj := &TrustyAI{}
	status := obj.GetStatus()
	if status == nil {
		t.Fatal("GetStatus() returned nil")
	}

	status.Phase = common.PhaseReady
	if obj.Status.Phase != common.PhaseReady {
		t.Errorf("GetStatus() did not return a pointer to the embedded status; phase = %q, want %q", obj.Status.Phase, common.PhaseReady)
	}
}

func TestConditionsRoundTrip(t *testing.T) {
	obj := &TrustyAI{}

	conds := []common.Condition{
		{
			Type:               string(common.ConditionTypeReady),
			Status:             metav1.ConditionTrue,
			Reason:             "AllHealthy",
			Message:            "All services healthy",
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: 1,
		},
		{
			Type:               string(common.ConditionTypeProvisioningSucceeded),
			Status:             metav1.ConditionTrue,
			Reason:             "Provisioned",
			Message:            "All resources applied",
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: 1,
		},
	}

	obj.SetConditions(conds)
	got := obj.GetConditions()

	if len(got) != len(conds) {
		t.Fatalf("GetConditions() returned %d conditions, want %d", len(got), len(conds))
	}
	for i, c := range got {
		if c.Type != conds[i].Type {
			t.Errorf("condition[%d].Type = %q, want %q", i, c.Type, conds[i].Type)
		}
		if c.Status != conds[i].Status {
			t.Errorf("condition[%d].Status = %q, want %q", i, c.Status, conds[i].Status)
		}
		if c.Reason != conds[i].Reason {
			t.Errorf("condition[%d].Reason = %q, want %q", i, c.Reason, conds[i].Reason)
		}
		if c.Message != conds[i].Message {
			t.Errorf("condition[%d].Message = %q, want %q", i, c.Message, conds[i].Message)
		}
		if !c.LastTransitionTime.Equal(&conds[i].LastTransitionTime) {
			t.Errorf("condition[%d].LastTransitionTime = %v, want %v", i, c.LastTransitionTime, conds[i].LastTransitionTime)
		}
		if c.ObservedGeneration != conds[i].ObservedGeneration {
			t.Errorf("condition[%d].ObservedGeneration = %d, want %d", i, c.ObservedGeneration, conds[i].ObservedGeneration)
		}
	}
}

func TestReleaseStatusRoundTrip(t *testing.T) {
	obj := &TrustyAI{}

	release := common.ComponentReleaseStatus{
		Releases: []common.ComponentRelease{
			{Name: "trustyai-operator-module", Version: "1.0.0"},
		},
	}

	obj.SetReleaseStatus(release)
	got := obj.GetReleaseStatus()

	if got == nil {
		t.Fatal("GetReleaseStatus() returned nil")
	}
	if len(got.Releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(got.Releases))
	}
	if got.Releases[0].Name != "trustyai-operator-module" {
		t.Errorf("release name = %q, want %q", got.Releases[0].Name, "trustyai-operator-module")
	}
	if got.Releases[0].Version != "1.0.0" {
		t.Errorf("release version = %q, want %q", got.Releases[0].Version, "1.0.0")
	}
}

func TestSingletonInstanceName(t *testing.T) {
	if TrustyAIInstanceName != "default-trustyai" {
		t.Errorf("TrustyAIInstanceName = %q, want %q", TrustyAIInstanceName, "default-trustyai")
	}
}
