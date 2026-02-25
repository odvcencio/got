package main

import (
	"fmt"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newGcCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Pack loose objects into a pack file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			summary, err := r.GC()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if summary.PackedObjects == 0 {
				fmt.Fprintln(out, "nothing to pack")
				return nil
			}

			fmt.Fprintf(
				out,
				"packed %d loose object(s) into %s (%s)\n",
				summary.PackedObjects,
				summary.PackFile,
				summary.IndexFile,
			)
			return nil
		},
	}
}
