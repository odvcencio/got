package main

import (
	"fmt"
	"io"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newMergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge <branch>",
		Short: "Merge a branch into the current branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branchName := args[0]

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			current, err := r.CurrentBranch()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "merging %s into %s...\n", branchName, current)

			report, err := r.Merge(branchName)
			if err != nil {
				return err
			}

			for _, f := range report.Files {
				printFileReport(out, f)
			}

			if report.HasConflicts {
				fmt.Fprintf(out, "merge completed with %d conflict", report.TotalConflicts)
				if report.TotalConflicts != 1 {
					fmt.Fprint(out, "s")
				}
				fmt.Fprintln(out)
				fmt.Fprintln(out, "fix conflicts and run got commit")
			} else {
				fmt.Fprintln(out, "merge completed cleanly")
				short := string(report.MergeCommit)
				if len(short) > 8 {
					short = short[:8]
				}
				fmt.Fprintf(out, "[%s %s] Merge branch '%s'\n", current, short, branchName)
			}

			return nil
		},
	}
}

func printFileReport(out io.Writer, f repo.FileMergeReport) {
	switch f.Status {
	case "conflict":
		fmt.Fprintf(out, "  %s: CONFLICT — %d conflict", f.Path, f.ConflictCount)
		if f.ConflictCount != 1 {
			fmt.Fprint(out, "s")
		}
		fmt.Fprintln(out)
	case "added":
		fmt.Fprintf(out, "  %s: %d entities (added)\n", f.Path, f.EntityCount)
	case "deleted":
		fmt.Fprintf(out, "  %s: deleted\n", f.Path)
	default: // "clean"
		fmt.Fprintf(out, "  %s: clean\n", f.Path)
	}
}
