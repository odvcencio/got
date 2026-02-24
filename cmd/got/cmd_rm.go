package main

import (
	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newRmCmd() *cobra.Command {
	var cached bool

	cmd := &cobra.Command{
		Use:   "rm [--cached] <files...>",
		Short: "Remove files from working tree and stage the deletion",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			return r.Remove(args, cached)
		},
	}
	cmd.Flags().BoolVar(&cached, "cached", false, "remove from index only, keep files on disk")
	return cmd
}
