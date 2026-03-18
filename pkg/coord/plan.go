package coord

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// Plan represents a coordination plan stored in refs/coord/plans/.
type Plan struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"` // draft, active, completed, archived
	Author      string     `json:"author,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Steps       []PlanStep `json:"steps,omitempty"`
}

// PlanStep is a single actionable item within a plan.
type PlanStep struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"` // pending, in_progress, completed, skipped
	AssignedTo  string `json:"assigned_to,omitempty"`
	Order       int    `json:"order"`
}

// Valid plan statuses.
var validPlanStatuses = map[string]bool{
	"draft": true, "active": true, "completed": true, "archived": true,
}

// Valid step statuses.
var validStepStatuses = map[string]bool{
	"pending": true, "in_progress": true, "completed": true, "skipped": true,
}

func validatePlanStatus(status string) error {
	if !validPlanStatuses[status] {
		return fmt.Errorf("invalid plan status %q: must be one of draft, active, completed, archived", status)
	}
	return nil
}

func validateStepStatus(status string) error {
	if !validStepStatuses[status] {
		return fmt.Errorf("invalid step status %q: must be one of pending, in_progress, completed, skipped", status)
	}
	return nil
}

func generatePlanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CreatePlan stores a new plan under refs/coord/plans/{id}.
func (c *Coordinator) CreatePlan(plan *Plan) error {
	if plan.ID == "" {
		plan.ID = generatePlanID()
	}
	now := time.Now().UTC()
	plan.CreatedAt = now
	plan.UpdatedAt = now
	if plan.Status == "" {
		plan.Status = "draft"
	}
	if err := validatePlanStatus(plan.Status); err != nil {
		return err
	}
	// Assign step IDs and order.
	for i := range plan.Steps {
		if plan.Steps[i].ID == "" {
			plan.Steps[i].ID = fmt.Sprintf("s%d", i+1)
		}
		plan.Steps[i].Order = i + 1
		if plan.Steps[i].Status == "" {
			plan.Steps[i].Status = "pending"
		}
		if err := validateStepStatus(plan.Steps[i].Status); err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
	}

	h, err := c.writeJSONBlob(plan)
	if err != nil {
		return fmt.Errorf("write plan blob: %w", err)
	}
	ref := refPath("plans", plan.ID)
	return c.Repo.UpdateRef(ref, h)
}

// GetPlan reads a plan by ID from refs/coord/plans/{id}.
func (c *Coordinator) GetPlan(id string) (*Plan, error) {
	ref := refPath("plans", id)
	h, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return nil, fmt.Errorf("plan %q not found: %w", id, err)
	}
	var plan Plan
	if err := c.readJSONBlob(h, &plan); err != nil {
		return nil, fmt.Errorf("read plan: %w", err)
	}
	return &plan, nil
}

// UpdatePlan overwrites a plan blob and updates the ref.
func (c *Coordinator) UpdatePlan(plan *Plan) error {
	if err := validatePlanStatus(plan.Status); err != nil {
		return err
	}
	for i, step := range plan.Steps {
		if err := validateStepStatus(step.Status); err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
	}
	plan.UpdatedAt = time.Now().UTC()
	h, err := c.writeJSONBlob(plan)
	if err != nil {
		return fmt.Errorf("write plan blob: %w", err)
	}
	ref := refPath("plans", plan.ID)
	return c.Repo.UpdateRef(ref, h)
}

// ListPlans returns all plans stored under refs/coord/plans/.
func (c *Coordinator) ListPlans() ([]*Plan, error) {
	refs, err := c.Repo.ListRefs("coord/plans")
	if err != nil {
		return nil, fmt.Errorf("list plan refs: %w", err)
	}

	var plans []*Plan
	for _, hash := range refs {
		var plan Plan
		if err := c.readJSONBlob(hash, &plan); err != nil {
			continue
		}
		plans = append(plans, &plan)
	}
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].CreatedAt.After(plans[j].CreatedAt)
	})
	return plans, nil
}

// DeletePlan removes a plan from refs/coord/plans/{id}.
func (c *Coordinator) DeletePlan(id string) error {
	ref := refPath("plans", id)
	oldHash, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return fmt.Errorf("plan %q not found: %w", id, err)
	}
	return c.Repo.DeleteRefCAS(ref, oldHash)
}
