package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newShortlogCmd() *cobra.Command {
	var summary bool
	var numbered bool

	cmd := &cobra.Command{
		Use:   "shortlog [-s] [-n]",
		Short: "Summarise commit history by author",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			entries, err := r.Shortlog(repo.ShortlogOptions{
				Summary:  summary,
				Numbered: numbered,
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, e := range entries {
				if summary {
					fmt.Fprintf(out, "%6d\t%s\n", e.Count, e.Author)
				} else {
					fmt.Fprintf(out, "%s (%d):\n", e.Author, e.Count)
					for _, title := range e.Titles {
						fmt.Fprintf(out, "      %s\n", title)
					}
					fmt.Fprintln(out)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&summary, "summary", "s", false, "suppress commit descriptions, only show counts")
	cmd.Flags().BoolVarP(&numbered, "numbered", "n", false, "sort by count descending instead of author name")

	return cmd
}
