package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

func newWorkonCmd() *cobra.Command {
	var (
		name         string
		done         bool
		autoDiscover bool
		notifyMode   string
		conflictMode string
		watchOnly    bool
		scope        string
		jsonFlag     bool
	)

	cmd := &cobra.Command{
		Use:   "workon",
		Short: "Join or leave a coordination session",
		Long: `Start coordinating entity-level changes with other agents in this repository.

Use --as to register as a named agent. Use --done to deregister and release all claims.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !done && name == "" {
				return fmt.Errorf("either --as <name> or --done is required")
			}

			r, err := repo.Open(".")
			if err != nil {
				return fmt.Errorf("open repo: %w", err)
			}

			cfg := coord.DefaultConfig
			if conflictMode != "" {
				cfg.ConflictMode = conflictMode
			}
			c := coord.New(r, cfg)

			if done {
				return workonDone(c, r, jsonFlag)
			}

			return workonStart(c, r, name, autoDiscover, notifyMode, watchOnly, scope, jsonFlag)
		},
	}

	cmd.Flags().StringVar(&name, "as", "", "agent name")
	cmd.Flags().BoolVar(&done, "done", false, "leave coordination session")
	cmd.Flags().BoolVar(&autoDiscover, "auto-discover", false, "discover workspaces from go.mod")
	cmd.Flags().StringVar(&notifyMode, "notify", "all", "notification filter: all, breaking")
	cmd.Flags().StringVar(&conflictMode, "conflict-mode", "", "override conflict mode")
	cmd.Flags().BoolVar(&watchOnly, "watch-only", false, "observe only, don't claim")
	cmd.Flags().StringVar(&scope, "scope", "", "limit coordination to package pattern")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")

	return cmd
}

func workonStart(c *coord.Coordinator, r *repo.Repo, name string, autoDiscover bool, notifyMode string, watchOnly bool, scope string, jsonOutput bool) error {
	hostname, _ := os.Hostname()

	info := coord.AgentInfo{
		Name:      name,
		Workspace: filepath.Base(r.RootDir),
		Host:      hostname,
	}

	id, err := c.RegisterAgent(info)
	if err != nil {
		return fmt.Errorf("register agent: %w", err)
	}

	// Set up signal handler to deregister agent on SIGTERM/SIGINT
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		_ = c.DeregisterAgent(id)
		os.Exit(0)
	}()

	// Save the agent ID to .graft/coord/agent-id for later use by --done
	agentIDDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(agentIDDir, 0o755); err != nil {
		return fmt.Errorf("create coord dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(agentIDDir, "agent-id"), []byte(id), 0o644); err != nil {
		return fmt.Errorf("save agent ID: %w", err)
	}

	// Auto-discover workspaces
	var discovered map[string]string
	if autoDiscover {
		discovered, _ = coord.AutoDiscoverWorkspaces(r.RootDir)
		if len(discovered) > 0 {
			cfg, err := userconfig.Load()
			if err == nil {
				if cfg.Workspaces == nil {
					cfg.Workspaces = make(map[string]string)
				}
				for wsName, wsPath := range discovered {
					if _, exists := cfg.Workspaces[wsName]; !exists {
						cfg.Workspaces[wsName] = wsPath
					}
				}
				_ = userconfig.Save(cfg)
			}
		}
	}

	// Get current agent and peer state
	agents, _ := c.ListAgents()
	claims, _ := c.ListClaims()

	result := workonResult{
		Status:    "joined",
		AgentID:   id,
		AgentName: name,
		Workspace: info.Workspace,
		Agents:    len(agents),
		Claims:    len(claims),
		Mode:      "editing",
		Scope:     scope,
		Notify:    notifyMode,
	}
	if watchOnly {
		result.Mode = "watching"
	}
	if len(discovered) > 0 {
		result.Discovered = discovered
	}

	if jsonOutput {
		return outputJSON(result)
	}

	fmt.Printf("Coordination session started\n")
	fmt.Printf("  Agent:     %s (%s)\n", name, id)
	fmt.Printf("  Workspace: %s\n", info.Workspace)
	fmt.Printf("  Mode:      %s\n", result.Mode)
	if scope != "" {
		fmt.Printf("  Scope:     %s\n", scope)
	}
	fmt.Printf("  Peers:     %d agent(s) active\n", len(agents)-1)
	fmt.Printf("  Claims:    %d active\n", len(claims))
	if len(discovered) > 0 {
		fmt.Printf("  Discovered workspaces:\n")
		for wsName, wsPath := range discovered {
			fmt.Printf("    %s -> %s\n", wsName, wsPath)
		}
	}

	return nil
}

func workonDone(c *coord.Coordinator, r *repo.Repo, jsonOutput bool) error {
	// Read saved agent ID
	agentIDPath := filepath.Join(r.GraftDir, "coord", "agent-id")
	data, err := os.ReadFile(agentIDPath)
	if err != nil {
		return fmt.Errorf("no active coordination session (missing %s)", agentIDPath)
	}
	agentID := string(data)

	agent, err := c.GetAgent(agentID)
	if err != nil {
		// Agent already gone; clean up local state
		_ = os.Remove(agentIDPath)
		if jsonOutput {
			return outputJSON(workonResult{Status: "already_done"})
		}
		fmt.Println("No active coordination session found.")
		return nil
	}

	agentName := agent.Name

	if err := c.DeregisterAgent(agentID); err != nil {
		return fmt.Errorf("deregister agent: %w", err)
	}

	_ = os.Remove(agentIDPath)

	if jsonOutput {
		return outputJSON(workonResult{
			Status:    "left",
			AgentID:   agentID,
			AgentName: agentName,
		})
	}

	fmt.Printf("Coordination session ended for %s (%s)\n", agentName, agentID)
	fmt.Println("All claims released.")
	return nil
}

type workonResult struct {
	Status     string            `json:"status"`
	AgentID    string            `json:"agent_id,omitempty"`
	AgentName  string            `json:"agent_name,omitempty"`
	Workspace  string            `json:"workspace,omitempty"`
	Mode       string            `json:"mode,omitempty"`
	Scope      string            `json:"scope,omitempty"`
	Notify     string            `json:"notify,omitempty"`
	Agents     int               `json:"agents,omitempty"`
	Claims     int               `json:"claims,omitempty"`
	Discovered map[string]string `json:"discovered,omitempty"`
}

func outputJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
