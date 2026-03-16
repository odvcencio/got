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
				return workonDone(c, r, name, jsonFlag)
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

	mode := "editing"
	if watchOnly {
		mode = "watching"
	}

	// Check for existing persistent session
	var id string
	var resumed bool
	existingSession, _ := coord.LoadSession(r.GraftDir, name)
	if existingSession != nil && !coord.IsSessionStale(existingSession, coord.SessionStaleThreshold) {
		// Resume: reuse the existing agent ID, update PID/host
		id = existingSession.AgentID
		c.AgentID = id
		existingSession.PID = os.Getpid()
		existingSession.Host = hostname
		existingSession.Scope = scope
		existingSession.Mode = mode
		if err := coord.TouchSession(r.GraftDir, existingSession); err != nil {
			return fmt.Errorf("touch session: %w", err)
		}
		// Update the agent heartbeat in the ref store too
		_ = c.Heartbeat(id)
		resumed = true
	} else {
		// New session (or replacing a stale one)
		info := coord.AgentInfo{
			Name:      name,
			Workspace: filepath.Base(r.RootDir),
			Host:      hostname,
		}

		var err error
		id, err = c.RegisterAgent(info)
		if err != nil {
			return fmt.Errorf("register agent: %w", err)
		}

		// Write persistent session
		sess := &coord.Session{
			AgentID:    id,
			AgentName:  name,
			Workspace:  filepath.Base(r.RootDir),
			Host:       hostname,
			StartedAt:  c.AgentStartedAt(),
			LastActive: c.AgentStartedAt(),
			PID:        os.Getpid(),
			Scope:      scope,
			Mode:       mode,
		}
		if err := coord.SaveSession(r.GraftDir, sess); err != nil {
			return fmt.Errorf("save session: %w", err)
		}
	}

	// Set up signal handler to deregister agent on SIGTERM/SIGINT
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		_ = c.DeregisterAgent(id)
		_ = coord.RemoveSession(r.GraftDir, name)
		os.Exit(0)
	}()

	// Save the agent ID to .graft/coord/agent-{name} for later use by --done.
	// Per-agent files prevent concurrent agents from clobbering each other's session.
	agentIDDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(agentIDDir, 0o755); err != nil {
		return fmt.Errorf("create coord dir: %w", err)
	}
	agentFileName := "agent-" + name
	if err := os.WriteFile(filepath.Join(agentIDDir, agentFileName), []byte(id), 0o644); err != nil {
		return fmt.Errorf("save agent ID: %w", err)
	}
	// Also write a legacy agent-id for backwards compat with single-agent workflows
	_ = os.WriteFile(filepath.Join(agentIDDir, "agent-id"), []byte(id), 0o644)

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

	status := "joined"
	if resumed {
		status = "resumed"
	}
	result := workonResult{
		Status:    status,
		AgentID:   id,
		AgentName: name,
		Workspace: filepath.Base(r.RootDir),
		Agents:    len(agents),
		Claims:    len(claims),
		Mode:      mode,
		Scope:     scope,
		Notify:    notifyMode,
	}
	if len(discovered) > 0 {
		result.Discovered = discovered
	}

	if jsonOutput {
		return outputJSON(result)
	}

	if resumed {
		fmt.Printf("Coordination session resumed\n")
	} else {
		fmt.Printf("Coordination session started\n")
	}
	fmt.Printf("  Agent:     %s (%s)\n", name, id)
	fmt.Printf("  Workspace: %s\n", filepath.Base(r.RootDir))
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

func workonDone(c *coord.Coordinator, r *repo.Repo, name string, jsonOutput bool) error {
	coordDir := filepath.Join(r.GraftDir, "coord")

	var agentID string
	var agentIDPath string

	// If name is provided, look up the per-agent file directly
	if name != "" {
		agentFileName := "agent-" + name
		p := filepath.Join(coordDir, agentFileName)
		if data, err := os.ReadFile(p); err == nil {
			agentID = string(data)
			agentIDPath = p
		}
	}

	// Otherwise scan for any per-agent file
	if agentID == "" {
		entries, _ := os.ReadDir(coordDir)
		for _, e := range entries {
			if e.IsDir() || e.Name() == "agent-id" {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(coordDir, e.Name())); err == nil {
				agentID = string(data)
				agentIDPath = filepath.Join(coordDir, e.Name())
				break
			}
		}
	}

	// Fall back to legacy single agent-id file
	if agentID == "" {
		legacyPath := filepath.Join(coordDir, "agent-id")
		data, err := os.ReadFile(legacyPath)
		if err != nil {
			return fmt.Errorf("no active coordination session found")
		}
		agentID = string(data)
		agentIDPath = legacyPath
	}

	agent, err := c.GetAgent(agentID)
	if err != nil {
		// Agent already gone; clean up local state
		_ = os.Remove(agentIDPath)
		if agentIDPath != filepath.Join(coordDir, "agent-id") {
			_ = os.Remove(filepath.Join(coordDir, "agent-id"))
		}
		if jsonOutput {
			return outputJSON(workonResult{Status: "already_done"})
		}
		fmt.Println("No active coordination session found.")
		return nil
	}

	agentName := agent.Name

	// Ownership check: if the owning process is still alive, block release
	// from a different process. This prevents one agent from accidentally
	// deregistering another active agent. Dead processes are allowed to be
	// cleaned up by any caller.
	if name != "" {
		if sess, _ := coord.LoadSession(r.GraftDir, name); sess != nil && sess.PID != 0 {
			callerPID := os.Getpid()
			if sess.PID != callerPID && isProcessAlive(sess.PID) {
				return fmt.Errorf("cannot release agent %q: owned by active PID %d (caller is PID %d)", name, sess.PID, callerPID)
			}
		}
	}

	if err := c.DeregisterAgent(agentID); err != nil {
		return fmt.Errorf("deregister agent: %w", err)
	}

	// Remove persistent session file
	if agentName != "" {
		_ = coord.RemoveSession(r.GraftDir, agentName)
	}

	// Clear any presence entries for this agent
	_ = coord.ClearAgentPresence(r.GraftDir, agentID)

	_ = os.Remove(agentIDPath)
	// Also clean legacy file if this was the last agent
	_ = os.Remove(filepath.Join(coordDir, "agent-id"))

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

// isProcessAlive checks whether a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 tests process existence without sending a real signal.
	return proc.Signal(syscall.Signal(0)) == nil
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
