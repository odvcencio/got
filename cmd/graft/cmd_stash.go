package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newStashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stash",
		Short: "Stash changes in the working directory",
		RunE:  stashPushRun, // bare "graft stash" behaves like "graft stash push"
	}

	cmd.AddCommand(newStashPushCmd())
	cmd.AddCommand(newStashPopCmd())
	cmd.AddCommand(newStashApplyCmd())
	cmd.AddCommand(newStashListCmd())
	cmd.AddCommand(newStashDropCmd())
	cmd.AddCommand(newStashShowCmd())

	return cmd
}

func newStashPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Save changes and revert working tree",
		Args:  cobra.NoArgs,
		RunE:  stashPushRun,
	}
}

func stashPushRun(cmd *cobra.Command, args []string) error {
	r, err := repo.Open(".")
	if err != nil {
		return err
	}

	author := r.ResolveAuthor()

	entry, err := r.Stash(author)
	if err != nil {
		return err
	}

	short := string(entry.CommitHash)
	if len(short) > 8 {
		short = short[:8]
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Saved working directory: %s %s\n", short, entry.Message)
	return nil
}

func newStashPopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pop [index]",
		Short: "Apply stash and remove it (uses 3-way merge)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			index, err := parseStashIndex(args)
			if err != nil {
				return err
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			result, err := r.StashApplyMerge(index)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if result.Clean {
				// Drop the stash only on clean apply.
				if err := r.StashDrop(index); err != nil {
					return err
				}
				fmt.Fprintf(out, "Dropped stash@{%d}.\n", index)
			} else {
				for _, p := range result.ConflictPaths {
					fmt.Fprintf(out, "CONFLICT in %s\n", p)
				}
				fmt.Fprintf(out, "Applied stash@{%d} with %d conflict(s). Stash not dropped. Resolve and commit.\n",
					index, len(result.ConflictPaths))
			}
			return nil
		},
	}
}

func newStashApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply [index]",
		Short: "Apply stash without removing (uses 3-way merge)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			index, err := parseStashIndex(args)
			if err != nil {
				return err
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			result, err := r.StashApplyMerge(index)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if result.Clean {
				fmt.Fprintf(out, "Applied stash@{%d}.\n", index)
			} else {
				for _, p := range result.ConflictPaths {
					fmt.Fprintf(out, "CONFLICT in %s\n", p)
				}
				fmt.Fprintf(out, "Applied stash@{%d} with %d conflict(s). Resolve and commit.\n",
					index, len(result.ConflictPaths))
			}
			return nil
		},
	}
}

func newStashListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all stash entries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			entries, err := r.StashList()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for i, e := range entries {
				ts := time.Unix(e.Timestamp, 0).UTC().Format(time.RFC3339)
				fmt.Fprintf(out, "stash@{%d}: %s (%s)\n", i, e.Message, ts)
			}
			return nil
		},
	}
}

func newStashDropCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "drop [index]",
		Short: "Remove a stash entry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			index, err := parseStashIndex(args)
			if err != nil {
				return err
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if err := r.StashDrop(index); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Dropped stash@{%d}.\n", index)
			return nil
		},
	}
}

func newStashShowCmd() *cobra.Command {
	var patchFlag bool

	cmd := &cobra.Command{
		Use:   "show [index]",
		Short: "Show files changed in a stash entry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			index, err := parseStashIndex(args)
			if err != nil {
				return err
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if patchFlag {
				// Show full diff.
				diff, err := r.StashShowDiff(index)
				if err != nil {
					return err
				}
				_, _ = out.Write(diff)
				return nil
			}

			// Show summary of changed files.
			entries, err := r.StashShow(index)
			if err != nil {
				return err
			}

			for _, e := range entries {
				fmt.Fprintf(out, " %s | %s\n", e.Path, e.ChangeType)
			}
			if len(entries) > 0 {
				fmt.Fprintf(out, " %d file(s) changed\n", len(entries))
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&patchFlag, "patch", "p", false, "show full diff (patch)")
	return cmd
}

// parseStashIndex extracts the stash index from the optional positional arg,
// defaulting to 0 when no argument is provided.
func parseStashIndex(args []string) (int, error) {
	if len(args) == 0 {
		return 0, nil
	}
	index, err := strconv.Atoi(args[0])
	if err != nil {
		return 0, fmt.Errorf("invalid stash index %q: %w", args[0], err)
	}
	if index < 0 {
		return 0, fmt.Errorf("stash index must be non-negative, got %d", index)
	}
	return index, nil
}
