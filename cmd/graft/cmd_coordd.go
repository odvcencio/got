package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
			decision, err := coordd.EvaluateActionPolicy(input)
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
			decision, err := coordd.EvaluateActionPolicy(input)
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
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "spawn --name <child> [--profile <profile>] -- <command...>",
		Short: "Spawn a governed detached child workstream",
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

			result, err := coordd.SpawnDetached(r, readActiveAgentID(r), coordd.SpawnRequest{
				Name:             name,
				Command:          args,
				RequestedProfile: profile,
			})
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
			return err
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "child agent/workstream name")
	cmd.Flags().StringVar(&profile, "profile", "", "requested child runtime profile")
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
			if jsonFlag {
				return outputJSON(cfg)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "mode: %s\n", cfg.Mode)
			fmt.Fprintf(cmd.OutOrStdout(), "require_active_agent: %t\n", cfg.RequireActiveAgent)
			fmt.Fprintf(cmd.OutOrStdout(), "preferred_backend: %s\n", cfg.PreferredBackend)
			fmt.Fprintf(cmd.OutOrStdout(), "container_runtime: %s\n", cfg.ContainerRuntime)
			fmt.Fprintf(cmd.OutOrStdout(), "container_image: %s\n", cfg.ContainerImage)
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
	if record.Backend != "" {
		fmt.Fprintf(w, " backend=%s", record.Backend)
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
	fmt.Fprintln(w)
}
