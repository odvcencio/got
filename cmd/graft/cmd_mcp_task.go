package main

import (
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/coord"
)

// mcpTaskToolDefs returns tool definitions for task management.
func mcpTaskToolDefs() []mcpTool {
	return []mcpTool{
		{
			Name:        "graft_task_create",
			Description: "Create an operational task. Tasks track immediate work items, distinct from plans which track feature lifecycle/design.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"title":        {Type: "string", Description: "task title (required)"},
					"description":  {Type: "string", Description: "task description"},
					"workspace":    {Type: "string", Description: "target workspace name"},
					"plan_id":      {Type: "string", Description: "parent plan ID to link this task to"},
					"plan_step_id": {Type: "string", Description: "parent plan step ID"},
					"assign":       {Type: "string", Description: "agent name or ID to assign"},
					"priority":     {Type: "string", Description: "priority number (higher = more important)"},
					"tags":         {Type: "string", Description: "comma-separated tags (e.g. backend,orchard)"},
				},
				Required: []string{"title"},
			}.toMap(),
		},
		{
			Name:        "graft_task_list",
			Description: "List operational tasks with optional filters. Use --all to aggregate tasks across all registered workspaces.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"status":    {Type: "string", Description: "filter by status: pending, in_progress, completed, blocked"},
					"workspace": {Type: "string", Description: "filter by target workspace"},
					"assignee":  {Type: "string", Description: "filter by assignee"},
					"plan_id":   {Type: "string", Description: "filter by parent plan ID"},
					"all":       {Type: "boolean", Description: "aggregate tasks across all registered workspaces"},
				},
			}.toMap(),
		},
		{
			Name:        "graft_task_get",
			Description: "Get full details of a task by ID.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"id": {Type: "string", Description: "task ID (required)"},
				},
				Required: []string{"id"},
			}.toMap(),
		},
		{
			Name:        "graft_task_update",
			Description: "Update a task's status, title, description, assignee, or other fields.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"id":          {Type: "string", Description: "task ID (required)"},
					"status":      {Type: "string", Description: "new status: pending, in_progress, completed, blocked"},
					"title":       {Type: "string", Description: "new title"},
					"description": {Type: "string", Description: "new description"},
					"assign":      {Type: "string", Description: "assign to agent name or ID"},
					"workspace":   {Type: "string", Description: "target workspace"},
					"priority":    {Type: "string", Description: "priority number"},
					"tags":        {Type: "string", Description: "comma-separated tags (replaces existing)"},
				},
				Required: []string{"id"},
			}.toMap(),
		},
		{
			Name:        "graft_task_claim",
			Description: "Assign a task to the current active agent and set status to in_progress.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"id": {Type: "string", Description: "task ID (required)"},
				},
				Required: []string{"id"},
			}.toMap(),
		},
		{
			Name:        "graft_task_delete",
			Description: "Delete a task by ID.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"id": {Type: "string", Description: "task ID (required)"},
				},
				Required: []string{"id"},
			}.toMap(),
		},
	}
}

// mcpDispatchTaskTool routes a task tool call to its handler.
func mcpDispatchTaskTool(name string, args map[string]any) (any, error) {
	switch name {
	case "graft_task_create":
		return mcpToolTaskCreate(args)
	case "graft_task_list":
		return mcpToolTaskList(args)
	case "graft_task_get":
		return mcpToolTaskGet(args)
	case "graft_task_update":
		return mcpToolTaskUpdate(args)
	case "graft_task_claim":
		return mcpToolTaskClaim(args)
	case "graft_task_delete":
		return mcpToolTaskDelete(args)
	default:
		return nil, fmt.Errorf("unknown task tool %q", name)
	}
}

// --- Tool implementations ---

func mcpToolTaskCreate(args map[string]any) (any, error) {
	title := mcpArgString(args, "title")
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	task := &coord.Task{
		Title:       title,
		Description: mcpArgString(args, "description"),
		Workspace:   mcpArgString(args, "workspace"),
		PlanID:      mcpArgString(args, "plan_id"),
		PlanStepID:  mcpArgString(args, "plan_step_id"),
		AssignedTo:  mcpArgString(args, "assign"),
	}

	if p := mcpArgString(args, "priority"); p != "" {
		fmt.Sscanf(p, "%d", &task.Priority)
	}

	if tagsStr := mcpArgString(args, "tags"); tagsStr != "" {
		for _, tag := range strings.Split(tagsStr, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				task.Tags = append(task.Tags, tag)
			}
		}
	}

	if err := c.CreateTask(task); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	return map[string]any{
		"status": "created",
		"id":     task.ID,
		"title":  task.Title,
	}, nil
}

func mcpToolTaskList(args map[string]any) (any, error) {
	type taskSummary struct {
		ID              string   `json:"id"`
		Title           string   `json:"title"`
		Status          string   `json:"status"`
		AssignedTo      string   `json:"assigned_to,omitempty"`
		Workspace       string   `json:"workspace,omitempty"`
		PlanID          string   `json:"plan_id,omitempty"`
		Priority        int      `json:"priority,omitempty"`
		Tags            []string `json:"tags,omitempty"`
		SourceWorkspace string   `json:"source_workspace,omitempty"`
		CreatedAt       string   `json:"created_at"`
		UpdatedAt       string   `json:"updated_at"`
	}

	var summaries []taskSummary
	collectFromCoordinator := func(wsName string, c *coord.Coordinator) error {
		tasks, err := c.ListTasks()
		if err != nil {
			return nil // skip errors
		}
		for _, t := range tasks {
			summaries = append(summaries, taskSummary{
				ID:              t.ID,
				Title:           t.Title,
				Status:          t.Status,
				AssignedTo:      t.AssignedTo,
				Workspace:       t.Workspace,
				PlanID:          t.PlanID,
				Priority:        t.Priority,
				Tags:            t.Tags,
				SourceWorkspace: wsName,
				CreatedAt:       t.CreatedAt.Format("2006-01-02T15:04:05Z"),
				UpdatedAt:       t.UpdatedAt.Format("2006-01-02T15:04:05Z"),
			})
		}
		return nil
	}

	if mcpArgBool(args, "all") {
		if err := iterateWorkspaces(func(name string, c *coord.Coordinator) error {
			return collectFromCoordinator(name, c)
		}); err != nil {
			return nil, err
		}
	} else {
		c, _, err := openCoordinator()
		if err != nil {
			return nil, err
		}
		if err := collectFromCoordinator("", c); err != nil {
			return nil, err
		}
	}

	// Apply filters.
	statusFilter := mcpArgString(args, "status")
	workspaceFilter := mcpArgString(args, "workspace")
	assigneeFilter := mcpArgString(args, "assignee")
	planFilter := mcpArgString(args, "plan_id")

	var filtered []taskSummary
	for _, s := range summaries {
		if statusFilter != "" && s.Status != statusFilter {
			continue
		}
		if workspaceFilter != "" && s.Workspace != workspaceFilter {
			continue
		}
		if assigneeFilter != "" && s.AssignedTo != assigneeFilter {
			continue
		}
		if planFilter != "" && s.PlanID != planFilter {
			continue
		}
		filtered = append(filtered, s)
	}

	if filtered == nil {
		filtered = []taskSummary{}
	}

	return map[string]any{
		"count": len(filtered),
		"tasks": filtered,
	}, nil
}

func mcpToolTaskGet(args map[string]any) (any, error) {
	id := mcpArgString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	task, err := c.GetTask(id)
	if err != nil {
		return nil, err
	}
	return task, nil
}

func mcpToolTaskUpdate(args map[string]any) (any, error) {
	id := mcpArgString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	task, err := c.GetTask(id)
	if err != nil {
		return nil, err
	}

	if s := mcpArgString(args, "status"); s != "" {
		task.Status = s
	}
	if t := mcpArgString(args, "title"); t != "" {
		task.Title = t
	}
	if d := mcpArgString(args, "description"); d != "" {
		task.Description = d
	}
	if a := mcpArgString(args, "assign"); a != "" {
		task.AssignedTo = a
	}
	if w := mcpArgString(args, "workspace"); w != "" {
		task.Workspace = w
	}
	if p := mcpArgString(args, "priority"); p != "" {
		fmt.Sscanf(p, "%d", &task.Priority)
	}
	if tagsStr := mcpArgString(args, "tags"); tagsStr != "" {
		task.Tags = nil
		for _, tag := range strings.Split(tagsStr, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				task.Tags = append(task.Tags, tag)
			}
		}
	}

	if err := c.UpdateTask(task); err != nil {
		return nil, fmt.Errorf("update task: %w", err)
	}

	return map[string]any{
		"status":      "updated",
		"id":          task.ID,
		"title":       task.Title,
		"task_status": task.Status,
	}, nil
}

func mcpToolTaskClaim(args map[string]any) (any, error) {
	id := mcpArgString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	activeID := readActiveAgentID(r)
	if activeID == "" {
		return nil, fmt.Errorf("no active coordination session; use graft_workon first")
	}

	agentName := activeID
	if agent, err := c.GetAgent(activeID); err == nil {
		agentName = agent.Name
	}

	task, err := c.GetTask(id)
	if err != nil {
		return nil, err
	}

	task.AssignedTo = agentName
	if task.Status == "pending" {
		task.Status = "in_progress"
	}

	if err := c.UpdateTask(task); err != nil {
		return nil, fmt.Errorf("claim task: %w", err)
	}

	return map[string]any{
		"status":      "claimed",
		"task_id":     task.ID,
		"assigned_to": agentName,
		"task_status": task.Status,
	}, nil
}

func mcpToolTaskDelete(args map[string]any) (any, error) {
	id := mcpArgString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	if err := c.DeleteTask(id); err != nil {
		return nil, err
	}

	return map[string]any{
		"status": "deleted",
		"id":     id,
	}, nil
}

