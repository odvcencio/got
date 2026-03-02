package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newResetCmd() *cobra.Command {
	var soft, mixed, hard bool

	cmd := &cobra.Command{
		Use:   "reset [--soft | --mixed | --hard] [<commit>] [-- <paths>...]",
		Short: "Reset HEAD, staging area, and/or working tree",
		Long: `Reset has two forms:

1. Path mode (no --soft/--mixed/--hard):
   graft reset [<paths>...]
   Unstages the given paths by restoring their index entries from HEAD.
   Does not move HEAD or touch the working tree.

2. Commit mode (with --soft, --mixed, or --hard):
   graft reset [--soft | --mixed | --hard] [<commit>]
   Moves HEAD to the specified commit (default: HEAD).
   --soft:  Only move HEAD. Staging and working tree are unchanged.
   --mixed: Move HEAD and reset staging (default).
   --hard:  Move HEAD, reset staging, and restore working tree.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			// If any mode flag is set, this is a commit-level reset.
			if soft || mixed || hard {
				mode := repo.ResetMixed
				if soft {
					mode = repo.ResetSoft
				} else if hard {
					mode = repo.ResetHard
				}

				// Determine target commit (default: HEAD).
				targetSpec := "HEAD"
				if len(args) > 0 {
					targetSpec = args[0]
				}

				target, err := r.ResolveTreeish(targetSpec)
				if err != nil {
					return fmt.Errorf("reset: %w", err)
				}

				if err := r.ResetToCommit(target, mode); err != nil {
					return err
				}

				fmt.Fprintf(cmd.OutOrStdout(), "HEAD is now at %s\n", shortHash(target))
				return nil
			}

			// Path mode: unstage paths (original behavior).
			return r.Reset(args)
		},
	}

	cmd.Flags().BoolVar(&soft, "soft", false, "only move HEAD (staging and working tree unchanged)")
	cmd.Flags().BoolVar(&mixed, "mixed", false, "move HEAD and reset staging (default commit-level mode)")
	cmd.Flags().BoolVar(&hard, "hard", false, "move HEAD, reset staging, and restore working tree")

	return cmd
}
