package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

// --- iterateWorkspaces helper for cross-repo coordination ---

// iterateWorkspaces calls fn for each registered workspace plus the current repo.
func iterateWorkspaces(fn func(name string, c *coord.Coordinator) error) error {
	cfg, err := userconfig.Load()
	if err != nil {
		return fmt.Errorf("load user config: %w", err)
	}

	visited := make(map[string]bool)

	if cfg.Workspaces != nil {
		for name, wsPath := range cfg.Workspaces {
			r, err := repo.Open(wsPath)
			if err != nil {
				continue // skip non-graft workspaces
			}
			c := coord.New(r, coord.DefaultConfig)
			if err := fn(name, c); err != nil {
				return err
			}
			visited[wsPath] = true
		}
	}

	// Also include current repo if not already visited.
	c, _, err := openCoordinator()
	if err == nil {
		cwd, _ := os.Getwd()
		if !visited[cwd] {
			_ = fn("(local)", c)
		}
	}

	return nil
}

// --- Task subcommand ---

func newCoordTaskCmd(jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage coordination tasks (operational work items)",
	}

	cmd.AddCommand(newCoordTaskListCmd(jsonFlag))
	cmd.AddCommand(newCoordTaskGetCmd(jsonFlag))
	cmd.AddCommand(newCoordTaskCreateCmd(jsonFlag))
	cmd.AddCommand(newCoordTaskUpdateCmd(jsonFlag))
	cmd.AddCommand(newCoordTaskClaimCmd(jsonFlag))
	cmd.AddCommand(newCoordTaskDeleteCmd(jsonFlag))

	return cmd
}

func newCoordTaskListCmd(jsonFlag *bool) *cobra.Command {
	var (
		status    string
		workspace string
		assignee  string
		planID    string
		all       bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks with optional filters",
		RunE: func(cmd *cobra.Command, args []string) error {
			type taskWithWorkspace struct {
				*coord.Task
				SourceWorkspace string `json:"source_workspace,omitempty"`
			}

			var allTasks []taskWithWorkspace

			if all {
				if err := iterateWorkspaces(func(name string, c *coord.Coordinator) error {
					tasks, err := c.ListTasks()
					if err != nil {
						return nil // skip errors for individual workspaces
					}
					for _, t := range tasks {
						allTasks = append(allTasks, taskWithWorkspace{
							Task:            t,
							SourceWorkspace: name,
						})
					}
					return nil
				}); err != nil {
					return err
				}
			} else {
				c, _, err := openCoordinator()
				if err != nil {
					return err
				}
				tasks, err := c.ListTasks()
				if err != nil {
					return fmt.Errorf("list tasks: %w", err)
				}
				for _, t := range tasks {
					allTasks = append(allTasks, taskWithWorkspace{Task: t})
				}
			}

			// Apply filters.
			var filtered []taskWithWorkspace
			for _, t := range allTasks {
				if status != "" && t.Status != status {
					continue
				}
				if workspace != "" && t.Workspace != workspace {
					continue
				}
				if assignee != "" && t.AssignedTo != assignee {
					continue
				}
				if planID != "" && t.PlanID != planID {
					continue
				}
				filtered = append(filtered, t)
			}

			// Sort: higher priority first, then newer first.
			sort.Slice(filtered, func(i, j int) bool {
				if filtered[i].Priority != filtered[j].Priority {
					return filtered[i].Priority > filtered[j].Priority
				}
				return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
			})

			if *jsonFlag {
				return outputJSON(filtered)
			}

			if len(filtered) == 0 {
				fmt.Println("No tasks.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			if all {
				fmt.Fprintln(w, "ID\tTITLE\tSTATUS\tASSIGNEE\tWORKSPACE\tSOURCE\tPRIORITY")
				for _, t := range filtered {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
						t.ID, truncate(t.Title, 40), t.Status,
						dashIfEmpty(t.AssignedTo), dashIfEmpty(t.Workspace),
						t.SourceWorkspace, t.Priority)
				}
			} else {
				fmt.Fprintln(w, "ID\tTITLE\tSTATUS\tASSIGNEE\tWORKSPACE\tPRIORITY")
				for _, t := range filtered {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n",
						t.ID, truncate(t.Title, 40), t.Status,
						dashIfEmpty(t.AssignedTo), dashIfEmpty(t.Workspace),
						t.Priority)
				}
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "filter by status: pending, in_progress, completed, blocked")
	cmd.Flags().StringVar(&workspace, "workspace", "", "filter by target workspace")
	cmd.Flags().StringVar(&assignee, "assignee", "", "filter by assignee")
	cmd.Flags().StringVar(&planID, "plan", "", "filter by parent plan ID")
	cmd.Flags().BoolVar(&all, "all", false, "aggregate tasks across all registered workspaces")
	return cmd
}

func newCoordTaskGetCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "get <task-id>",
		Short: "Show task details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			task, err := c.GetTask(args[0])
			if err != nil {
				return fmt.Errorf("get task: %w", err)
			}

			if *jsonFlag {
				return outputJSON(task)
			}

			fmt.Printf("Task: %s\n", task.Title)
			fmt.Printf("  ID:          %s\n", task.ID)
			fmt.Printf("  Status:      %s\n", task.Status)
			if task.Description != "" {
				fmt.Printf("  Description: %s\n", task.Description)
			}
			if task.AssignedTo != "" {
				fmt.Printf("  Assigned to: %s\n", task.AssignedTo)
			}
			if task.Workspace != "" {
				fmt.Printf("  Workspace:   %s\n", task.Workspace)
			}
			if task.PlanID != "" {
				fmt.Printf("  Plan:        %s\n", task.PlanID)
				if task.PlanStepID != "" {
					fmt.Printf("  Plan step:   %s\n", task.PlanStepID)
				}
			}
			if task.Priority != 0 {
				fmt.Printf("  Priority:    %d\n", task.Priority)
			}
			if len(task.Tags) > 0 {
				fmt.Printf("  Tags:        %s\n", strings.Join(task.Tags, ", "))
			}
			fmt.Printf("  Created:     %s\n", task.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("  Updated:     %s\n", task.UpdatedAt.Format("2006-01-02 15:04:05"))

			return nil
		},
	}
}

func newCoordTaskCreateCmd(jsonFlag *bool) *cobra.Command {
	var (
		description string
		workspace   string
		planID      string
		planStepID  string
		assign      string
		priority    int
		tags        string
	)

	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			task := &coord.Task{
				Title:       args[0],
				Description: description,
				Workspace:   workspace,
				PlanID:      planID,
				PlanStepID:  planStepID,
				AssignedTo:  assign,
				Priority:    priority,
			}

			if tags != "" {
				for _, tag := range strings.Split(tags, ",") {
					tag = strings.TrimSpace(tag)
					if tag != "" {
						task.Tags = append(task.Tags, tag)
					}
				}
			}

			if err := c.CreateTask(task); err != nil {
				return fmt.Errorf("create task: %w", err)
			}

			if *jsonFlag {
				return outputJSON(task)
			}

			fmt.Printf("Created task %s: %s\n", task.ID, task.Title)
			return nil
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "task description")
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace")
	cmd.Flags().StringVar(&planID, "plan", "", "parent plan ID")
	cmd.Flags().StringVar(&planStepID, "plan-step", "", "parent plan step ID")
	cmd.Flags().StringVar(&assign, "assign", "", "assign to agent name or ID")
	cmd.Flags().IntVar(&priority, "priority", 0, "task priority (higher = more important)")
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tags")
	return cmd
}

func newCoordTaskUpdateCmd(jsonFlag *bool) *cobra.Command {
	var (
		status      string
		title       string
		description string
		assign      string
		workspace   string
		priority    int
		tags        string
	)

	cmd := &cobra.Command{
		Use:   "update <task-id>",
		Short: "Update a task's fields",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			task, err := c.GetTask(args[0])
			if err != nil {
				return fmt.Errorf("get task: %w", err)
			}

			if status != "" {
				task.Status = status
			}
			if title != "" {
				task.Title = title
			}
			if description != "" {
				task.Description = description
			}
			if assign != "" {
				task.AssignedTo = assign
			}
			if workspace != "" {
				task.Workspace = workspace
			}
			if cmd.Flags().Changed("priority") {
				task.Priority = priority
			}
			if tags != "" {
				task.Tags = nil
				for _, tag := range strings.Split(tags, ",") {
					tag = strings.TrimSpace(tag)
					if tag != "" {
						task.Tags = append(task.Tags, tag)
					}
				}
			}

			if err := c.UpdateTask(task); err != nil {
				return fmt.Errorf("update task: %w", err)
			}

			if *jsonFlag {
				return outputJSON(task)
			}

			fmt.Printf("Updated task %s: %s [%s]\n", task.ID, task.Title, task.Status)
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "new status: pending, in_progress, completed, blocked")
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	cmd.Flags().StringVar(&assign, "assign", "", "assign to agent name or ID")
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace")
	cmd.Flags().IntVar(&priority, "priority", 0, "task priority")
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tags (replaces existing)")
	return cmd
}

func newCoordTaskClaimCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "claim <task-id>",
		Short: "Assign a task to the current agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinator()
			if err != nil {
				return err
			}

			activeID := readActiveAgentID(r)
			if activeID == "" {
				return fmt.Errorf("no active coordination session; run 'graft workon --as <name>' first")
			}

			// Resolve agent name.
			agentName := activeID
			if agent, err := c.GetAgent(activeID); err == nil {
				agentName = agent.Name
			}

			task, err := c.GetTask(args[0])
			if err != nil {
				return fmt.Errorf("get task: %w", err)
			}

			task.AssignedTo = agentName
			if task.Status == "pending" {
				task.Status = "in_progress"
			}

			if err := c.UpdateTask(task); err != nil {
				return fmt.Errorf("claim task: %w", err)
			}

			if *jsonFlag {
				return outputJSON(map[string]any{
					"status":      "claimed",
					"task_id":     task.ID,
					"assigned_to": agentName,
					"task_status": task.Status,
				})
			}

			fmt.Printf("Claimed task %s: %s (assigned to %s)\n", task.ID, task.Title, agentName)
			return nil
		},
	}
}

func newCoordTaskDeleteCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <task-id>",
		Short: "Delete a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			if err := c.DeleteTask(args[0]); err != nil {
				return fmt.Errorf("delete task: %w", err)
			}

			if *jsonFlag {
				return outputJSON(map[string]string{
					"status": "deleted",
					"id":     args[0],
				})
			}

			fmt.Printf("Deleted task %s\n", args[0])
			return nil
		},
	}
}

// dashIfEmpty returns "-" if s is empty, otherwise s.
func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
