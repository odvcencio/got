package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newCleanCmd() *cobra.Command {
	var dryRun, force, dirs, ignoredOnly, ignoredToo bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove untracked files from the working tree",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			opts := repo.CleanOptions{
				Directories: dirs,
				Force:       force,
				IgnoredOnly: ignoredOnly,
				IgnoredToo:  ignoredToo,
			}

			out := cmd.OutOrStdout()

			if dryRun {
				paths, err := r.CleanDryRun(opts)
				if err != nil {
					return err
				}
				for _, p := range paths {
					fmt.Fprintf(out, "Would remove %s\n", p)
				}
				return nil
			}

			paths, err := r.Clean(opts)
			if err != nil {
				return err
			}
			for _, p := range paths {
				fmt.Fprintf(out, "Removing %s\n", p)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "dry run")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "force removal")
	cmd.Flags().BoolVarP(&dirs, "directories", "d", false, "also remove directories")
	cmd.Flags().BoolVarP(&ignoredOnly, "ignored-only", "x", false, "remove only ignored files")
	cmd.Flags().BoolVarP(&ignoredToo, "ignored-too", "X", false, "also remove ignored files")

	return cmd
}
