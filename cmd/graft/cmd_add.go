package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var quiet bool
	var skipEntities bool
	var forceEntities bool
	var stdin bool
	var stdin0 bool

	cmd := &cobra.Command{
		Use:   "add <files...>",
		Short: "Stage files for the next commit",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !stdin && !stdin0 {
				return fmt.Errorf("requires at least 1 arg(s), only received 0")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if stdin || stdin0 {
				scanner := bufio.NewScanner(os.Stdin)
				if stdin0 {
					scanner.Split(splitNull)
				}
				for scanner.Scan() {
					line := strings.TrimSpace(scanner.Text())
					if line != "" {
						args = append(args, line)
					}
				}
				if err := scanner.Err(); err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			opts := repo.AddOptions{
				SkipEntities:  skipEntities,
				ForceEntities: forceEntities,
			}

			if quiet {
				return r.AddWithOptions(args, nil, opts)
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
				case repo.AddProgressPhaseEntityStart:
				if progressLineActive {
					fmt.Fprintln(out)
					progressLineActive = false
				}
				fmt.Fprintln(out, "Extracting entities...")
			case repo.AddProgressPhaseEntityFile:
				if shouldRenderAddProgress(event.Current, event.Total) {
					fmt.Fprintf(out, "\rExtracting entities... %d/%d", event.Current, event.Total)
					progressLineActive = true
				}
			case repo.AddProgressPhaseEntityComplete:
				if progressLineActive {
					fmt.Fprintln(out)
					progressLineActive = false
				}
			case repo.AddProgressPhaseWriteIndex:
					if progressLineActive {
						fmt.Fprintln(out)
						progressLineActive = false
					}
					fmt.Fprintf(out, "Updated staging index (%d file(s))\n", event.Total)
				}
			}

			if err := r.AddWithOptions(args, progress, opts); err != nil {
				if progressLineActive {
					fmt.Fprintln(out)
				}
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress add progress output")
	cmd.Flags().BoolVar(&skipEntities, "skip-entities", false, "skip entity extraction (faster, lower memory)")
	cmd.Flags().BoolVar(&forceEntities, "force-entities", false, "force entity extraction for data formats above size threshold")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read file paths from stdin (one per line)")
	cmd.Flags().BoolVar(&stdin0, "stdin0", false, "read file paths from stdin, null-separated (for git ls-files -z)")
	return cmd
}

func splitNull(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
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
