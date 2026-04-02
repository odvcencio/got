package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/odvcencio/arbiter/overrides"
	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newCoorddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coordd",
		Short: "Local coordination daemon runtime",
	}

	cmd.AddCommand(newCoorddServeCmd())
	cmd.AddCommand(newCoorddTailCmd())
	cmd.AddCommand(newCoorddSnapshotCmd())
	cmd.AddCommand(newCoorddPreflightCmd())
	cmd.AddCommand(newCoorddExecCmd())
	cmd.AddCommand(newCoorddSpawnCmd())
	cmd.AddCommand(newCoorddSpawnShowCmd())
	cmd.AddCommand(newCoorddSpawnTraceCmd())
	cmd.AddCommand(newCoorddSpawnConsumeCmd())
	cmd.AddCommand(newCoorddSpawnAttachCmd())
	cmd.AddCommand(newCoorddSpawnHeartbeatCmd())
	cmd.AddCommand(newCoorddSpawnFinishCmd())
	cmd.AddCommand(newCoorddSpawnWaitCmd())
	cmd.AddCommand(newCoorddSpawnsCmd())
	cmd.AddCommand(newCoorddGuardCmd())
	return cmd
}

func openCoorddRuntime() (*repo.Repo, *coord.Coordinator, error) {
	r, err := repo.Open(".")
	if err != nil {
		return nil, nil, fmt.Errorf("open repo: %w", err)
	}
	c := coord.New(r, coord.DefaultConfig)
	return r, c, nil
}

func newCoorddServeCmd() *cobra.Command {
	var interval time.Duration
	var once bool
	var print bool
	var snapshotLimit int

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the local coordination daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, c, err := openCoorddRuntime()
			if err != nil {
				return err
			}

			opts := coordd.Options{
				Interval:          interval,
				SnapshotFileLimit: snapshotLimit,
			}
			if print {
				opts.PrintWriter = cmd.OutOrStdout()
			}
			d := coordd.New(r, c, opts)

			if once {
				_, err := d.RunOnce()
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return d.Run(ctx)
		},
	}

	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "polling interval for local coord runtime")
	cmd.Flags().BoolVar(&once, "once", false, "run one coordd poll cycle and exit")
	cmd.Flags().BoolVar(&print, "print", false, "print emitted events as JSON lines")
	cmd.Flags().IntVar(&snapshotLimit, "snapshot-limit", 256, "maximum number of files with stored contents per snapshot")
	return cmd
}

func newCoorddTailCmd() *cobra.Command {
	var limit int
	var follow bool
	var jsonFlag bool
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Read coordd local event journal",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}

			printed := map[string]bool{}
			printBatch := func(events []coordd.Event) error {
				if jsonFlag && !follow {
					return outputJSON(events)
				}
				for _, event := range events {
					if printed[event.ID] {
						continue
					}
					printed[event.ID] = true
					if jsonFlag {
						if err := json.NewEncoder(cmd.OutOrStdout()).Encode(event); err != nil {
							return err
						}
						continue
					}
					printCoorddEvent(cmd.OutOrStdout(), event)
				}
				return nil
			}

			events, err := coordd.ListEvents(r.GraftDir, limit)
			if err != nil {
				return err
			}
			if err := printBatch(events); err != nil {
				return err
			}
			if !follow {
				return nil
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					events, err := coordd.ListEvents(r.GraftDir, 0)
					if err != nil {
						return err
					}
					if err := printBatch(events); err != nil {
						return err
					}
				}
			}
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of historical events to show")
	cmd.Flags().BoolVar(&follow, "follow", false, "follow the coordd event journal")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output (JSON lines in follow mode)")
	cmd.Flags().DurationVar(&interval, "interval", time.Second, "polling interval while following")
	return cmd
}

func newCoorddSnapshotCmd() *cobra.Command {
	var jsonFlag bool
	var snapshotLimit int

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Capture a local WIP snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}

			activeID := readActiveAgentID(r)
			statusEntries, err := r.Status()
			if err != nil {
				return fmt.Errorf("status: %w", err)
			}
			snapshot, err := coordd.CaptureSnapshot(r, activeID, statusEntries, snapshotLimit)
			if err != nil {
				return err
			}
			if snapshot == nil {
				if jsonFlag {
					return outputJSON(map[string]any{"status": "clean"})
				}
				fmt.Fprintln(cmd.OutOrStdout(), "No worktree changes to snapshot.")
				return nil
			}

			if jsonFlag {
				return outputJSON(snapshot)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Snapshot %s captured with %d changed file(s)\n", snapshot.ID, snapshot.Summary.Changed)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	cmd.Flags().IntVar(&snapshotLimit, "snapshot-limit", 256, "maximum number of files with stored contents per snapshot")
	return cmd
}

func newCoorddPreflightCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "preflight -- <command...>",
		Short: "Evaluate whether a local action is allowed before execution",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("requires a command to inspect")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var r *repo.Repo
			if opened, _, err := openCoorddRuntime(); err == nil {
				r = opened
			}

			activeID := ""
			if r != nil {
				activeID = readActiveAgentID(r)
			}
			input, err := coordd.BuildShellActionInput(r, activeID, args)
			if err != nil {
				return err
			}
			decision, err := coordd.EvaluateActionPolicyWithRepo(r, input)
			if err != nil {
				return err
			}
			if r != nil {
				_ = coordd.RecordPreflightDecision(r.GraftDir, input, decision)
			}

			result := map[string]any{
				"input":    input,
				"decision": decision,
			}
			if jsonFlag {
				return outputJSON(result)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", decision.Action, decision.Reason)
			fmt.Fprintf(cmd.OutOrStdout(), "selector: %s\n", input.Action.Selector)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddExecCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "exec -- <command...>",
		Short: "Run a command through coordd preflight policy",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("requires a command to execute")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var r *repo.Repo
			if opened, _, err := openCoorddRuntime(); err == nil {
				r = opened
			}

			activeID := ""
			if r != nil {
				activeID = readActiveAgentID(r)
			}
			input, err := coordd.BuildShellActionInput(r, activeID, args)
			if err != nil {
				return err
			}
			decision, err := coordd.EvaluateActionPolicyWithRepo(r, input)
			if err != nil {
				return err
			}
			if r != nil {
				_ = coordd.RecordPreflightDecision(r.GraftDir, input, decision)
			}

			if decision.Action == "Advisory" {
				fmt.Fprintf(cmd.ErrOrStderr(), "coordd advisory: %s\n", decision.Reason)
			}

			execIO := coordd.ExecIO{
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			}
			if jsonFlag {
				execIO.Stdout = cmd.ErrOrStderr()
			}
			result, err := coordd.ExecuteGuardedWithIO(r, input, decision, execIO)
			if jsonFlag {
				if result == nil {
					result = &coordd.ExecResult{Decision: decision}
				}
				if outputErr := outputJSON(result); outputErr != nil {
					return outputErr
				}
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddSpawnCmd() *cobra.Command {
	var name string
	var profile string
	var runtimeName string
	var launchMode string
	var bootstrapCoord bool
	var taskID string
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "spawn --name <child> [--profile <profile>] [--runtime <auto|detached|container>] [--launch <detached|lease>] [--bootstrap-coord] -- <command...>",
		Short: "Start or authorize a governed child workstream",
		Args: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required")
			}
			if len(args) == 0 {
				return fmt.Errorf("requires a child command to execute")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}

			req := coordd.SpawnRequest{
				Name:             name,
				Command:          args,
				RequestedProfile: profile,
				Runtime:          runtimeName,
				Launch:           launchMode,
				BootstrapCoord:   bootstrapCoord,
				TaskID:           taskID,
			}

			var (
				result   *coordd.SpawnResult
				spawnErr error
			)
			if launchMode == "lease" {
				result, spawnErr = coordd.AuthorizeSpawn(r, readActiveAgentID(r), req)
			} else {
				result, spawnErr = coordd.SpawnDetached(r, readActiveAgentID(r), req)
			}
			if jsonFlag {
				if outputErr := outputJSON(result); outputErr != nil {
					return outputErr
				}
			}
			if !jsonFlag && result != nil && result.Record != nil {
				printCoorddSpawn(cmd.OutOrStdout(), result.Record)
				if result.SpawnDecision != nil && result.SpawnDecision.Action == "Advisory" {
					fmt.Fprintf(cmd.ErrOrStderr(), "coordd advisory: %s\n", result.SpawnDecision.Reason)
				}
			}
			return spawnErr
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "child agent/workstream name")
	cmd.Flags().StringVar(&profile, "profile", "", "requested child runtime profile")
	cmd.Flags().StringVar(&runtimeName, "runtime", "auto", "spawn runtime: auto, detached, or container")
	cmd.Flags().StringVar(&launchMode, "launch", "detached", "spawn delivery: detached or lease")
	cmd.Flags().BoolVar(&bootstrapCoord, "bootstrap-coord", false, "auto-register a coord agent/session for the child lease or launch")
	cmd.Flags().StringVar(&taskID, "task", "", "optional coord task id to bind to this child workstream")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddSpawnHeartbeatCmd() *cobra.Command {
	var id string
	var childAgentID string
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "spawn-heartbeat --id <spawn-id>",
		Short: "Record heartbeat for a leased child workstream",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("--id is required")
			}
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			record, err := coordd.TouchSpawn(r.GraftDir, id, childAgentID)
			if err != nil {
				return err
			}
			if jsonFlag {
				return outputJSON(record)
			}
			printCoorddSpawn(cmd.OutOrStdout(), record)
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "spawn record id")
	cmd.Flags().StringVar(&childAgentID, "child-agent-id", "", "optional in-process child agent id")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddSpawnShowCmd() *cobra.Command {
	var id string
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "spawn-show --id <spawn-id>",
		Short: "Show a child workstream record and lease package",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("--id is required")
			}
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			view, err := coordd.LoadSpawnView(r.GraftDir, id)
			if err != nil {
				return err
			}
			if view == nil || view.Record == nil {
				return fmt.Errorf("spawn %q not found", id)
			}
			if jsonFlag {
				return outputJSON(view)
			}
			printCoorddSpawn(cmd.OutOrStdout(), view.Record)
			if view.Lease != nil && len(view.Lease.Env) > 0 {
				keys := make([]string, 0, len(view.Lease.Env))
				for key := range view.Lease.Env {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s=%s\n", key, view.Lease.Env[key])
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "spawn record id")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddSpawnTraceCmd() *cobra.Command {
	var id string
	var execLimit int
	var eventLimit int
	var view string
	var matchedOnly bool
	var collapseHeartbeats bool
	var phases []string
	var noDefaultFallbacks bool
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "spawn-trace --id <spawn-id>",
		Short: "Show the unified trace bundle for a child workstream",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("--id is required")
			}
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			trace, err := coordd.LoadSpawnTrace(r.GraftDir, id, execLimit, eventLimit)
			if err != nil {
				return err
			}
			if trace == nil || trace.Record == nil {
				return fmt.Errorf("spawn %q not found", id)
			}
			switch strings.TrimSpace(view) {
			case "", "summary":
				view = "summary"
			case "raw":
			default:
				return fmt.Errorf("invalid --view %q", view)
			}
			if jsonFlag {
				if view == "raw" {
					return outputJSON(trace)
				}
				return outputJSON(coordd.BuildSpawnTraceView(trace, coordd.SpawnTraceViewOptions{
					MatchedOnly:        matchedOnly,
					CollapseHeartbeats: collapseHeartbeats,
					Phases:             phases,
					NoDefaultFallbacks: noDefaultFallbacks,
				}))
			}
			if view == "raw" {
				printCoorddSpawn(cmd.OutOrStdout(), trace.Record)
				if trace.Lease != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "lease_env=%d execs=%d events=%d\n", len(trace.Lease.Env), len(trace.Execs), len(trace.Events))
				}
				for _, execTrace := range trace.Execs {
					decision := ""
					rule := ""
					if execTrace.Result != nil && execTrace.Result.Decision != nil {
						decision = execTrace.Result.Decision.Action
						rule = execTrace.Result.Decision.Rule
					}
					fmt.Fprintf(cmd.OutOrStdout(), "exec[%s] action=%s rule=%s exit=%d selector=%s\n", execTrace.ID, decision, rule, execExitCode(execTrace.Result), execTrace.Input.Action.Selector)
				}
				for _, event := range trace.Events {
					printCoorddEvent(cmd.OutOrStdout(), event)
				}
				return nil
			}
			renderSpawnTraceSummary(cmd.OutOrStdout(), coordd.BuildSpawnTraceView(trace, coordd.SpawnTraceViewOptions{
				MatchedOnly:        matchedOnly,
				CollapseHeartbeats: collapseHeartbeats,
				Phases:             phases,
				NoDefaultFallbacks: noDefaultFallbacks,
			}))
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "spawn record id")
	cmd.Flags().IntVar(&execLimit, "execs", 20, "maximum exec traces to include (0 = all)")
	cmd.Flags().IntVar(&eventLimit, "events", 50, "maximum events to include (0 = all)")
	cmd.Flags().StringVar(&view, "view", "summary", "trace view: summary or raw")
	cmd.Flags().BoolVar(&matchedOnly, "matched-only", true, "show only matched rules in summary view")
	cmd.Flags().BoolVar(&collapseHeartbeats, "collapse-heartbeats", true, "collapse consecutive heartbeat events in summary view")
	cmd.Flags().StringSliceVar(&phases, "phase", nil, "limit summary view to one or more phases: authorization, activation, execution, completion, other")
	cmd.Flags().BoolVar(&noDefaultFallbacks, "no-default-fallbacks", false, "hide default or fallback rules in summary view")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddSpawnConsumeCmd() *cobra.Command {
	var id string
	var childAgentID string
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "spawn-consume --id <spawn-id>",
		Short: "Mark a leased child active and return its lease bootstrap package",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("--id is required")
			}
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			view, err := coordd.ConsumeSpawn(r.GraftDir, id, childAgentID)
			if err != nil {
				return err
			}
			if jsonFlag {
				return outputJSON(view)
			}
			if view == nil || view.Record == nil {
				return fmt.Errorf("spawn %q not found", id)
			}
			printCoorddSpawn(cmd.OutOrStdout(), view.Record)
			if view.Lease != nil && len(view.Lease.Env) > 0 {
				keys := make([]string, 0, len(view.Lease.Env))
				for key := range view.Lease.Env {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s=%s\n", key, view.Lease.Env[key])
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "spawn record id")
	cmd.Flags().StringVar(&childAgentID, "child-agent-id", "", "optional in-process child agent id")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddSpawnAttachCmd() *cobra.Command {
	var id string
	var heartbeat time.Duration
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "spawn-attach --id <spawn-id>",
		Short: "Launch a leased child command with automatic heartbeat and finish handling",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("--id is required")
			}
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			execIO := coordd.ExecIO{
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			}
			if jsonFlag {
				execIO.Stdout = cmd.ErrOrStderr()
			}
			record, err := coordd.AttachSpawn(r, id, heartbeat, execIO)
			if jsonFlag && record != nil {
				if outputErr := outputJSON(record); outputErr != nil {
					return outputErr
				}
			}
			if !jsonFlag && record != nil {
				printCoorddSpawn(cmd.OutOrStdout(), record)
			}
			return err
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "spawn record id")
	cmd.Flags().DurationVar(&heartbeat, "heartbeat", 5*time.Second, "heartbeat interval while attached child is running")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddSpawnFinishCmd() *cobra.Command {
	var id string
	var status string
	var childAgentID string
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "spawn-finish --id <spawn-id>",
		Short: "Finish a leased or detached child workstream record",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("--id is required")
			}
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			record, err := coordd.FinishSpawn(r.GraftDir, id, status, childAgentID)
			if err != nil {
				return err
			}
			if jsonFlag {
				return outputJSON(record)
			}
			printCoorddSpawn(cmd.OutOrStdout(), record)
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "spawn record id")
	cmd.Flags().StringVar(&status, "status", "completed", "final status: completed, failed, aborted, blocked")
	cmd.Flags().StringVar(&childAgentID, "child-agent-id", "", "optional in-process child agent id")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddSpawnWaitCmd() *cobra.Command {
	var id string
	var timeout time.Duration
	var poll time.Duration
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "spawn-wait --id <spawn-id>",
		Short: "Wait for a child workstream to reach a terminal status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("--id is required")
			}
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			record, err := coordd.WaitSpawn(r.GraftDir, id, timeout, poll)
			if jsonFlag && record != nil {
				if outputErr := outputJSON(record); outputErr != nil {
					return outputErr
				}
			}
			if !jsonFlag && record != nil {
				printCoorddSpawn(cmd.OutOrStdout(), record)
			}
			return err
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "spawn record id")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "maximum time to wait")
	cmd.Flags().DurationVar(&poll, "poll", 200*time.Millisecond, "poll interval")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddSpawnsCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "spawns",
		Short: "List coordd child workstreams",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}

			records, err := coordd.ListSpawnRecords(r.GraftDir)
			if err != nil {
				return err
			}
			if jsonFlag {
				return outputJSON(records)
			}
			if len(records) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No coordd spawns.")
				return nil
			}
			for _, record := range records {
				printCoorddSpawn(cmd.OutOrStdout(), &record)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddGuardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guard",
		Short: "Inspect and update local coordd guard configuration",
	}
	cmd.AddCommand(newCoorddGuardShowCmd())
	cmd.AddCommand(newCoorddGuardModeCmd())
	cmd.AddCommand(newCoorddGuardAllowCmd())
	cmd.AddCommand(newCoorddGuardBackendCmd())
	cmd.AddCommand(newCoorddGuardRuntimeCmd())
	cmd.AddCommand(newCoorddGuardImageCmd())
	cmd.AddCommand(newCoorddGuardOverrideCmd())
	return cmd
}

func newCoorddGuardShowCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show local coordd guard configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			cfg, err := coordd.LoadGuardConfig(r.GraftDir)
			if err != nil {
				return err
			}
			overrideStore, err := coordd.LoadGuardOverrideStore(r.GraftDir)
			if err != nil {
				return err
			}
			actionBundle, err := coordd.LoadActionPolicyBundleInfo(r)
			if err != nil {
				return err
			}
			spawnBundle, err := coordd.LoadSpawnPolicyBundleInfo(r)
			if err != nil {
				return err
			}
			if jsonFlag {
				return outputJSON(map[string]any{
					"config":     cfg,
					"overrides":  overrideListFromSnapshot(overrideStore.Snapshot()),
					"bundle_ids": map[string]string{"action": "coordd/action", "spawn": "coordd/spawn"},
					"policies": map[string]coordd.PolicyBundleInfo{
						"action": actionBundle,
						"spawn":  spawnBundle,
					},
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "mode: %s\n", cfg.Mode)
			fmt.Fprintf(cmd.OutOrStdout(), "require_active_agent: %t\n", cfg.RequireActiveAgent)
			fmt.Fprintf(cmd.OutOrStdout(), "preferred_backend: %s\n", cfg.PreferredBackend)
			fmt.Fprintf(cmd.OutOrStdout(), "container_runtime: %s\n", cfg.ContainerRuntime)
			fmt.Fprintf(cmd.OutOrStdout(), "container_image: %s\n", cfg.ContainerImage)
			fmt.Fprintf(cmd.OutOrStdout(), "override_store: %s\n", coordd.GuardOverridesPath(r.GraftDir))
			fmt.Fprintf(cmd.OutOrStdout(), "rule_overrides: %d\n", len(overrideListFromSnapshot(overrideStore.Snapshot())))
			fmt.Fprintf(cmd.OutOrStdout(), "action_policy: %s\n", formatPolicyBundleSummary(actionBundle))
			fmt.Fprintf(cmd.OutOrStdout(), "spawn_policy: %s\n", formatPolicyBundleSummary(spawnBundle))
			if len(cfg.AllowedActions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "allowed_actions: []")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "allowed_actions:")
			for _, pattern := range cfg.AllowedActions {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", pattern)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddGuardModeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mode <off|advisory|enforce>",
		Short: "Set the local coordd guard mode",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := strings.TrimSpace(args[0])
			switch mode {
			case "off", "advisory", "enforce":
			default:
				return fmt.Errorf("invalid mode %q", mode)
			}

			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			cfg, err := coordd.LoadGuardConfig(r.GraftDir)
			if err != nil {
				return err
			}
			cfg.Mode = mode
			if err := coordd.SaveGuardConfig(r.GraftDir, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "coordd guard mode set to %s\n", mode)
			return nil
		},
	}
	return cmd
}

func newCoorddGuardAllowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allow <pattern>",
		Short: "Add a local coordd allowlist pattern",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pattern := strings.TrimSpace(args[0])
			if pattern == "" {
				return fmt.Errorf("allow pattern cannot be empty")
			}
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			cfg, err := coordd.LoadGuardConfig(r.GraftDir)
			if err != nil {
				return err
			}
			for _, existing := range cfg.AllowedActions {
				if existing == pattern {
					fmt.Fprintf(cmd.OutOrStdout(), "coordd guard already allows %s\n", pattern)
					return nil
				}
			}
			cfg.AllowedActions = append(cfg.AllowedActions, pattern)
			if err := coordd.SaveGuardConfig(r.GraftDir, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "coordd guard now allows %s\n", pattern)
			return nil
		},
	}
	return cmd
}

func newCoorddGuardBackendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backend <auto|host-direct|host-bwrap|container>",
		Short: "Set the preferred coordd execution backend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backend := strings.TrimSpace(args[0])
			switch backend {
			case "auto", "host-direct", "host-bwrap", "container":
			default:
				return fmt.Errorf("invalid backend %q", backend)
			}

			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			cfg, err := coordd.LoadGuardConfig(r.GraftDir)
			if err != nil {
				return err
			}
			cfg.PreferredBackend = backend
			if err := coordd.SaveGuardConfig(r.GraftDir, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "coordd preferred backend set to %s\n", backend)
			return nil
		},
	}
	return cmd
}

func newCoorddGuardRuntimeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runtime <auto|podman|docker>",
		Short: "Set the preferred coordd container runtime",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeName := strings.TrimSpace(args[0])
			switch runtimeName {
			case "auto", "podman", "docker":
			default:
				return fmt.Errorf("invalid runtime %q", runtimeName)
			}

			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			cfg, err := coordd.LoadGuardConfig(r.GraftDir)
			if err != nil {
				return err
			}
			cfg.ContainerRuntime = runtimeName
			if err := coordd.SaveGuardConfig(r.GraftDir, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "coordd container runtime set to %s\n", runtimeName)
			return nil
		},
	}
	return cmd
}

func newCoorddGuardImageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "image <image-ref>",
		Short: "Set the coordd container image",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			image := strings.TrimSpace(args[0])
			if image == "" {
				return fmt.Errorf("container image cannot be empty")
			}

			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			cfg, err := coordd.LoadGuardConfig(r.GraftDir)
			if err != nil {
				return err
			}
			cfg.ContainerImage = image
			if err := coordd.SaveGuardConfig(r.GraftDir, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "coordd container image set to %s\n", image)
			return nil
		},
	}
	return cmd
}

func newCoorddGuardOverrideCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "override",
		Short: "Manage Arbiter runtime overrides for coordd policies",
	}
	cmd.AddCommand(newCoorddGuardOverrideListCmd())
	cmd.AddCommand(newCoorddGuardOverrideSetCmd())
	cmd.AddCommand(newCoorddGuardOverrideClearCmd())
	return cmd
}

func newCoorddGuardOverrideListCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List coordd policy rule overrides",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			store, err := coordd.LoadGuardOverrideStore(r.GraftDir)
			if err != nil {
				return err
			}
			entries := overrideListFromSnapshot(store.Snapshot())
			if jsonFlag {
				return outputJSON(entries)
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No coordd policy overrides.")
				return nil
			}
			for _, entry := range entries {
				fmt.Fprintf(cmd.OutOrStdout(), "%s/%s", entry.Policy, entry.Rule)
				if entry.KillSwitch != nil {
					fmt.Fprintf(cmd.OutOrStdout(), " kill_switch=%t", *entry.KillSwitch)
				}
				if entry.Rollout != nil {
					fmt.Fprintf(cmd.OutOrStdout(), " rollout=%d", *entry.Rollout)
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}

func newCoorddGuardOverrideSetCmd() *cobra.Command {
	var (
		killSwitch bool
		rollout    int
	)

	cmd := &cobra.Command{
		Use:   "set <action|spawn> <rule>",
		Short: "Set a coordd policy rule override",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			bundleID, policyName, err := coorddOverrideBundle(args[0])
			if err != nil {
				return err
			}
			rule := strings.TrimSpace(args[1])
			if rule == "" {
				return fmt.Errorf("rule name cannot be empty")
			}
			if !cmd.Flags().Changed("kill-switch") && !cmd.Flags().Changed("rollout") {
				return fmt.Errorf("set at least one of --kill-switch or --rollout")
			}

			ov := overrides.RuleOverride{}
			if cmd.Flags().Changed("kill-switch") {
				ov.KillSwitch = &killSwitch
			}
			if cmd.Flags().Changed("rollout") {
				if rollout < 0 || rollout > 100 {
					return fmt.Errorf("rollout must be between 0 and 100")
				}
				value := uint16(rollout)
				ov.Rollout = &value
			}

			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			store, err := coordd.LoadGuardOverrideStore(r.GraftDir)
			if err != nil {
				return err
			}
			if err := store.SetRule(bundleID, rule, ov); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "coordd override set for %s/%s\n", policyName, rule)
			return nil
		},
	}

	cmd.Flags().BoolVar(&killSwitch, "kill-switch", false, "set the rule kill switch")
	cmd.Flags().IntVar(&rollout, "rollout", -1, "set the rule rollout percentage")
	return cmd
}

func newCoorddGuardOverrideClearCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear <action|spawn> <rule>",
		Short: "Clear a coordd policy rule override",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			bundleID, policyName, err := coorddOverrideBundle(args[0])
			if err != nil {
				return err
			}
			rule := strings.TrimSpace(args[1])
			if rule == "" {
				return fmt.Errorf("rule name cannot be empty")
			}
			r, _, err := openCoorddRuntime()
			if err != nil {
				return err
			}
			store, err := coordd.LoadGuardOverrideStore(r.GraftDir)
			if err != nil {
				return err
			}
			if err := store.SetRule(bundleID, rule, overrides.RuleOverride{}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "coordd override cleared for %s/%s\n", policyName, rule)
			return nil
		},
	}
	return cmd
}

type coorddRuleOverrideView struct {
	Policy     string  `json:"policy"`
	BundleID   string  `json:"bundle_id"`
	Rule       string  `json:"rule"`
	KillSwitch *bool   `json:"kill_switch,omitempty"`
	Rollout    *uint16 `json:"rollout,omitempty"`
}

func overrideListFromSnapshot(snapshot overrides.Snapshot) []coorddRuleOverrideView {
	var entries []coorddRuleOverrideView
	for bundleID, rules := range snapshot.Rules {
		policyName := coorddOverridePolicyName(bundleID)
		for rule, ov := range rules {
			entry := coorddRuleOverrideView{
				Policy:     policyName,
				BundleID:   bundleID,
				Rule:       rule,
				KillSwitch: ov.KillSwitch,
				Rollout:    ov.Rollout,
			}
			entries = append(entries, entry)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Policy == entries[j].Policy {
			return entries[i].Rule < entries[j].Rule
		}
		return entries[i].Policy < entries[j].Policy
	})
	return entries
}

func coorddOverrideBundle(name string) (bundleID string, policyName string, err error) {
	switch strings.TrimSpace(name) {
	case "action":
		return "coordd/action", "action", nil
	case "spawn":
		return "coordd/spawn", "spawn", nil
	default:
		return "", "", fmt.Errorf("invalid policy %q", name)
	}
}

func coorddOverridePolicyName(bundleID string) string {
	switch bundleID {
	case "coordd/action":
		return "action"
	case "coordd/spawn":
		return "spawn"
	default:
		return bundleID
	}
}

func printCoorddEvent(w io.Writer, event coordd.Event) {
	fmt.Fprintf(w, "[%s] %s", event.Timestamp.Format("2006-01-02 15:04:05"), event.Type)
	if event.AgentID != "" {
		fmt.Fprintf(w, " agent=%s", event.AgentID)
	}
	if len(event.Data) > 0 {
		fmt.Fprintf(w, " %v", event.Data)
	}
	fmt.Fprintln(w)
}

func printCoorddSpawn(w io.Writer, record *coordd.SpawnRecord) {
	if record == nil {
		return
	}
	fmt.Fprintf(w, "[%s] %s", record.StartedAt.Format("2006-01-02 15:04:05"), record.Name)
	fmt.Fprintf(w, " id=%s", record.ID)
	fmt.Fprintf(w, " status=%s", record.Status)
	if record.LaunchMode != "" {
		fmt.Fprintf(w, " launch=%s", record.LaunchMode)
	}
	if record.Backend != "" {
		fmt.Fprintf(w, " backend=%s", record.Backend)
	}
	if record.RequestedRuntime != "" {
		fmt.Fprintf(w, " runtime=%s", record.RequestedRuntime)
	}
	if record.RequestedProfile.Name != "" {
		fmt.Fprintf(w, " requested=%s", record.RequestedProfile.Name)
	}
	if record.EffectiveProfile.Name != "" {
		fmt.Fprintf(w, " effective=%s", record.EffectiveProfile.Name)
	}
	if record.PID != 0 {
		fmt.Fprintf(w, " pid=%d", record.PID)
	}
	if record.ContainerID != "" {
		fmt.Fprintf(w, " container=%s", record.ContainerID)
	}
	if record.StdoutPath != "" {
		fmt.Fprintf(w, " stdout=%s", record.StdoutPath)
	}
	if record.StderrPath != "" {
		fmt.Fprintf(w, " stderr=%s", record.StderrPath)
	}
	if record.ParentAgentID != "" {
		fmt.Fprintf(w, " parent=%s", record.ParentAgentID)
	}
	if record.ChildAgentID != "" {
		fmt.Fprintf(w, " child=%s", record.ChildAgentID)
	}
	if record.ChildAgentName != "" {
		fmt.Fprintf(w, " child_name=%s", record.ChildAgentName)
	}
	if record.Task != nil && record.Task.ID != "" {
		fmt.Fprintf(w, " task=%s", record.Task.ID)
		if record.Task.Status != "" {
			fmt.Fprintf(w, " task_status=%s", record.Task.Status)
		}
		if record.Task.AssignedTo != "" {
			fmt.Fprintf(w, " task_assignee=%s", record.Task.AssignedTo)
		}
	}
	fmt.Fprintln(w)
}

func execExitCode(result *coordd.ExecResult) int {
	if result == nil {
		return 0
	}
	return result.ExitCode
}

func renderSpawnTraceSummary(w io.Writer, trace *coordd.SpawnTraceView) {
	if trace == nil || trace.Record == nil {
		return
	}
	printCoorddSpawn(w, trace.Record)
	fmt.Fprintf(w, "raw_events=%d rendered_events=%d", trace.RawEventCount, trace.RenderedEventCount)
	if trace.CollapsedHeartbeats > 0 {
		fmt.Fprintf(w, " collapsed_heartbeats=%d", trace.CollapsedHeartbeats)
	}
	fmt.Fprintln(w)
	if trace.SpawnAction != nil {
		renderTraceDecisionSummary(w, "spawn_action", trace.SpawnAction)
	}
	if trace.SpawnPolicy != nil {
		renderTraceDecisionSummary(w, "spawn_policy", trace.SpawnPolicy)
	}
	for _, execTrace := range trace.Execs {
		action := ""
		rule := ""
		profile := ""
		if execTrace.Decision != nil {
			action = execTrace.Decision.Action
			rule = execTrace.Decision.Rule
			profile = execTrace.Decision.Profile
		}
		fmt.Fprintf(w, "exec[%s] %s action=%s rule=%s profile=%s exit=%d backend=%s\n", execTrace.ID, execTrace.Selector, action, rule, profile, execTrace.ExitCode, execTrace.Backend)
		if execTrace.Decision != nil && len(execTrace.Decision.Rules) > 0 {
			fmt.Fprintf(w, "  matched=%s\n", joinTraceRuleNames(execTrace.Decision.Rules))
		}
	}
	for _, phase := range trace.Phases {
		fmt.Fprintf(w, "%s:\n", phase.Name)
		for _, event := range phase.Events {
			fmt.Fprintf(w, "  %s", event.Type)
			if event.Count > 1 {
				fmt.Fprintf(w, " x%d", event.Count)
			}
			if event.AgentID != "" {
				fmt.Fprintf(w, " agent=%s", event.AgentID)
			}
			if event.Status != "" {
				fmt.Fprintf(w, " status=%s", event.Status)
			}
			if event.Decision != "" {
				fmt.Fprintf(w, " decision=%s", event.Decision)
			}
			if event.Rule != "" {
				fmt.Fprintf(w, " rule=%s", event.Rule)
			}
			if event.Profile != "" {
				fmt.Fprintf(w, " profile=%s", event.Profile)
			}
			if event.Backend != "" {
				fmt.Fprintf(w, " backend=%s", event.Backend)
			}
			if event.ExitCode != nil {
				fmt.Fprintf(w, " exit=%d", *event.ExitCode)
			}
			if event.Task != nil && event.Task.ID != "" {
				fmt.Fprintf(w, " task=%s", event.Task.ID)
				if event.Task.Status != "" {
					fmt.Fprintf(w, " task_status=%s", event.Task.Status)
				}
			}
			if !event.FirstAt.IsZero() {
				fmt.Fprintf(w, " at=%s", event.FirstAt.Format("15:04:05"))
				if !event.LastAt.IsZero() && !event.LastAt.Equal(event.FirstAt) {
					fmt.Fprintf(w, "..%s", event.LastAt.Format("15:04:05"))
				}
			}
			fmt.Fprintln(w)
		}
	}
}

func renderTraceDecisionSummary(w io.Writer, label string, decision *coordd.TraceDecisionView) {
	if decision == nil {
		return
	}
	fmt.Fprintf(w, "%s action=%s rule=%s profile=%s\n", label, decision.Action, decision.Rule, decision.Profile)
	if decision.Bundle.Root != "" || decision.Bundle.Embedded {
		fmt.Fprintf(w, "  bundle=%s\n", formatPolicyBundleSummary(decision.Bundle))
	}
	if decision.RuleOrigin != nil {
		fmt.Fprintf(w, "  rule_origin=%s\n", formatPolicyOrigin(decision.RuleOrigin))
	}
	if decision.Reason != "" {
		fmt.Fprintf(w, "  reason=%s\n", decision.Reason)
	}
	if len(decision.Rules) > 0 {
		fmt.Fprintf(w, "  matched=%s\n", joinTraceRuleNames(decision.Rules))
	}
	for _, step := range decision.Governance {
		fmt.Fprintf(w, "  govern[%t] %s", step.Result, step.Check)
		if step.Detail != "" {
			fmt.Fprintf(w, " :: %s", step.Detail)
		}
		if step.Origin != nil {
			fmt.Fprintf(w, " @ %s", formatPolicyOrigin(step.Origin))
		}
		fmt.Fprintln(w)
	}
}

func formatPolicyBundleSummary(info coordd.PolicyBundleInfo) string {
	if info.Embedded {
		return "embedded defaults"
	}
	root := strings.TrimSpace(info.Root)
	if root == "" {
		return "unknown"
	}
	if len(info.Files) <= 1 {
		return root
	}
	return fmt.Sprintf("%s (%d files)", root, len(info.Files))
}

func formatPolicyOrigin(origin *coordd.PolicySourceOrigin) string {
	if origin == nil {
		return ""
	}
	if origin.Line > 0 {
		return fmt.Sprintf("%s:%d", origin.File, origin.Line)
	}
	return origin.File
}

func joinTraceRuleNames(rules []coordd.TraceRuleView) string {
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		if strings.TrimSpace(rule.Rule) == "" {
			continue
		}
		name := rule.Rule
		if rule.Priority != 0 {
			name = fmt.Sprintf("%s@%d", name, rule.Priority)
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}
