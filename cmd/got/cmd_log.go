package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newLogCmd() *cobra.Command {
	var oneline bool
	var limit int

	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show commit history",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			headHash, err := r.ResolveRef("HEAD")
			if err != nil {
				return fmt.Errorf("cannot resolve HEAD: %w", err)
			}

			commits, err := r.Log(headHash, limit)
			if err != nil {
				return err
			}

			if len(commits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no commits yet")
				return nil
			}

			// Determine the current branch name for decoration.
			branchName := ""
			head, err := r.Head()
			if err == nil && strings.HasPrefix(head, "refs/heads/") {
				branchName = strings.TrimPrefix(head, "refs/heads/")
			}

			// Reconstruct hashes: the first commit's hash is headHash,
			// and each subsequent commit's hash is the first parent of the
			// previous commit.
			hashes := make([]object.Hash, len(commits))
			hashes[0] = headHash
			for i := 1; i < len(commits); i++ {
				hashes[i] = commits[i-1].Parents[0]
			}

			out := cmd.OutOrStdout()
			for i, c := range commits {
				h := hashes[i]
				decoration := buildDecoration(h, headHash, branchName)

				if oneline {
					short := string(h)
					if len(short) > 8 {
						short = short[:8]
					}
					if decoration != "" {
						fmt.Fprintf(out, "%s %s %s\n", short, decoration, c.Message)
					} else {
						fmt.Fprintf(out, "%s %s\n", short, c.Message)
					}
				} else {
					if decoration != "" {
						fmt.Fprintf(out, "commit %s %s\n", h, decoration)
					} else {
						fmt.Fprintf(out, "commit %s\n", h)
					}
					fmt.Fprintf(out, "Author: %s\n", c.Author)
					fmt.Fprintf(out, "Date:   %s\n", time.Unix(c.Timestamp, 0).Format("2006-01-02 15:04:05"))
					fmt.Fprintln(out)
					fmt.Fprintf(out, "    %s\n", c.Message)
					fmt.Fprintln(out)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&oneline, "oneline", false, "compact one-line format")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "maximum number of commits to show")

	return cmd
}

// buildDecoration returns a string like "(HEAD -> main)" if the commit is
// the current HEAD, or "" otherwise.
func buildDecoration(commitHash, headHash object.Hash, branchName string) string {
	if commitHash != headHash {
		return ""
	}
	if branchName != "" {
		return "(HEAD -> " + branchName + ")"
	}
	return "(HEAD)"
}
