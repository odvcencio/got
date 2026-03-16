package coord

import (
	"fmt"
	"sort"
	"time"
)

// Task represents an operational work item stored in refs/coord/tasks/.
// Tasks are distinct from Plans: plans track designs/feature lifecycle,
// tasks track immediate operational work.
type Task struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`                // pending, in_progress, completed, blocked
	AssignedTo  string    `json:"assigned_to,omitempty"`  // agent name or ID
	Workspace   string    `json:"workspace,omitempty"`    // which repo this task targets
	PlanID      string    `json:"plan_id,omitempty"`      // optional link to parent plan
	PlanStepID  string    `json:"plan_step_id,omitempty"` // optional link to plan step
	Priority    int       `json:"priority,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Tags        []string  `json:"tags,omitempty"`
}

// Valid task statuses.
var validTaskStatuses = map[string]bool{
	"pending": true, "in_progress": true, "completed": true, "blocked": true,
}

func validateTaskStatus(status string) error {
	if !validTaskStatuses[status] {
		return fmt.Errorf("invalid task status %q: must be one of pending, in_progress, completed, blocked", status)
	}
	return nil
}

func generateTaskID() string {
	return generatePlanID() // same random hex approach
}

// CreateTask stores a new task under refs/coord/tasks/{id}.
func (c *Coordinator) CreateTask(task *Task) error {
	if task.Title == "" {
		return fmt.Errorf("task title is required")
	}
	if task.ID == "" {
		task.ID = generateTaskID()
	}
	now := time.Now().UTC()
	task.CreatedAt = now
	task.UpdatedAt = now
	if task.Status == "" {
		task.Status = "pending"
	}
	if err := validateTaskStatus(task.Status); err != nil {
		return err
	}

	h, err := c.writeJSONBlob(task)
	if err != nil {
		return fmt.Errorf("write task blob: %w", err)
	}
	ref := refPath("tasks", task.ID)
	return c.Repo.UpdateRef(ref, h)
}

// GetTask reads a task by ID from refs/coord/tasks/{id}.
func (c *Coordinator) GetTask(id string) (*Task, error) {
	ref := refPath("tasks", id)
	h, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return nil, fmt.Errorf("task %q not found: %w", id, err)
	}
	var task Task
	if err := c.readJSONBlob(h, &task); err != nil {
		return nil, fmt.Errorf("read task: %w", err)
	}
	return &task, nil
}

// UpdateTask overwrites a task blob and updates the ref.
func (c *Coordinator) UpdateTask(task *Task) error {
	if err := validateTaskStatus(task.Status); err != nil {
		return err
	}
	task.UpdatedAt = time.Now().UTC()
	h, err := c.writeJSONBlob(task)
	if err != nil {
		return fmt.Errorf("write task blob: %w", err)
	}
	ref := refPath("tasks", task.ID)
	return c.Repo.UpdateRef(ref, h)
}

// ListTasks returns all tasks stored under refs/coord/tasks/.
func (c *Coordinator) ListTasks() ([]*Task, error) {
	refs, err := c.Repo.ListRefs("coord/tasks")
	if err != nil {
		return nil, fmt.Errorf("list task refs: %w", err)
	}

	var tasks []*Task
	for _, hash := range refs {
		var task Task
		if err := c.readJSONBlob(hash, &task); err != nil {
			continue
		}
		tasks = append(tasks, &task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		// Higher priority first, then newer first.
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority > tasks[j].Priority
		}
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
	return tasks, nil
}

// DeleteTask removes a task from refs/coord/tasks/{id}.
func (c *Coordinator) DeleteTask(id string) error {
	ref := refPath("tasks", id)
	oldHash, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return fmt.Errorf("task %q not found: %w", id, err)
	}
	return c.Repo.DeleteRefCAS(ref, oldHash)
}

// ListTasksByPlan returns all tasks linked to a specific plan.
func (c *Coordinator) ListTasksByPlan(planID string) ([]*Task, error) {
	all, err := c.ListTasks()
	if err != nil {
		return nil, err
	}
	var filtered []*Task
	for _, t := range all {
		if t.PlanID == planID {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

// ListTasksByWorkspace returns all tasks targeting a specific workspace.
func (c *Coordinator) ListTasksByWorkspace(workspace string) ([]*Task, error) {
	all, err := c.ListTasks()
	if err != nil {
		return nil, err
	}
	var filtered []*Task
	for _, t := range all {
		if t.Workspace == workspace {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

// ListTasksByAssignee returns all tasks assigned to a specific agent.
func (c *Coordinator) ListTasksByAssignee(agentName string) ([]*Task, error) {
	all, err := c.ListTasks()
	if err != nil {
		return nil, err
	}
	var filtered []*Task
	for _, t := range all {
		if t.AssignedTo == agentName {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}
