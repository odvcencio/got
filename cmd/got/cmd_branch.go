package main

import (
	"fmt"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newBranchCmd() *cobra.Command {
	var deleteBranch string

	cmd := &cobra.Command{
		Use:   "branch [name]",
		Short: "List, create, or delete branches",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			// Delete mode.
			if deleteBranch != "" {
				if err := r.DeleteBranch(deleteBranch); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "deleted branch '%s'\n", deleteBranch)
				return nil
			}

			// Create mode.
			if len(args) == 1 {
				head, err := r.ResolveRef("HEAD")
				if err != nil {
					return fmt.Errorf("cannot resolve HEAD: %w", err)
				}
				if err := r.CreateBranch(args[0], head); err != nil {
					return err
				}
				return nil
			}

			// List mode.
			branches, err := r.ListBranches()
			if err != nil {
				return err
			}

			current, _ := r.CurrentBranch()

			out := cmd.OutOrStdout()
			for _, b := range branches {
				if b == current {
					fmt.Fprintf(out, "* %s\n", b)
				} else {
					fmt.Fprintf(out, "  %s\n", b)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&deleteBranch, "delete", "d", "", "delete the named branch")

	return cmd
}
