package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newBlameCmd() *cobra.Command {
	var entitySelector string
	var limit int
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "blame [<path>]",
		Short: "Show entity-level attribution",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 {
				return fmt.Errorf("--limit must be greater than 0")
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			// Single-entity blame via --entity flag.
			if entitySelector != "" {
				result, err := r.BlameEntity(entitySelector, limit)
				if err != nil {
					return err
				}

				if jsonFlag {
					return writeJSON(cmd.OutOrStdout(), JSONBlameOutput{
						Path:       result.Path,
						EntityKey:  result.EntityKey,
						Author:     result.Author,
						CommitHash: string(result.CommitHash),
						Message:    result.Message,
					})
				}

				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", result.EntityKey, result.Author, result.CommitHash, result.Message)
				return nil
			}

			// Batch file blame via positional path arg.
			if len(args) == 0 {
				return fmt.Errorf("either --entity or a file path argument is required")
			}

			path := args[0]
			results, err := r.BlameFile(path, limit)
			if err != nil {
				return err
			}

			if jsonFlag {
				entities := make([]JSONBlameOutput, len(results))
				for i, res := range results {
					entities[i] = JSONBlameOutput{
						Path:       res.Path,
						EntityKey:  res.EntityKey,
						Author:     res.Author,
						CommitHash: string(res.CommitHash),
						Message:    res.Message,
					}
				}
				return writeJSON(cmd.OutOrStdout(), JSONBatchBlameOutput{
					Path:     path,
					Entities: entities,
				})
			}

			for _, res := range results {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", res.EntityKey, res.Author, res.CommitHash, res.Message)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&entitySelector, "entity", "", "entity selector in the form <path::entity_key>")
	cmd.Flags().IntVar(&limit, "limit", 200, "maximum number of commits to scan")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")

	return cmd
}
