package main

import (
	"os"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newArchiveCmd() *cobra.Command {
	var format string
	var prefix string

	cmd := &cobra.Command{
		Use:   "archive [--format=tar|zip] [--prefix=<prefix>/] <tree-ish>",
		Short: "Create an archive of files from a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			return r.Archive(os.Stdout, args[0], repo.ArchiveOptions{
				Format: format,
				Prefix: prefix,
			})
		},
	}

	cmd.Flags().StringVar(&format, "format", "tar", "archive format: tar or zip")
	cmd.Flags().StringVar(&prefix, "prefix", "", "prepend prefix to each filename in the archive")

	return cmd
}
