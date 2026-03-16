package coord

import (
	"testing"
)

func TestCreateAndListPlans(t *testing.T) {
	c := newTestCoordinator(t)

	plan1 := &Plan{Title: "Plan Alpha", Description: "First plan"}
	if err := c.CreatePlan(plan1); err != nil {
		t.Fatalf("CreatePlan 1: %v", err)
	}
	if plan1.ID == "" {
		t.Fatal("expected non-empty plan ID")
	}

	plan2 := &Plan{Title: "Plan Beta", Description: "Second plan"}
	if err := c.CreatePlan(plan2); err != nil {
		t.Fatalf("CreatePlan 2: %v", err)
	}

	plans, err := c.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}
}

func TestGetPlan(t *testing.T) {
	c := newTestCoordinator(t)

	plan := &Plan{
		Title:       "Detailed Plan",
		Description: "A plan with steps",
		Steps: []PlanStep{
			{Description: "Step one"},
			{Description: "Step two"},
			{Description: "Step three"},
		},
	}
	if err := c.CreatePlan(plan); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	got, err := c.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}

	if got.Title != "Detailed Plan" {
		t.Errorf("title = %q, want Detailed Plan", got.Title)
	}
	if len(got.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(got.Steps))
	}
	if got.Steps[0].Status != "pending" {
		t.Errorf("step 0 status = %q, want pending", got.Steps[0].Status)
	}
	if got.Steps[0].Order != 1 {
		t.Errorf("step 0 order = %d, want 1", got.Steps[0].Order)
	}
}

func TestGetPlan_NotFound(t *testing.T) {
	c := newTestCoordinator(t)

	_, err := c.GetPlan("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent plan")
	}
}

func TestPlanUpdateAndDelete(t *testing.T) {
	c := newTestCoordinator(t)

	plan := &Plan{Title: "Mutable Plan"}
	if err := c.CreatePlan(plan); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	plan.Status = "active"
	if err := c.UpdatePlan(plan); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}

	got, _ := c.GetPlan(plan.ID)
	if got.Status != "active" {
		t.Errorf("status = %q, want active", got.Status)
	}

	if err := c.DeletePlan(plan.ID); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	plans, _ := c.ListPlans()
	if len(plans) != 0 {
		t.Fatalf("expected 0 plans after delete, got %d", len(plans))
	}
}
