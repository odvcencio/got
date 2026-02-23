package main

import (
	"fmt"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newCheckoutCmd() *cobra.Command {
	var createBranch bool

	cmd := &cobra.Command{
		Use:   "checkout <branch>",
		Short: "Switch branches",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if createBranch {
				head, err := r.ResolveRef("HEAD")
				if err != nil {
					return fmt.Errorf("cannot resolve HEAD: %w", err)
				}
				if err := r.CreateBranch(target, head); err != nil {
					return err
				}
			}

			if err := r.Checkout(target); err != nil {
				return err
			}

			if createBranch {
				fmt.Fprintf(cmd.OutOrStdout(), "switched to new branch '%s'\n", target)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "switched to branch '%s'\n", target)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&createBranch, "branch", "b", false, "create and switch to a new branch")

	return cmd
}
