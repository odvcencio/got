package main

import (
	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset [paths...]",
		Short: "Unstage paths by restoring index entries from HEAD",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			return r.Reset(args)
		},
	}
}
