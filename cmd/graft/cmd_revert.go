package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newRevertCmd() *cobra.Command {
	var continueFlag, abortFlag bool

	cmd := &cobra.Command{
		Use:   "revert <commit>",
		Short: "Revert a commit by creating an inverse commit",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if continueFlag {
				result, err := r.RevertContinue()
				if err != nil {
					return err
				}
				short := string(result.CommitHash)
				if len(short) > 8 {
					short = short[:8]
				}
				fmt.Fprintf(cmd.OutOrStdout(), "[%s %s] %s\n", branchName(r), short, result.Message)
				return nil
			}
			if abortFlag {
				return r.RevertAbort()
			}

			if len(args) == 0 {
				return fmt.Errorf("commit argument is required")
			}

			// Resolve the target (reuse the same approach as cherry-pick).
			targetHash, err := resolveCherryPickTarget(r, args[0])
			if err != nil {
				return err
			}

			result, err := r.Revert(targetHash)
			if err != nil {
				return err
			}

			short := string(result.CommitHash)
			if len(short) > 8 {
				short = short[:8]
			}
			fmt.Fprintf(cmd.OutOrStdout(), "[%s %s] %s\n", branchName(r), short, result.Message)
			return nil
		},
	}

	cmd.Flags().BoolVar(&continueFlag, "continue", false, "continue after conflict resolution")
	cmd.Flags().BoolVar(&abortFlag, "abort", false, "abort revert in progress")
	return cmd
}
