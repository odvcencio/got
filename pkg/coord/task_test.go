package coord

import (
	"testing"
)

func TestCreateAndListTasks(t *testing.T) {
	c := newTestCoordinator(t)

	task1 := &Task{Title: "Task Alpha", Description: "First task"}
	if err := c.CreateTask(task1); err != nil {
		t.Fatalf("CreateTask 1: %v", err)
	}
	if task1.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if task1.Status != "pending" {
		t.Errorf("default status = %q, want pending", task1.Status)
	}

	task2 := &Task{Title: "Task Beta", Description: "Second task"}
	if err := c.CreateTask(task2); err != nil {
		t.Fatalf("CreateTask 2: %v", err)
	}

	tasks, err := c.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestGetTask(t *testing.T) {
	c := newTestCoordinator(t)

	task := &Task{
		Title:       "Detailed Task",
		Description: "A task with details",
		Workspace:   "graft",
		Priority:    5,
		Tags:        []string{"backend", "coord"},
	}
	if err := c.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := c.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if got.Title != "Detailed Task" {
		t.Errorf("title = %q, want Detailed Task", got.Title)
	}
	if got.Workspace != "graft" {
		t.Errorf("workspace = %q, want graft", got.Workspace)
	}
	if got.Priority != 5 {
		t.Errorf("priority = %d, want 5", got.Priority)
	}
	if len(got.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(got.Tags))
	}
}

func TestGetTask_NotFound(t *testing.T) {
	c := newTestCoordinator(t)

	_, err := c.GetTask("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestTaskUpdateAndDelete(t *testing.T) {
	c := newTestCoordinator(t)

	task := &Task{Title: "Mutable Task"}
	if err := c.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	task.Status = "in_progress"
	task.AssignedTo = "agent-1"
	if err := c.UpdateTask(task); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	got, _ := c.GetTask(task.ID)
	if got.Status != "in_progress" {
		t.Errorf("status = %q, want in_progress", got.Status)
	}
	if got.AssignedTo != "agent-1" {
		t.Errorf("assigned_to = %q, want agent-1", got.AssignedTo)
	}

	if err := c.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	tasks, _ := c.ListTasks()
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after delete, got %d", len(tasks))
	}
}

func TestTaskInvalidStatus(t *testing.T) {
	c := newTestCoordinator(t)

	task := &Task{Title: "Bad Status", Status: "invalid"}
	if err := c.CreateTask(task); err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestCreateTaskRequiresTitle(t *testing.T) {
	c := newTestCoordinator(t)

	task := &Task{}
	if err := c.CreateTask(task); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestListTasksByPlan(t *testing.T) {
	c := newTestCoordinator(t)

	t1 := &Task{Title: "Plan task 1", PlanID: "plan-abc"}
	t2 := &Task{Title: "Plan task 2", PlanID: "plan-abc"}
	t3 := &Task{Title: "Other task", PlanID: "plan-xyz"}

	for _, task := range []*Task{t1, t2, t3} {
		if err := c.CreateTask(task); err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
	}

	tasks, err := c.ListTasksByPlan("plan-abc")
	if err != nil {
		t.Fatalf("ListTasksByPlan: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks for plan-abc, got %d", len(tasks))
	}
}

func TestListTasksByWorkspace(t *testing.T) {
	c := newTestCoordinator(t)

	t1 := &Task{Title: "Graft task", Workspace: "graft"}
	t2 := &Task{Title: "Orchard task", Workspace: "orchard"}
	t3 := &Task{Title: "Another graft task", Workspace: "graft"}

	for _, task := range []*Task{t1, t2, t3} {
		if err := c.CreateTask(task); err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
	}

	tasks, err := c.ListTasksByWorkspace("graft")
	if err != nil {
		t.Fatalf("ListTasksByWorkspace: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks for workspace graft, got %d", len(tasks))
	}
}

func TestListTasksByAssignee(t *testing.T) {
	c := newTestCoordinator(t)

	t1 := &Task{Title: "Task for alice", AssignedTo: "alice"}
	t2 := &Task{Title: "Task for bob", AssignedTo: "bob"}
	t3 := &Task{Title: "Another for alice", AssignedTo: "alice"}

	for _, task := range []*Task{t1, t2, t3} {
		if err := c.CreateTask(task); err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
	}

	tasks, err := c.ListTasksByAssignee("alice")
	if err != nil {
		t.Fatalf("ListTasksByAssignee: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks for alice, got %d", len(tasks))
	}
}

func TestTaskPrioritySorting(t *testing.T) {
	c := newTestCoordinator(t)

	t1 := &Task{Title: "Low priority", Priority: 1}
	t2 := &Task{Title: "High priority", Priority: 10}
	t3 := &Task{Title: "Medium priority", Priority: 5}

	for _, task := range []*Task{t1, t2, t3} {
		if err := c.CreateTask(task); err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
	}

	tasks, err := c.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0].Priority != 10 {
		t.Errorf("first task priority = %d, want 10", tasks[0].Priority)
	}
	if tasks[1].Priority != 5 {
		t.Errorf("second task priority = %d, want 5", tasks[1].Priority)
	}
	if tasks[2].Priority != 1 {
		t.Errorf("third task priority = %d, want 1", tasks[2].Priority)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	c := newTestCoordinator(t)

	err := c.DeleteTask("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestTaskStatusTransitions(t *testing.T) {
	c := newTestCoordinator(t)

	task := &Task{Title: "Status transitions"}
	if err := c.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	statuses := []string{"in_progress", "blocked", "in_progress", "completed"}
	for _, s := range statuses {
		task.Status = s
		if err := c.UpdateTask(task); err != nil {
			t.Fatalf("UpdateTask to %q: %v", s, err)
		}
		got, _ := c.GetTask(task.ID)
		if got.Status != s {
			t.Errorf("status = %q, want %q", got.Status, s)
		}
	}
}

func TestTaskWithPlanLink(t *testing.T) {
	c := newTestCoordinator(t)

	// Create a plan first.
	plan := &Plan{Title: "Parent Plan"}
	if err := c.CreatePlan(plan); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Create a task linked to the plan.
	task := &Task{
		Title:      "Linked task",
		PlanID:     plan.ID,
		PlanStepID: "s1",
	}
	if err := c.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := c.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.PlanID != plan.ID {
		t.Errorf("plan_id = %q, want %q", got.PlanID, plan.ID)
	}
	if got.PlanStepID != "s1" {
		t.Errorf("plan_step_id = %q, want s1", got.PlanStepID)
	}

	// ListTasksByPlan should find it.
	planTasks, err := c.ListTasksByPlan(plan.ID)
	if err != nil {
		t.Fatalf("ListTasksByPlan: %v", err)
	}
	if len(planTasks) != 1 {
		t.Fatalf("expected 1 task for plan, got %d", len(planTasks))
	}
}
