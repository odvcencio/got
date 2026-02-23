package main

import (
	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <files...>",
		Short: "Stage files for the next commit",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			return r.Add(args)
		},
	}
}
