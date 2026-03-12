package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newBlameCmd() *cobra.Command {
	var entitySelector string
	var limit int
	var jsonFlag bool
	var coordFlag bool

	cmd := &cobra.Command{
		Use:   "blame [<path>]",
		Short: "Show entity-level attribution and coordination history",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 {
				return fmt.Errorf("--limit must be greater than 0")
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			// Coordination blame mode: show claims and feed events for a file
			if coordFlag {
				if len(args) == 0 && entitySelector == "" {
					return fmt.Errorf("--coord requires a file path argument or --entity flag")
				}

				filePath := ""
				if len(args) > 0 {
					filePath = args[0]
				}

				return blameCoord(cmd, r, filePath, entitySelector, jsonFlag)
			}

			if entitySelector != "" && len(args) > 0 {
				return fmt.Errorf("--entity and positional path argument are mutually exclusive")
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
	cmd.Flags().BoolVar(&coordFlag, "coord", false, "show coordination claims and feed history for a file")

	return cmd
}

// blameCoord shows active claims and recent feed events for a file.
func blameCoord(cmd *cobra.Command, r *repo.Repo, filePath, entityFilter string, jsonOutput bool) error {
	c := coord.New(r, coord.DefaultConfig)
	out := cmd.OutOrStdout()

	// Get claims on this file
	var claims []coord.ClaimInfo
	if filePath != "" {
		claims, _ = c.ClaimsForFile(filePath)
	}

	// Filter claims by entity key if requested
	if entityFilter != "" && len(claims) > 0 {
		var filtered []coord.ClaimInfo
		for _, cl := range claims {
			if cl.EntityKey == entityFilter {
				filtered = append(filtered, cl)
			}
		}
		claims = filtered
	}

	// Walk feed for events touching this file's entities
	events, _ := c.WalkFeed("", 100)
	var relevant []coord.FeedEvent
	for _, evt := range events {
		for _, ent := range evt.Entities {
			match := false
			if filePath != "" && ent.File == filePath {
				match = true
			}
			if entityFilter != "" && ent.Key == entityFilter {
				match = true
			}
			if match {
				relevant = append(relevant, evt)
				break
			}
		}
	}

	if jsonOutput {
		result := coordBlameResult{
			File:           filePath,
			ActiveClaims:   claims,
			RecentActivity: relevant,
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(out, string(data))
		return nil
	}

	displayPath := filePath
	if displayPath == "" {
		displayPath = entityFilter
	}
	fmt.Fprintf(out, "Entity blame for %s\n\n", displayPath)

	if len(claims) > 0 {
		fmt.Fprintln(out, "Active claims:")
		for _, cl := range claims {
			fmt.Fprintf(out, "  %s — %s (%s, %s)\n",
				cl.EntityKey, cl.AgentName, cl.Mode,
				cl.ClaimedAt.Format(time.RFC3339))
		}
	} else {
		fmt.Fprintln(out, "No active claims.")
	}

	if len(relevant) > 0 {
		fmt.Fprintln(out, "\nRecent activity:")
		for _, evt := range relevant {
			entityKey := ""
			if len(evt.Entities) > 0 {
				entityKey = evt.Entities[0].Key
			}
			fmt.Fprintf(out, "  [%s] %s by %s\n", evt.Event, entityKey, evt.AgentName)
		}
	} else {
		fmt.Fprintln(out, "\nNo recent activity.")
	}

	return nil
}

type coordBlameResult struct {
	File           string             `json:"file"`
	ActiveClaims   []coord.ClaimInfo  `json:"active_claims"`
	RecentActivity []coord.FeedEvent  `json:"recent_activity"`
}
