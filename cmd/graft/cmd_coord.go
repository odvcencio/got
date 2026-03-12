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

	cmd := &cobra.Command{
		Use:   "coord",
		Short: "Multi-agent coordination dashboard and tools",
		Long:  `View and manage entity-level coordination state: agents, claims, feed, and impact analysis.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return coordDashboard(jsonFlag)
		},
	}

	cmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "JSON output")

	cmd.AddCommand(newCoordAgentsCmd(&jsonFlag))
	cmd.AddCommand(newCoordClaimsCmd(&jsonFlag))
	cmd.AddCommand(newCoordFeedCmd(&jsonFlag))
	cmd.AddCommand(newCoordImpactCmd(&jsonFlag))
	cmd.AddCommand(newCoordCheckCmd(&jsonFlag))
	cmd.AddCommand(newCoordDiffCmd(&jsonFlag))
	cmd.AddCommand(newCoordXrefsCmd(&jsonFlag))
	cmd.AddCommand(newCoordGraphCmd(&jsonFlag))
	cmd.AddCommand(newCoordWatchCmd(&jsonFlag))
	cmd.AddCommand(newCoordUnwatchCmd(&jsonFlag))
	cmd.AddCommand(newCoordResolveCmd(&jsonFlag))

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
	data, err := os.ReadFile(filepath.Join(r.GraftDir, "coord", "agent-id"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// --- Dashboard ---

func coordDashboard(jsonOutput bool) error {
	c, r, err := openCoordinator()
	if err != nil {
		return err
	}

	agents, _ := c.ListAgents()
	claims, _ := c.ListClaims()
	feedEvents, _ := c.WalkFeed("", 100)

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

	activeID := readActiveAgentID(r)

	result := dashboardResult{
		Agents:    len(agents),
		Claims:    len(claims),
		Conflicts: conflictCount,
		FeedCount: len(feedEvents),
		ActiveID:  activeID,
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
	if activeID != "" {
		fmt.Printf("  Active as:  %s\n", activeID)
	} else {
		fmt.Printf("  Active as:  (not joined)\n")
	}

	return nil
}

type dashboardResult struct {
	Agents    int    `json:"agents"`
	Claims    int    `json:"claims"`
	Conflicts int    `json:"conflicts"`
	FeedCount int    `json:"feed_count"`
	ActiveID  string `json:"active_id,omitempty"`
}

// --- Agents ---

func newCoordAgentsCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "List registered agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			agents, err := c.ListAgents()
			if err != nil {
				return fmt.Errorf("list agents: %w", err)
			}

			if *jsonFlag {
				return outputJSON(agents)
			}

			if len(agents) == 0 {
				fmt.Println("No active agents.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tWORKSPACE\tHOST\tHEARTBEAT")
			for _, a := range agents {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					a.ID, a.Name, a.Workspace, a.Host,
					a.HeartbeatAt.Format("15:04:05"))
			}
			return w.Flush()
		},
	}
}

// --- Claims ---

func newCoordClaimsCmd(jsonFlag *bool) *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "claims",
		Short: "List active claims",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			claims, err := c.ListClaims()
			if err != nil {
				return fmt.Errorf("list claims: %w", err)
			}

			// Filter by workspace agent name prefix if requested
			if workspace != "" {
				var filtered []coord.ClaimInfo
				for _, cl := range claims {
					if strings.Contains(cl.AgentName, workspace) || strings.Contains(cl.File, workspace) {
						filtered = append(filtered, cl)
					}
				}
				claims = filtered
			}

			if *jsonFlag {
				return outputJSON(claims)
			}

			if len(claims) == 0 {
				fmt.Println("No active claims.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ENTITY\tFILE\tAGENT\tMODE\tSINCE")
			for _, cl := range claims {
				entityDisplay := cl.EntityKey
				if len(entityDisplay) > 60 {
					entityDisplay = entityDisplay[:57] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					entityDisplay, cl.File, cl.AgentName, cl.Mode,
					cl.ClaimedAt.Format("15:04:05"))
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "filter claims by workspace")
	return cmd
}

// --- Feed ---

func newCoordFeedCmd(jsonFlag *bool) *cobra.Command {
	var (
		since string
		mine  bool
	)

	cmd := &cobra.Command{
		Use:   "feed",
		Short: "Show coordination feed events",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinator()
			if err != nil {
				return err
			}

			events, err := c.WalkFeed(since, 50)
			if err != nil {
				return fmt.Errorf("walk feed: %w", err)
			}

			if mine {
				activeID := readActiveAgentID(r)
				if activeID != "" {
					var filtered []coord.FeedEvent
					for _, ev := range events {
						if ev.AgentID == activeID {
							filtered = append(filtered, ev)
						}
					}
					events = filtered
				}
			}

			if *jsonFlag {
				return outputJSON(events)
			}

			if len(events) == 0 {
				fmt.Println("No feed events.")
				return nil
			}

			for _, ev := range events {
				fmt.Printf("[%s] %s by %s", ev.FeedHash[:8], ev.Event, ev.AgentName)
				if ev.CommitHash != "" {
					fmt.Printf(" (commit %s)", ev.CommitHash[:8])
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
				EntityKey string `json:"entity_key"`
				File      string `json:"file"`
				HeldBy    string `json:"held_by"`
				Mode      string `json:"mode"`
			}

			var conflicts []conflict
			if activeID != "" {
				// Check if any of our agent's potential claims conflict
				for _, cl := range claims {
					if cl.Agent != activeID && cl.Mode == coord.ClaimEditing {
						conflicts = append(conflicts, conflict{
							EntityKey: cl.EntityKey,
							File:      cl.File,
							HeldBy:    cl.AgentName,
							Mode:      cl.Mode,
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
					fmt.Printf("  %s in %s (held by %s)\n", c.EntityKey, c.File, c.HeldBy)
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
	return &cobra.Command{
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
			err = c.AcquireClaim(activeID, coord.ClaimRequest{
				EntityKey: entityKey,
				File:      "",
				Mode:      coord.ClaimWatching,
			})
			if err != nil {
				return fmt.Errorf("watch: %w", err)
			}

			if *jsonFlag {
				return outputJSON(map[string]string{
					"status":     "watching",
					"entity_key": entityKey,
				})
			}

			fmt.Printf("Watching: %s\n", entityKey)
			return nil
		},
	}
}

// --- Unwatch ---

func newCoordUnwatchCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "unwatch <entity-key>",
		Short: "Remove a watch claim",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			entityKey := args[0]
			keyHash := coord.EntityKeyHash(entityKey)
			if err := c.ReleaseClaim(keyHash); err != nil {
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
						"status":     "transferred",
						"key_hash":   keyHash,
						"to_agent":   transfer,
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

// Note: outputJSON is defined in cmd_workon.go and shared across the package.
