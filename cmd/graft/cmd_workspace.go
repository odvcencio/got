package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

func newWorkspaceCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage workspace registry for multi-repo coordination",
		Long:  `Add, list, or remove workspaces from ~/.graftconfig for cross-repository coordination.`,
	}

	cmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "JSON output")

	cmd.AddCommand(newWorkspaceAddCmd(&jsonFlag))
	cmd.AddCommand(newWorkspaceListCmd(&jsonFlag))
	cmd.AddCommand(newWorkspaceRemoveCmd(&jsonFlag))

	return cmd
}

func newWorkspaceAddCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "add <name> <path>",
		Short: "Register a workspace",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			wsPath := args[1]

			// Resolve to absolute path
			absPath, err := filepath.Abs(wsPath)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			// Verify the path exists
			info, err := os.Stat(absPath)
			if err != nil {
				return fmt.Errorf("path %q does not exist: %w", absPath, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("path %q is not a directory", absPath)
			}

			cfg, err := userconfig.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if cfg.Workspaces == nil {
				cfg.Workspaces = make(map[string]string)
			}
			cfg.Workspaces[name] = absPath

			if err := userconfig.Save(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			if *jsonFlag {
				return outputJSON(map[string]string{
					"status": "added",
					"name":   name,
					"path":   absPath,
				})
			}

			fmt.Printf("Workspace %q added: %s\n", name, absPath)
			return nil
		},
	}
}

func newWorkspaceListCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := userconfig.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			workspaces := cfg.Workspaces
			if workspaces == nil {
				workspaces = make(map[string]string)
			}

			if *jsonFlag {
				return outputJSON(workspaces)
			}

			if len(workspaces) == 0 {
				fmt.Println("No workspaces registered.")
				fmt.Println("Use 'graft workspace add <name> <path>' to register one.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPATH")
			for name, path := range workspaces {
				fmt.Fprintf(w, "%s\t%s\n", name, path)
			}
			return w.Flush()
		},
	}
}

func newWorkspaceRemoveCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Unregister a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := userconfig.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if cfg.Workspaces == nil {
				return fmt.Errorf("workspace %q not found", name)
			}

			if _, ok := cfg.Workspaces[name]; !ok {
				return fmt.Errorf("workspace %q not found", name)
			}

			delete(cfg.Workspaces, name)
			if len(cfg.Workspaces) == 0 {
				cfg.Workspaces = nil
			}

			if err := userconfig.Save(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			if *jsonFlag {
				return outputJSON(map[string]string{
					"status": "removed",
					"name":   name,
				})
			}

			fmt.Printf("Workspace %q removed\n", name)
			return nil
		},
	}
}
