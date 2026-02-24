package main

import (
	"fmt"
	"time"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newReflogCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "reflog [ref]",
		Short: "Show ref update history",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			ref := ""
			if len(args) == 1 {
				ref = args[0]
			}
			entries, err := r.ReadReflog(ref, limit)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, e := range entries {
				sha := string(e.NewHash)
				if len(sha) > 8 {
					sha = sha[:8]
				}
				ts := time.Unix(e.Timestamp, 0).UTC().Format(time.RFC3339)
				fmt.Fprintf(out, "%s %s %s %s\n", sha, ts, e.Ref, e.Reason)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum entries to show")
	return cmd
}
