package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "add <files...>",
		Short: "Stage files for the next commit",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			if quiet {
				return r.Add(args)
			}

			out := cmd.ErrOrStderr()
			progressLineActive := false
			progress := func(event repo.AddProgress) {
				switch event.Phase {
				case repo.AddProgressPhaseScanStart:
					fmt.Fprintln(out, "Scanning files...")
				case repo.AddProgressPhaseScanComplete:
					fmt.Fprintf(out, "Found %d file(s) to stage\n", event.Total)
				case repo.AddProgressPhaseStageFile:
					if shouldRenderAddProgress(event.Current, event.Total) {
						fmt.Fprintf(out, "\rStaging files... %d/%d", event.Current, event.Total)
						progressLineActive = true
					}
				case repo.AddProgressPhaseWriteIndex:
					if progressLineActive {
						fmt.Fprintln(out)
						progressLineActive = false
					}
					fmt.Fprintf(out, "Updated staging index (%d file(s))\n", event.Total)
				}
			}

			if err := r.AddWithProgress(args, progress); err != nil {
				if progressLineActive {
					fmt.Fprintln(out)
				}
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress add progress output")
	return cmd
}

func shouldRenderAddProgress(current, total int) bool {
	if total <= 0 {
		return false
	}
	if current <= 1 || current == total {
		return true
	}
	if total <= 100 {
		return true
	}
	step := total / 100 // cap updates to around 100 writes for huge adds
	if step < 10 {
		step = 10
	}
	return current%step == 0
}
