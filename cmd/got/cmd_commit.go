package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newCommitCmd() *cobra.Command {
	var message string
	var author string

	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Record changes to the repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" {
				return fmt.Errorf("commit message is required (-m)")
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if author == "" {
				author = os.Getenv("USER")
				if author == "" {
					author = "unknown"
				}
			}

			h, err := r.Commit(message, author)
			if err != nil {
				return err
			}

			// Determine current branch name for output.
			branch := "HEAD"
			head, err := r.Head()
			if err == nil && strings.HasPrefix(head, "refs/heads/") {
				branch = strings.TrimPrefix(head, "refs/heads/")
			}

			// Short hash: first 8 characters.
			short := string(h)
			if len(short) > 8 {
				short = short[:8]
			}

			fmt.Fprintf(cmd.OutOrStdout(), "[%s %s] %s\n", branch, short, message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "commit message")
	cmd.Flags().StringVar(&author, "author", "", "override author (default: $USER)")

	return cmd
}
