package main

import (
	"fmt"
	"strings"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newCherryPickCmd() *cobra.Command {
	var entitySelector string

	cmd := &cobra.Command{
		Use:   "cherry-pick --entity <path::entity_key> <commit>",
		Short: "Cherry-pick a commit, optionally scoped to one entity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			targetHash, err := resolveCherryPickTarget(r, args[0])
			if err != nil {
				return err
			}

			result, err := r.CherryPickEntity(entitySelector, targetHash)
			if err != nil {
				return err
			}

			branch := "HEAD"
			head, err := r.Head()
			if err == nil && strings.HasPrefix(head, "refs/heads/") {
				branch = strings.TrimPrefix(head, "refs/heads/")
			}

			short := string(result.CommitHash)
			if len(short) > 8 {
				short = short[:8]
			}
			fmt.Fprintf(cmd.OutOrStdout(), "[%s %s] %s\n", branch, short, result.Message)
			return nil
		},
	}

	cmd.Flags().StringVar(&entitySelector, "entity", "", "entity selector in the form <path::entity_key>")
	_ = cmd.MarkFlagRequired("entity")

	return cmd
}

func resolveCherryPickTarget(r *repo.Repo, raw string) (object.Hash, error) {
	targetArg := strings.TrimSpace(raw)
	if targetArg == "" {
		return "", fmt.Errorf("commit is required")
	}

	if resolved, err := r.ResolveRef(targetArg); err == nil {
		if _, readErr := r.Store.ReadCommit(resolved); readErr == nil {
			return resolved, nil
		}
	}

	targetHash := object.Hash(targetArg)
	if _, err := r.Store.ReadCommit(targetHash); err != nil {
		return "", fmt.Errorf("cannot resolve commit %q: %w", raw, err)
	}
	return targetHash, nil
}
