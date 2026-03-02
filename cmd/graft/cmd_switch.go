package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newSwitchCmd() *cobra.Command {
	var createBranch string

	cmd := &cobra.Command{
		Use:   "switch <branch>",
		Short: "Switch branches (modern alternative to checkout)",
		Long: `Switch to a different branch, updating the working directory.

This is the modern alternative to 'graft checkout' for branch switching.
Use -c to create a new branch and switch to it in one step.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			target := args[0]

			// TODO: handle "-" for previous branch once reflog tracks
			// checkout operations with enough info to identify the
			// source branch.
			if target == "-" {
				return fmt.Errorf("switch to previous branch (-) is not yet supported")
			}

			// Handle -c (create and switch to new branch).
			if createBranch != "" {
				head, err := r.ResolveRef("HEAD")
				if err != nil {
					return fmt.Errorf("cannot resolve HEAD: %w", err)
				}
				if err := r.CreateBranch(createBranch, head); err != nil {
					return err
				}
				target = createBranch
			}

			if err := r.Checkout(target); err != nil {
				return err
			}

			if createBranch != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "switched to new branch '%s'\n", target)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "switched to branch '%s'\n", target)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&createBranch, "create", "c", "", "create and switch to a new branch")

	return cmd
}
