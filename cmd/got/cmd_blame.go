package main

import (
	"fmt"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newBlameCmd() *cobra.Command {
	var entitySelector string
	var limit int

	cmd := &cobra.Command{
		Use:   "blame --entity <path::entity_key>",
		Short: "Show entity-level attribution",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 {
				return fmt.Errorf("--limit must be greater than 0")
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			result, err := r.BlameEntity(entitySelector, limit)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", result.EntityKey, result.Author, result.CommitHash, result.Message)
			return nil
		},
	}

	cmd.Flags().StringVar(&entitySelector, "entity", "", "entity selector in the form <path::entity_key>")
	cmd.Flags().IntVar(&limit, "limit", 200, "maximum number of commits to scan")
	_ = cmd.MarkFlagRequired("entity")

	return cmd
}
