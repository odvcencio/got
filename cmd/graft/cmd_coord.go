package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

func newCoordCmd() *cobra.Command {
	var jsonFlag bool

	var allWorkspaces bool

	cmd := &cobra.Command{
		Use:   "coord",
		Short: "Multi-agent coordination dashboard and tools",
		Long:  `View and manage shared coordination state: agents, claims, plans, notes, tasks, feed, and impact analysis.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return coordDashboard(jsonFlag, allWorkspaces)
		},
	}

	cmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "JSON output")
	cmd.Flags().BoolVar(&allWorkspaces, "all", false, "aggregate dashboard across all registered workspaces")

	cmd.AddCommand(newCoordAgentsCmd(&jsonFlag))
	cmd.AddCommand(newCoordClaimsCmd(&jsonFlag))
	cmd.AddCommand(newCoordDecisionsCmd(&jsonFlag))
	cmd.AddCommand(newCoordFeedCmd(&jsonFlag))
	cmd.AddCommand(newCoordImpactCmd(&jsonFlag))
	cmd.AddCommand(newCoordCheckCmd(&jsonFlag))
	cmd.AddCommand(newCoordDiffCmd(&jsonFlag))
	cmd.AddCommand(newCoordXrefsCmd(&jsonFlag))
	cmd.AddCommand(newCoordGraphCmd(&jsonFlag))
	cmd.AddCommand(newCoordWatchCmd(&jsonFlag))
	cmd.AddCommand(newCoordUnwatchCmd(&jsonFlag))
	cmd.AddCommand(newCoordResolveCmd(&jsonFlag))
	cmd.AddCommand(newCoordPlanCmd(&jsonFlag))
	cmd.AddCommand(newCoordNoteCmd(&jsonFlag))
	cmd.AddCommand(newCoordTaskCmd(&jsonFlag))
	cmd.AddCommand(newCoordPublishCmd(&jsonFlag))
	cmd.AddCommand(newCoordHeartbeatCmd(&jsonFlag))
	cmd.AddCommand(newCoordSessionsCmd(&jsonFlag))
	cmd.AddCommand(newCoordPresenceCmd(&jsonFlag))
	cmd.AddCommand(newCoordReadingCmd(&jsonFlag))

	return cmd
}

// openCoordinator opens the repo and creates a coordinator.
func openCoordinator() (*coord.Coordinator, *repo.Repo, error) {
	r, err := repo.Open(".")
	if err != nil {
		return nil, nil, fmt.Errorf("open repo: %w", err)
	}
	c := coord.New(r, coord.DefaultConfig)
	return c, r, nil
}

// readActiveAgentID reads the current agent ID from .graft/coord/agent-id.
func readActiveAgentID(r *repo.Repo) string {
	if envID := strings.TrimSpace(os.Getenv("GRAFT_COORD_AGENT_ID")); envID != "" {
		return envID
	}
	data, err := os.ReadFile(filepath.Join(r.GraftDir, "coord", "agent-id"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// --- Dashboard ---

func coordDashboard(jsonOutput bool, allWorkspaces bool) error {
	if allWorkspaces {
		return coordDashboardAll(jsonOutput)
	}

	c, r, err := openCoordinator()
	if err != nil {
		return err
	}

	agents, _ := c.ListAgents()
	claims, _ := c.ListClaims()
	feedEvents, _ := c.WalkFeed("", 100)
	tasks, _ := c.ListTasks()
	notes, _ := c.ListNotes()

	// Count conflicts: claims by different agents on same entity
	conflictCount := 0
	claimsByEntity := make(map[string][]string)
	for _, cl := range claims {
		claimsByEntity[cl.EntityKeyHash] = append(claimsByEntity[cl.EntityKeyHash], cl.AgentName)
	}
	for _, holders := range claimsByEntity {
		if len(holders) > 1 {
			conflictCount++
		}
	}

	// Count tasks by status.
	taskPending, taskInProgress := 0, 0
	for _, t := range tasks {
		switch t.Status {
		case "pending":
			taskPending++
		case "in_progress":
			taskInProgress++
		}
	}

	activeID := readActiveAgentID(r)

	result := dashboardResult{
		Agents:       len(agents),
		Claims:       len(claims),
		Conflicts:    conflictCount,
		FeedCount:    len(feedEvents),
		Notes:        len(notes),
		Tasks:        len(tasks),
		TasksPending: taskPending,
		TasksActive:  taskInProgress,
		ActiveID:     activeID,
	}

	if jsonOutput {
		return outputJSON(result)
	}

	fmt.Println("Coordination Dashboard")
	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("  Agents:     %d\n", result.Agents)
	fmt.Printf("  Claims:     %d\n", result.Claims)
	fmt.Printf("  Conflicts:  %d\n", result.Conflicts)
	fmt.Printf("  Feed:       %d event(s)\n", result.FeedCount)
	fmt.Printf("  Notes:      %d\n", result.Notes)
	fmt.Printf("  Tasks:      %d (%d pending, %d active)\n", result.Tasks, result.TasksPending, result.TasksActive)
	if activeID != "" {
		fmt.Printf("  Active as:  %s\n", activeID)
	} else {
		fmt.Printf("  Active as:  (not joined)\n")
	}

	return nil
}

func coordDashboardAll(jsonOutput bool) error {
	result := dashboardResult{}
	claimsByEntity := make(map[string][]string)

	if err := iterateWorkspaces(func(name string, c *coord.Coordinator) error {
		agents, _ := c.ListAgents()
		claims, _ := c.ListClaims()
		feedEvents, _ := c.WalkFeed("", 100)
		tasks, _ := c.ListTasks()
		notes, _ := c.ListNotes()

		result.Agents += len(agents)
		result.Claims += len(claims)
		result.FeedCount += len(feedEvents)
		result.Notes += len(notes)
		result.Tasks += len(tasks)

		for _, t := range tasks {
			switch t.Status {
			case "pending":
				result.TasksPending++
			case "in_progress":
				result.TasksActive++
			}
		}

		for _, cl := range claims {
			key := name + ":" + cl.EntityKeyHash
			claimsByEntity[key] = append(claimsByEntity[key], cl.AgentName)
		}
		return nil
	}); err != nil {
		return err
	}

	for _, holders := range claimsByEntity {
		if len(holders) > 1 {
			result.Conflicts++
		}
	}

	_, r, _ := openCoordinator()
	if r != nil {
		result.ActiveID = readActiveAgentID(r)
	}

	if jsonOutput {
		return outputJSON(result)
	}

	fmt.Println("Coordination Dashboard (all workspaces)")
	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("  Agents:     %d\n", result.Agents)
	fmt.Printf("  Claims:     %d\n", result.Claims)
	fmt.Printf("  Conflicts:  %d\n", result.Conflicts)
	fmt.Printf("  Feed:       %d event(s)\n", result.FeedCount)
	fmt.Printf("  Notes:      %d\n", result.Notes)
	fmt.Printf("  Tasks:      %d (%d pending, %d active)\n", result.Tasks, result.TasksPending, result.TasksActive)
	if result.ActiveID != "" {
		fmt.Printf("  Active as:  %s\n", result.ActiveID)
	} else {
		fmt.Printf("  Active as:  (not joined)\n")
	}

	return nil
}

type dashboardResult struct {
	Agents       int    `json:"agents"`
	Claims       int    `json:"claims"`
	Conflicts    int    `json:"conflicts"`
	FeedCount    int    `json:"feed_count"`
	Notes        int    `json:"notes"`
	Tasks        int    `json:"tasks"`
	TasksPending int    `json:"tasks_pending"`
	TasksActive  int    `json:"tasks_active"`
	ActiveID     string `json:"active_id,omitempty"`
}

// --- Agents ---

func newCoordAgentsCmd(jsonFlag *bool) *cobra.Command {
	var allWorkspaces bool

	cmd := &cobra.Command{
		Use:   "agents",
		Short: "List registered agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			var allAgents []coord.AgentInfo

			if allWorkspaces {
				cfg, err := userconfig.Load()
				if err != nil {
					return fmt.Errorf("load user config: %w", err)
				}
				if cfg.Workspaces != nil {
					for wsName, wsPath := range cfg.Workspaces {
						r, err := repo.Open(wsPath)
						if err != nil {
							continue // skip non-graft workspaces
						}
						wc := coord.New(r, coord.DefaultConfig)
						agents, err := wc.ListAgents()
						if err != nil {
							continue
						}
						for i := range agents {
							if agents[i].Workspace == "" {
								agents[i].Workspace = wsName
							}
						}
						allAgents = append(allAgents, agents...)
					}
				}
				// Also include current repo
				c, _, err := openCoordinator()
				if err == nil {
					agents, _ := c.ListAgents()
					// Deduplicate: skip agents already collected
					seen := make(map[string]bool)
					for _, a := range allAgents {
						seen[a.ID] = true
					}
					for _, a := range agents {
						if !seen[a.ID] {
							allAgents = append(allAgents, a)
						}
					}
				}
			} else {
				c, _, err := openCoordinator()
				if err != nil {
					return err
				}
				agents, err := c.ListAgents()
				if err != nil {
					return fmt.Errorf("list agents: %w", err)
				}
				allAgents = agents
			}

			if *jsonFlag {
				return outputJSON(allAgents)
			}

			if len(allAgents) == 0 {
				fmt.Println("No active agents.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tWORKSPACE\tHOST\tHEARTBEAT")
			for _, a := range allAgents {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					a.ID, a.Name, a.Workspace, a.Host,
					a.HeartbeatAt.Format("15:04:05"))
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&allWorkspaces, "all", false, "list agents across all registered workspaces")
	return cmd
}

// --- Claims ---

func newCoordClaimsCmd(jsonFlag *bool) *cobra.Command {
	var (
		workspace     string
		allWorkspaces bool
	)

	cmd := &cobra.Command{
		Use:   "claims",
		Short: "List active claims",
		RunE: func(cmd *cobra.Command, args []string) error {
			type claimWithSource struct {
				coord.ClaimInfo
				SourceWorkspace string `json:"source_workspace,omitempty"`
			}

			var allClaims []claimWithSource

			if allWorkspaces {
				if err := iterateWorkspaces(func(name string, c *coord.Coordinator) error {
					claims, err := c.ListClaims()
					if err != nil {
						return nil
					}
					for _, cl := range claims {
						allClaims = append(allClaims, claimWithSource{
							ClaimInfo:       cl,
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
				claims, err := c.ListClaims()
				if err != nil {
					return fmt.Errorf("list claims: %w", err)
				}
				for _, cl := range claims {
					allClaims = append(allClaims, claimWithSource{ClaimInfo: cl})
				}
			}

			// Filter by workspace agent name prefix if requested
			if workspace != "" {
				var filtered []claimWithSource
				for _, cl := range allClaims {
					if strings.Contains(cl.AgentName, workspace) || strings.Contains(cl.File, workspace) {
						filtered = append(filtered, cl)
					}
				}
				allClaims = filtered
			}

			if *jsonFlag {
				return outputJSON(allClaims)
			}

			if len(allClaims) == 0 {
				fmt.Println("No active claims.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			if allWorkspaces {
				fmt.Fprintln(w, "ENTITY\tFILE\tAGENT\tMODE\tSOURCE\tSINCE")
				for _, cl := range allClaims {
					entityDisplay := cl.EntityKey
					if len(entityDisplay) > 60 {
						entityDisplay = entityDisplay[:57] + "..."
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
						entityDisplay, cl.File, cl.AgentName, cl.Mode,
						cl.SourceWorkspace,
						cl.ClaimedAt.Format("15:04:05"))
				}
			} else {
				fmt.Fprintln(w, "ENTITY\tFILE\tAGENT\tMODE\tSINCE")
				for _, cl := range allClaims {
					entityDisplay := cl.EntityKey
					if len(entityDisplay) > 60 {
						entityDisplay = entityDisplay[:57] + "..."
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
						entityDisplay, cl.File, cl.AgentName, cl.Mode,
						cl.ClaimedAt.Format("15:04:05"))
				}
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "filter claims by workspace")
	cmd.Flags().BoolVar(&allWorkspaces, "all", false, "aggregate claims across all registered workspaces")
	return cmd
}

// --- Decisions ---

func newCoordDecisionsCmd(jsonFlag *bool) *cobra.Command {
	var limit int
	var mine bool
	var entity string

	cmd := &cobra.Command{
		Use:   "decisions",
		Short: "Show recent local decision traces",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, r, err := openCoordinator()
			if err != nil {
				return err
			}

			decisions, err := coord.ListDecisions(r.GraftDir, 0)
			if err != nil {
				return fmt.Errorf("list decisions: %w", err)
			}

			if mine {
				activeID := readActiveAgentID(r)
				var filtered []coord.DecisionGraph
				for _, decision := range decisions {
					if activeID != "" && decision.AgentID == activeID {
						filtered = append(filtered, decision)
					}
				}
				decisions = filtered
			}

			if entity != "" {
				var filtered []coord.DecisionGraph
				for _, decision := range decisions {
					if strings.Contains(decision.EntityKey, entity) || strings.Contains(decision.File, entity) {
						filtered = append(filtered, decision)
					}
				}
				decisions = filtered
			}

			if limit > 0 && len(decisions) > limit {
				decisions = decisions[:limit]
			}

			if *jsonFlag {
				return outputJSON(decisions)
			}

			if len(decisions) == 0 {
				fmt.Println("No decision traces.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TIME\tOUTCOME\tACTION\tENTITY\tRULE\tSOURCE")
			for _, decision := range decisions {
				entityDisplay := decision.EntityKey
				if entityDisplay == "" {
					entityDisplay = decision.File
				}
				if len(entityDisplay) > 60 {
					entityDisplay = entityDisplay[:57] + "..."
				}
				rule := decision.Rule
				if rule == "" {
					rule = "-"
				}
				source := decision.Source
				if source == "" {
					source = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					decision.CreatedAt.Format("2006-01-02 15:04:05"),
					decision.Outcome.Status,
					decision.Action,
					entityDisplay,
					rule,
					source,
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of decision traces to show")
	cmd.Flags().BoolVar(&mine, "mine", false, "show only decision traces for the active agent")
	cmd.Flags().StringVar(&entity, "entity", "", "filter decision traces by entity key or file path")
	return cmd
}

// --- Feed ---

func newCoordFeedCmd(jsonFlag *bool) *cobra.Command {
	var (
		since         string
		mine          bool
		allWorkspaces bool
	)

	cmd := &cobra.Command{
		Use:   "feed",
		Short: "Show coordination feed events",
		RunE: func(cmd *cobra.Command, args []string) error {
			type feedEventWithSource struct {
				coord.FeedEvent
				SourceWorkspace string `json:"source_workspace,omitempty"`
			}

			var allEvents []feedEventWithSource

			if allWorkspaces {
				if err := iterateWorkspaces(func(name string, c *coord.Coordinator) error {
					events, err := c.WalkFeed(since, 50)
					if err != nil {
						return nil
					}
					for _, ev := range events {
						allEvents = append(allEvents, feedEventWithSource{
							FeedEvent:       ev,
							SourceWorkspace: name,
						})
					}
					return nil
				}); err != nil {
					return err
				}
				// Sort by timestamp (FeedHash is derived from content, not time, so
				// we use the event order -- most recent first within each workspace).
			} else {
				c, _, err := openCoordinator()
				if err != nil {
					return err
				}
				events, err := c.WalkFeed(since, 50)
				if err != nil {
					return fmt.Errorf("walk feed: %w", err)
				}
				for _, ev := range events {
					allEvents = append(allEvents, feedEventWithSource{FeedEvent: ev})
				}
			}

			if mine {
				_, r, _ := openCoordinator()
				if r != nil {
					activeID := readActiveAgentID(r)
					if activeID != "" {
						var filtered []feedEventWithSource
						for _, ev := range allEvents {
							if ev.AgentID == activeID {
								filtered = append(filtered, ev)
							}
						}
						allEvents = filtered
					}
				}
			}

			if *jsonFlag {
				return outputJSON(allEvents)
			}

			if len(allEvents) == 0 {
				fmt.Println("No feed events.")
				return nil
			}

			for _, ev := range allEvents {
				prefix := ""
				if allWorkspaces && ev.SourceWorkspace != "" {
					prefix = fmt.Sprintf("[%s] ", ev.SourceWorkspace)
				}
				hashDisplay := ev.FeedHash
				if len(hashDisplay) > 8 {
					hashDisplay = hashDisplay[:8]
				}
				fmt.Printf("%s[%s] %s by %s", prefix, hashDisplay, ev.Event, ev.AgentName)
				if ev.CommitHash != "" {
					commitDisplay := ev.CommitHash
					if len(commitDisplay) > 8 {
						commitDisplay = commitDisplay[:8]
					}
					fmt.Printf(" (commit %s)", commitDisplay)
				}
				fmt.Println()
				for _, ent := range ev.Entities {
					breaking := ""
					if ent.Breaking {
						breaking = " [BREAKING]"
					}
					fmt.Printf("  %s %s in %s%s\n", ent.Change, ent.Key, ent.File, breaking)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "show events after this feed hash")
	cmd.Flags().BoolVar(&mine, "mine", false, "show only my events")
	cmd.Flags().BoolVar(&allWorkspaces, "all", false, "aggregate feed events across all registered workspaces")
	return cmd
}

// --- Impact ---

func newCoordImpactCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "impact [entity-key]",
		Short: "Run impact analysis for entity changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			cfg, _ := userconfig.Load()
			workspaces := make(map[string]string)
			if cfg != nil && cfg.Workspaces != nil {
				workspaces = cfg.Workspaces
			}

			var changes []coord.EntityChange
			if len(args) > 0 {
				for _, key := range args {
					changes = append(changes, coord.EntityChange{
						Key:    key,
						Change: "unknown",
					})
				}
			} else {
				// Use recent feed events to get changes
				events, _ := c.WalkFeed("", 10)
				for _, ev := range events {
					changes = append(changes, ev.Entities...)
				}
			}

			if len(changes) == 0 {
				if *jsonFlag {
					return outputJSON(coord.ImpactReport{})
				}
				fmt.Println("No entity changes to analyze.")
				return nil
			}

			report, err := c.AnalyzeImpact(changes, workspaces)
			if err != nil {
				return fmt.Errorf("analyze impact: %w", err)
			}

			if *jsonFlag {
				return outputJSON(report)
			}

			if len(report.Workspaces) == 0 {
				fmt.Println("No cross-workspace impact detected.")
				return nil
			}

			for ws, impact := range report.Workspaces {
				fmt.Printf("Workspace: %s\n", ws)
				if len(impact.Callers) > 0 {
					fmt.Printf("  Affected callers:\n")
					for _, caller := range impact.Callers {
						fmt.Printf("    %s\n", caller)
					}
				}
				if len(impact.AgentsAffected) > 0 {
					fmt.Printf("  Agents affected:\n")
					for _, agent := range impact.AgentsAffected {
						fmt.Printf("    %s\n", agent)
					}
				}
			}

			return nil
		},
	}
}

// --- Check ---

func newCoordCheckCmd(jsonFlag *bool) *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Quick conflict check (hook-optimized)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinator()
			if err != nil {
				return err
			}

			activeID := readActiveAgentID(r)
			claims, _ := c.ListClaims()

			// Find conflicts: claims on the same entity by different agents
			type conflict struct {
				EntityKey    string `json:"entity_key"`
				File         string `json:"file"`
				HeldBy       string `json:"held_by"`
				Mode         string `json:"mode"`
				Decision     string `json:"decision,omitempty"`
				Reason       string `json:"reason,omitempty"`
				Rule         string `json:"rule,omitempty"`
				RequireForce bool   `json:"require_force,omitempty"`
			}

			var conflicts []conflict
			if activeID != "" {
				// Check if any of our agent's potential claims conflict
				for _, cl := range claims {
					if cl.Agent != activeID && cl.Mode == coord.ClaimEditing {
						req := coord.ClaimRequest{
							EntityKey: cl.EntityKey,
							File:      cl.File,
							Mode:      coord.ClaimEditing,
						}
						ctx, decisionErr := c.InspectClaimDecisionWithExisting(activeID, req, &cl)
						if decisionErr != nil {
							return fmt.Errorf("evaluate claim decision: %w", decisionErr)
						}
						recordCoordDecision(c, cmd.ErrOrStderr(), "graft coord check", activeID, req, ctx, coord.DecisionOutcome{
							Status:  "inspection_reported",
							Message: coordDecisionMessage(req, ctx),
						})
						conflicts = append(conflicts, conflict{
							EntityKey:    cl.EntityKey,
							File:         cl.File,
							HeldBy:       cl.AgentName,
							Mode:         cl.Mode,
							Decision:     ctx.Decision.Action,
							Reason:       ctx.Decision.Reason,
							Rule:         ctx.Decision.Rule,
							RequireForce: ctx.Decision.RequireForce,
						})
					}
				}
			}

			result := struct {
				OK        bool       `json:"ok"`
				Conflicts []conflict `json:"conflicts,omitempty"`
			}{
				OK:        len(conflicts) == 0,
				Conflicts: conflicts,
			}

			if *jsonFlag {
				return outputJSON(result)
			}

			if quiet {
				if !result.OK {
					fmt.Printf("%d conflict(s)\n", len(conflicts))
					return fmt.Errorf("conflicts detected")
				}
				return nil
			}

			if result.OK {
				fmt.Println("No conflicts detected.")
			} else {
				fmt.Printf("%d conflict(s) detected:\n", len(conflicts))
				for _, c := range conflicts {
					decision := c.Decision
					if decision == "" {
						decision = "Conflict"
					}
					fmt.Printf("  [%s] %s in %s (held by %s)\n", decision, c.EntityKey, c.File, c.HeldBy)
					if c.Reason != "" {
						fmt.Printf("    %s\n", c.Reason)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&quiet, "quiet", false, "minimal output (exit code only)")
	return cmd
}

// --- Diff ---

func newCoordDiffCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <agent-id>",
		Short: "Show another agent's claimed entities",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			targetID := args[0]
			agent, err := c.GetAgent(targetID)
			if err != nil {
				return fmt.Errorf("agent not found: %w", err)
			}

			claims, _ := c.ListClaims()
			var agentClaims []coord.ClaimInfo
			for _, cl := range claims {
				if cl.Agent == targetID {
					agentClaims = append(agentClaims, cl)
				}
			}

			result := struct {
				Agent  *coord.AgentInfo  `json:"agent"`
				Claims []coord.ClaimInfo `json:"claims"`
			}{
				Agent:  agent,
				Claims: agentClaims,
			}

			if *jsonFlag {
				return outputJSON(result)
			}

			fmt.Printf("Agent: %s (%s)\n", agent.Name, agent.ID)
			if len(agentClaims) == 0 {
				fmt.Println("No active claims.")
				return nil
			}

			fmt.Printf("Claims (%d):\n", len(agentClaims))
			for _, cl := range agentClaims {
				fmt.Printf("  [%s] %s in %s\n", cl.Mode, cl.EntityKey, cl.File)
			}

			return nil
		},
	}
}

// --- Xrefs ---

func newCoordXrefsCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "xrefs <qualified-name>",
		Short: "Reverse call lookup for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			key := args[0]
			idx, err := c.LoadXrefIndex()
			if err != nil {
				// Try building a fresh one
				cfg, _ := userconfig.Load()
				if cfg != nil {
					modulePath := ""
					gomodPath := filepath.Join(c.Repo.RootDir, "go.mod")
					if deps, parseErr := coord.ParseGoModDeps(gomodPath); parseErr == nil {
						modulePath = deps.Module
					}
					idx, err = coord.BuildXrefIndex(c.Repo.RootDir, modulePath)
					if err != nil {
						return fmt.Errorf("build xref index: %w", err)
					}
				} else {
					return fmt.Errorf("no xref index available: %w", err)
				}
			}

			sites, ok := idx.Refs[key]
			if !ok {
				if *jsonFlag {
					return outputJSON([]coord.XrefCallSite{})
				}
				fmt.Printf("No references found for %s\n", key)
				return nil
			}

			if *jsonFlag {
				return outputJSON(sites)
			}

			fmt.Printf("References to %s:\n", key)
			for _, site := range sites {
				fmt.Printf("  %s:%d in %s\n", site.File, site.Line, site.Entity)
			}

			return nil
		},
	}
}

// --- Graph ---

func newCoordGraphCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "graph",
		Short: "Show workspace dependency graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := userconfig.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if cfg.Workspaces == nil || len(cfg.Workspaces) == 0 {
				if *jsonFlag {
					return outputJSON(map[string]any{"workspaces": map[string]string{}, "edges": []string{}})
				}
				fmt.Println("No workspaces configured. Use 'graft workspace add' or 'graft workon --auto-discover'.")
				return nil
			}

			graph, err := coord.BuildWorkspaceGraph(cfg.Workspaces)
			if err != nil {
				return fmt.Errorf("build workspace graph: %w", err)
			}

			type graphEdge struct {
				From string `json:"from"`
				To   string `json:"to"`
			}

			var edges []graphEdge
			for wsName := range cfg.Workspaces {
				deps := graph.DependentsOf(wsName)
				for _, dep := range deps {
					edges = append(edges, graphEdge{From: dep, To: wsName})
				}
			}

			if *jsonFlag {
				result := struct {
					Workspaces map[string]string `json:"workspaces"`
					Edges      []graphEdge       `json:"edges"`
				}{
					Workspaces: cfg.Workspaces,
					Edges:      edges,
				}
				return outputJSON(result)
			}

			fmt.Println("Workspace Dependency Graph")
			fmt.Println(strings.Repeat("-", 40))
			for wsName, wsPath := range cfg.Workspaces {
				deps := graph.DependentsOf(wsName)
				if len(deps) > 0 {
					fmt.Printf("  %s (%s)\n", wsName, wsPath)
					fmt.Printf("    depended on by: %s\n", strings.Join(deps, ", "))
				} else {
					fmt.Printf("  %s (%s) [leaf]\n", wsName, wsPath)
				}
			}

			return nil
		},
	}
}

// --- Watch ---

func newCoordWatchCmd(jsonFlag *bool) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "watch <entity-key>",
		Short: "Add a watch claim on an entity",
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

			entityKey := args[0]

			// Try to resolve entity key to a file if not provided
			filePath := file
			if filePath == "" {
				filePath = c.ResolveEntityFile(entityKey)
			}

			err = c.AcquireClaim(activeID, coord.ClaimRequest{
				EntityKey: entityKey,
				File:      filePath,
				Mode:      coord.ClaimWatching,
			})
			if err != nil {
				return fmt.Errorf("watch: %w", err)
			}

			if *jsonFlag {
				result := map[string]string{
					"status":     "watching",
					"entity_key": entityKey,
				}
				if filePath != "" {
					result["file"] = filePath
				}
				return outputJSON(result)
			}

			if filePath != "" {
				fmt.Printf("Watching: %s (in %s)\n", entityKey, filePath)
			} else {
				fmt.Printf("Watching: %s\n", entityKey)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "file path for the entity (auto-resolved if omitted)")
	return cmd
}

// --- Unwatch ---

func newCoordUnwatchCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "unwatch <entity-key>",
		Short: "Remove a watch claim",
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

			entityKey := args[0]
			keyHash := coord.EntityKeyHash(entityKey)
			if err := c.ReleaseWatch(keyHash, activeID); err != nil {
				return fmt.Errorf("unwatch: %w", err)
			}

			if *jsonFlag {
				return outputJSON(map[string]string{
					"status":     "unwatched",
					"entity_key": entityKey,
				})
			}

			fmt.Printf("Stopped watching: %s\n", entityKey)
			return nil
		},
	}
}

// --- Resolve ---

func newCoordResolveCmd(jsonFlag *bool) *cobra.Command {
	var transfer string

	cmd := &cobra.Command{
		Use:   "resolve <entity-key-hash>",
		Short: "Release or transfer a claim",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinator()
			if err != nil {
				return err
			}

			keyHash := args[0]
			activeID := readActiveAgentID(r)

			if transfer != "" {
				if activeID == "" {
					return fmt.Errorf("no active session for transfer source")
				}
				if err := c.TransferClaim(keyHash, activeID, transfer); err != nil {
					return fmt.Errorf("transfer: %w", err)
				}
				if *jsonFlag {
					return outputJSON(map[string]string{
						"status":   "transferred",
						"key_hash": keyHash,
						"to_agent": transfer,
					})
				}
				fmt.Printf("Claim %s transferred to %s\n", keyHash, transfer)
				return nil
			}

			if err := c.ReleaseClaim(keyHash); err != nil {
				return fmt.Errorf("release: %w", err)
			}

			if *jsonFlag {
				return outputJSON(map[string]string{
					"status":   "released",
					"key_hash": keyHash,
				})
			}

			fmt.Printf("Claim %s released\n", keyHash)
			return nil
		},
	}

	cmd.Flags().StringVar(&transfer, "transfer", "", "transfer claim to another agent ID")
	return cmd
}

// --- Plan ---

func newCoordPlanCmd(jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Manage coordination plans",
	}

	cmd.AddCommand(newCoordPlanListCmd(jsonFlag))
	cmd.AddCommand(newCoordPlanGetCmd(jsonFlag))
	cmd.AddCommand(newCoordPlanCreateCmd(jsonFlag))

	return cmd
}

func newCoordPlanListCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all coordination plans",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			plans, err := c.ListPlans()
			if err != nil {
				return fmt.Errorf("list plans: %w", err)
			}

			if *jsonFlag {
				return outputJSON(plans)
			}

			if len(plans) == 0 {
				fmt.Println("No plans.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTITLE\tSTATUS\tSTEPS\tCREATED")
			for _, p := range plans {
				completed := 0
				for _, s := range p.Steps {
					if s.Status == "completed" {
						completed++
					}
				}
				stepSummary := fmt.Sprintf("%d/%d done", completed, len(p.Steps))
				if len(p.Steps) == 0 {
					stepSummary = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					p.ID, truncate(p.Title, 40), p.Status, stepSummary,
					p.CreatedAt.Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
}

func newCoordPlanGetCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "get <plan-id>",
		Short: "Show plan details with step status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			plan, err := c.GetPlan(args[0])
			if err != nil {
				return fmt.Errorf("get plan: %w", err)
			}

			if *jsonFlag {
				return outputJSON(plan)
			}

			fmt.Printf("Plan: %s\n", plan.Title)
			fmt.Printf("  ID:      %s\n", plan.ID)
			fmt.Printf("  Status:  %s\n", plan.Status)
			if plan.Author != "" {
				fmt.Printf("  Author:  %s\n", plan.Author)
			}
			if plan.Description != "" {
				fmt.Printf("  Desc:    %s\n", plan.Description)
			}
			fmt.Printf("  Created: %s\n", plan.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("  Updated: %s\n", plan.UpdatedAt.Format("2006-01-02 15:04:05"))

			if len(plan.Steps) == 0 {
				fmt.Println("  Steps:   (none)")
				return nil
			}

			fmt.Printf("  Steps (%d):\n", len(plan.Steps))
			for _, step := range plan.Steps {
				marker := " "
				switch step.Status {
				case "completed":
					marker = "x"
				case "in_progress":
					marker = ">"
				case "skipped":
					marker = "-"
				}
				assignee := ""
				if step.AssignedTo != "" {
					assignee = fmt.Sprintf(" [%s]", step.AssignedTo)
				}
				fmt.Printf("    [%s] %d. %s%s\n", marker, step.Order, step.Description, assignee)
			}

			return nil
		},
	}
}

func newCoordPlanCreateCmd(jsonFlag *bool) *cobra.Command {
	var (
		description string
		status      string
	)

	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new coordination plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinator()
			if err != nil {
				return err
			}

			author := readActiveAgentID(r)

			plan := &coord.Plan{
				Title:       args[0],
				Description: description,
				Status:      status,
				Author:      author,
			}

			if err := c.CreatePlan(plan); err != nil {
				return fmt.Errorf("create plan: %w", err)
			}

			if *jsonFlag {
				return outputJSON(plan)
			}

			fmt.Printf("Created plan %s: %s\n", plan.ID, plan.Title)
			return nil
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "plan description")
	cmd.Flags().StringVar(&status, "status", "draft", "initial status (draft, active)")
	return cmd
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// --- Publish ---

func newCoordPublishCmd(jsonFlag *bool) *cobra.Command {
	var commitRef string

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a feed event for a commit (for git/buckley commits)",
		Long:  `Manually publish a coordination feed event for the latest commit, useful when commits are made via git or buckley rather than graft.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinator()
			if err != nil {
				return err
			}

			activeID := readActiveAgentID(r)
			if activeID == "" {
				return fmt.Errorf("no active coordination session; run 'graft workon --as <name>' first")
			}
			c.AgentID = activeID

			// Resolve the commit to publish
			ref := commitRef
			if ref == "" {
				ref = "HEAD"
			}
			commitHash, err := c.Repo.ResolveRef(ref)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", ref, err)
			}

			if err := c.PostCommitHook(commitHash); err != nil {
				return fmt.Errorf("publish: %w", err)
			}

			if *jsonFlag {
				return outputJSON(map[string]string{
					"status":      "published",
					"commit_hash": string(commitHash),
					"agent_id":    activeID,
				})
			}

			fmt.Printf("Published feed event for commit %s\n", string(commitHash)[:12])
			return nil
		},
	}

	cmd.Flags().StringVar(&commitRef, "commit", "", "commit ref to publish (default: HEAD)")
	return cmd
}

// --- Heartbeat ---

func newCoordHeartbeatCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "heartbeat",
		Short: "Update the active agent's heartbeat timestamp",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinator()
			if err != nil {
				return err
			}

			activeID := readActiveAgentID(r)
			if activeID == "" {
				return fmt.Errorf("no active coordination session; run 'graft workon --as <name>' first")
			}

			if err := c.Heartbeat(activeID); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}

			if *jsonFlag {
				return outputJSON(map[string]string{
					"status":   "ok",
					"agent_id": activeID,
				})
			}

			fmt.Printf("Heartbeat updated for agent %s\n", activeID)
			return nil
		},
	}
}

// --- Sessions ---

func newCoordSessionsCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List active persistent sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, r, err := openCoordinator()
			if err != nil {
				return err
			}

			sessions, err := coord.ListSessions(r.GraftDir)
			if err != nil {
				return fmt.Errorf("list sessions: %w", err)
			}

			if *jsonFlag {
				return outputJSON(sessions)
			}

			if len(sessions) == 0 {
				fmt.Println("No active sessions.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "AGENT\tID\tMODE\tHOST\tPID\tLAST ACTIVE\tSTALE")
			for _, s := range sessions {
				stale := ""
				if coord.IsSessionStale(&s, coord.SessionStaleThreshold) {
					stale = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
					s.AgentName, s.AgentID, s.Mode, s.Host, s.PID,
					s.LastActive.Format("15:04:05"), stale)
			}
			return w.Flush()
		},
	}
}

// --- Presence ---

func newCoordPresenceCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "presence",
		Short: "Show who is reading what files",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			entries, err := c.ListPresence()
			if err != nil {
				return fmt.Errorf("list presence: %w", err)
			}

			if *jsonFlag {
				return outputJSON(entries)
			}

			if len(entries) == 0 {
				fmt.Println("No active readers.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "AGENT\tFILE\tENTITY\tSINCE")
			for _, e := range entries {
				entity := e.Entity
				if entity == "" {
					entity = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					e.AgentName, e.File, entity,
					e.Timestamp.Format("15:04:05"))
			}
			return w.Flush()
		},
	}
}

// --- Reading ---

func newCoordReadingCmd(jsonFlag *bool) *cobra.Command {
	var entity string

	cmd := &cobra.Command{
		Use:   "reading <file>",
		Short: "Register that you are reading a file",
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
			c.AgentID = activeID

			file := args[0]
			if err := c.RegisterPresence(file, entity); err != nil {
				return fmt.Errorf("register presence: %w", err)
			}

			if *jsonFlag {
				result := map[string]string{
					"status":   "reading",
					"file":     file,
					"agent_id": activeID,
				}
				if entity != "" {
					result["entity"] = entity
				}
				return outputJSON(result)
			}

			if entity != "" {
				fmt.Printf("Reading: %s (entity: %s)\n", file, entity)
			} else {
				fmt.Printf("Reading: %s\n", file)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&entity, "entity", "", "specific entity being read")
	return cmd
}

// Note: outputJSON is defined in cmd_workon.go and shared across the package.
