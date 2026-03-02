package main

import (
	"fmt"
	"io"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newMergeCmd() *cobra.Command {
	var abortFlag bool

	cmd := &cobra.Command{
		Use:   "merge <branch>",
		Short: "Merge a branch into the current branch",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if abortFlag {
				if len(args) > 0 {
					return fmt.Errorf("--abort takes no positional arguments")
				}
				if err := r.MergeAbort(); err != nil {
					return err
				}
				fmt.Fprintln(out, "merge aborted, working tree restored")
				return nil
			}

			if len(args) < 1 {
				return fmt.Errorf("required argument <branch> not provided")
			}
			branchName := args[0]

			current, err := r.CurrentBranch()
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "merging %s into %s...\n", branchName, current)

			report, err := r.Merge(branchName)
			if err != nil {
				return err
			}

			if report.IsFastForward {
				short := string(report.MergeCommit)
				if len(short) > 8 {
					short = short[:8]
				}
				fmt.Fprintf(out, "fast-forward %s to %s\n", current, short)
				return nil
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
				fmt.Fprintln(out, "fix conflicts and run graft commit")
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

	cmd.Flags().BoolVar(&abortFlag, "abort", false, "abort the current merge and restore original state")

	return cmd
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
