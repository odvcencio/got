package main

import (
	"fmt"
	"os"
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

	author := os.Getenv("USER")
	if author == "" {
		author = "unknown"
	}

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
		Short: "Apply stash and remove it",
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

			if err := r.StashPop(index); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Dropped stash@{%d}.\n", index)
			return nil
		},
	}
}

func newStashApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply [index]",
		Short: "Apply stash without removing",
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

			if err := r.StashApply(index); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Applied stash@{%d}.\n", index)
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
	return &cobra.Command{
		Use:   "show [index]",
		Short: "Show stash details",
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

			entries, err := r.StashList()
			if err != nil {
				return err
			}

			if index < 0 || index >= len(entries) {
				return fmt.Errorf("stash: index %d out of range (stack has %d entries)", index, len(entries))
			}

			e := entries[index]
			short := string(e.CommitHash)
			if len(short) > 8 {
				short = short[:8]
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "stash@{%d}: %s\ncommit: %s\n", index, e.Message, short)
			return nil
		},
	}
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
	return index, nil
}
