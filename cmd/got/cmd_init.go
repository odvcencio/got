package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [path]",
		Short: "Create an empty got repository",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}

			abs, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			// Ensure the target directory exists.
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return fmt.Errorf("create directory: %w", err)
			}

			r, err := repo.Init(abs)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "initialized empty got repository in %s\n", filepath.Join(r.RootDir, ".got")+string(filepath.Separator))
			return nil
		},
	}
}
