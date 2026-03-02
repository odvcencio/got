package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newModuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "module",
		Short: "Manage graft modules",
	}

	cmd.AddCommand(newModuleListCmd())
	cmd.AddCommand(newModuleStatusCmd())
	cmd.AddCommand(newModuleAddCmd())
	cmd.AddCommand(newModuleRmCmd())
	cmd.AddCommand(newModuleSyncCmd())
	cmd.AddCommand(newModuleUpdateCmd())

	return cmd
}

func newModuleListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured modules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			modules, err := r.ListModules()
			if err != nil {
				return err
			}
			if len(modules) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no modules configured")
				return nil
			}

			for _, m := range modules {
				var ref string
				if m.Track != "" {
					ref = fmt.Sprintf("(tracking %s)", m.Track)
				} else if m.Pin != "" {
					ref = fmt.Sprintf("(pinned %s)", m.Pin)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n",
					m.Name, m.Path, shortHashOrNone(m.Commit), ref)
			}
			return nil
		},
	}
}

func newModuleStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show module status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			entries, err := r.ModuleStatus()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no modules configured")
				return nil
			}

			for _, e := range entries {
				var tracking string
				if e.Track != "" {
					tracking = e.Track
				} else if e.Pin != "" {
					tracking = e.Pin
				}

				var state string
				switch {
				case e.LockedCommit == "":
					state = "not locked"
				case e.Synced:
					state = "synced"
				default:
					state = "out of sync"
				}

				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n",
					e.Name, e.Path, tracking, shortHashOrNone(e.LockedCommit), state)
			}
			return nil
		},
	}
}

func newModuleAddCmd() *cobra.Command {
	var track string
	var pin string

	cmd := &cobra.Command{
		Use:   "add <url> [<path>]",
		Short: "Add a module",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if track != "" && pin != "" {
				return fmt.Errorf("--track and --pin are mutually exclusive")
			}

			url := args[0]
			var modPath string
			if len(args) == 2 {
				modPath = args[1]
			} else {
				modPath = inferModulePath(url)
			}

			name := filepath.Base(modPath)

			// Default to tracking main if neither specified.
			if track == "" && pin == "" {
				track = "main"
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			entry := repo.ModuleEntry{
				Name:  name,
				URL:   url,
				Path:  modPath,
				Track: track,
				Pin:   pin,
			}
			if err := r.AddModuleEntry(entry); err != nil {
				return err
			}

			var ref string
			if track != "" {
				ref = fmt.Sprintf("tracking %s", track)
			} else {
				ref = fmt.Sprintf("pinned %s", pin)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added module %q -> %s (%s)\n", name, modPath, ref)
			fmt.Fprintln(cmd.OutOrStdout(), "run 'graft module update' to fetch objects")
			return nil
		},
	}

	cmd.Flags().StringVar(&track, "track", "", "branch to track")
	cmd.Flags().StringVar(&pin, "pin", "", "tag or commit to pin to")

	return cmd
}

func newModuleRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a module",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			// Get module info first so we can show the path in the message.
			mod, err := r.GetModule(name)
			if err != nil {
				return err
			}
			modPath := mod.Path

			// Remove the working tree directory if it exists.
			absPath := filepath.Join(r.RootDir, filepath.FromSlash(modPath))
			if err := os.RemoveAll(absPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove working tree: %w", err)
			}

			if err := r.RemoveModuleEntry(name); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "removed module %q (was at %s)\n", name, modPath)
			return nil
		},
	}
}

func newModuleSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Sync module working trees from lock file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if err := r.ModuleSync(); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "modules synced")
			return nil
		},
	}
}

func newModuleUpdateCmd() *cobra.Command {
	var depth int

	cmd := &cobra.Command{
		Use:   "update [<name>...]",
		Short: "Fetch latest objects for modules",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			modules, err := r.ListModules()
			if err != nil {
				return err
			}
			if len(modules) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no modules configured")
				return nil
			}

			// If names specified, filter to just those.
			nameFilter := make(map[string]bool, len(args))
			for _, a := range args {
				nameFilter[a] = true
			}

			ctx := cmd.Context()
			anyUpdated := false

			for _, m := range modules {
				if len(nameFilter) > 0 && !nameFilter[m.Name] {
					continue
				}

				// Canonicalize the module URL.
				resolvedURL, err := canonicalizeRemoteSpec(m.URL)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: invalid URL %q: %v\n", m.Name, m.URL, err)
					continue
				}

				result, err := r.ModuleFetchAndUpdate(ctx, m.Name, resolvedURL, depth)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %v\n", m.Name, err)
					continue
				}

				if !result.Changed {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: already up to date\n", result.Name)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s (%d objects fetched)\n",
						result.Name,
						shortHashOrNone(result.OldCommit),
						shortHash(result.NewCommit),
						result.ObjectCount)
					anyUpdated = true
				}
			}

			if anyUpdated {
				fmt.Fprintln(cmd.OutOrStdout(), "run 'graft module sync' to checkout updated versions")
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&depth, "depth", 0, "limit fetch depth (0 = full)")

	return cmd
}

// inferModulePath derives a module working tree path from a URL.
// It strips common shorthand prefixes (github:, gh:, etc.), takes the last
// path segment, and strips a .git suffix.
func inferModulePath(url string) string {
	// Strip known shorthand prefixes.
	for _, prefix := range []string{"github:", "gh:", "gitlab:", "gl:", "bitbucket:", "bb:"} {
		if strings.HasPrefix(url, prefix) {
			url = strings.TrimPrefix(url, prefix)
			break
		}
	}

	// Take the last path segment.
	seg := url
	if idx := strings.LastIndex(seg, "/"); idx >= 0 {
		seg = seg[idx+1:]
	}
	// Also handle colon-separated paths (e.g., git@host:org/repo.git).
	if idx := strings.LastIndex(seg, ":"); idx >= 0 {
		seg = seg[idx+1:]
	}

	// Strip .git suffix.
	seg = strings.TrimSuffix(seg, ".git")

	return seg
}

// shortHashOrNone returns "-------" if the hash is empty, otherwise the first
// 8 characters of the hash.
func shortHashOrNone(h object.Hash) string {
	if h == "" {
		return "-------"
	}
	return shortHash(h)
}
