package main

import (
	"fmt"
	"path/filepath"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage multiple working trees",
	}

	cmd.AddCommand(newWorktreeAddCmd())
	cmd.AddCommand(newWorktreeListCmd())
	cmd.AddCommand(newWorktreeRemoveCmd())
	cmd.AddCommand(newWorktreePruneCmd())

	return cmd
}

func newWorktreeAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <path> [<branch>]",
		Short: "Create a new linked worktree",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			path, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("worktree add: resolve path: %w", err)
			}

			var branch string
			if len(args) >= 2 {
				branch = args[1]
			} else {
				branch, err = r.CurrentBranch()
				if err != nil {
					return fmt.Errorf("worktree add: %w", err)
				}
			}

			if _, err := r.WorktreeAdd(path, branch); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created worktree at %s on branch %s\n", path, branch)
			return nil
		},
	}
}

func newWorktreeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all worktrees",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			worktrees, err := r.WorktreeList()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, wt := range worktrees {
				hash := shortHashStr(wt.Head)
				if wt.Branch != "" {
					fmt.Fprintf(out, "%s\t%s\t[%s]\n", wt.Path, hash, wt.Branch)
				} else {
					fmt.Fprintf(out, "%s\t%s\n", wt.Path, hash)
				}
			}
			return nil
		},
	}
}

func newWorktreeRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			name := args[0]
			if err := r.WorktreeRemove(name); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed worktree %s\n", name)
			return nil
		},
	}
}

func newWorktreePruneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Remove stale worktree entries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if err := r.WorktreePrune(); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Pruned stale worktrees\n")
			return nil
		},
	}
}
