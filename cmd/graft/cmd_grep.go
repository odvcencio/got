package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newGrepCmd() *cobra.Command {
	var caseInsensitive bool
	var fixedString bool

	cmd := &cobra.Command{
		Use:   "grep [-i] [-F] <pattern> [<pathspec>...]",
		Short: "Search tracked files for a pattern",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			opts := repo.GrepOptions{
				Pattern:         args[0],
				CaseInsensitive: caseInsensitive,
				FixedString:     fixedString,
			}

			// If additional args provided, use first as path pattern.
			if len(args) > 1 {
				opts.PathPattern = args[1]
			}

			results, err := r.Grep(opts)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, res := range results {
				fmt.Fprintf(out, "%s:%d:%s\n", res.Path, res.Line, res.Content)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&caseInsensitive, "ignore-case", "i", false, "case insensitive matching")
	cmd.Flags().BoolVarP(&fixedString, "fixed-strings", "F", false, "interpret pattern as a fixed string")

	return cmd
}
