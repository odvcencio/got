package main

import (
	"fmt"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify loose and packed object integrity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			report, err := r.Store.Verify()
			if err != nil {
				return err
			}

			fmt.Fprintf(
				cmd.OutOrStdout(),
				"ok: verified %d loose object(s), %d pack file(s), %d packed object(s)\n",
				report.LooseObjects,
				report.PackFiles,
				report.PackObjects,
			)
			return nil
		},
	}
}
