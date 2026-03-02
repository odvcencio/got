package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newFetchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch [remote]",
		Short: "Download objects and refs from a remote",
		Long:  "Fetch downloads objects and refs from a remote without modifying the working tree or current branch. Remote refs are stored under refs/remotes/<remote>/.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			remoteName := "origin"
			if len(args) == 1 {
				remoteName = args[0]
			}

			result, err := r.FetchContext(cmd.Context(), remoteName)
			if err != nil {
				return err
			}

			if len(result.UpdatedRefs) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "already up to date\n")
				return nil
			}

			for _, ru := range result.UpdatedRefs {
				if ru.OldHash == "" {
					fmt.Fprintf(cmd.OutOrStdout(), " * [new ref] %s -> %s\n", shortHash(ru.NewHash), ru.Name)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "   %s..%s %s\n", shortHash(ru.OldHash), shortHash(ru.NewHash), ru.Name)
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "fetched %d objects from %s\n", result.ObjectCount, result.RemoteName)
			return nil
		},
	}

	return cmd
}
