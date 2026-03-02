package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newCherryPickCmd() *cobra.Command {
	var entitySelector string
	var continueFlag, abortFlag, skipFlag bool

	cmd := &cobra.Command{
		Use:   "cherry-pick [--entity <path::entity_key>] [--continue | --abort | --skip] [<commit>]",
		Short: "Cherry-pick a commit, optionally scoped to one entity",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			// Handle sequencer flags (--continue, --abort, --skip).
			flagCount := 0
			if continueFlag {
				flagCount++
			}
			if abortFlag {
				flagCount++
			}
			if skipFlag {
				flagCount++
			}
			if flagCount > 1 {
				return fmt.Errorf("cherry-pick: only one of --continue, --abort, or --skip may be specified")
			}

			if continueFlag {
				if len(args) > 0 {
					return fmt.Errorf("cherry-pick --continue takes no arguments")
				}
				result, err := r.CherryPickContinue()
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
			}

			if abortFlag {
				if len(args) > 0 {
					return fmt.Errorf("cherry-pick --abort takes no arguments")
				}
				if err := r.CherryPickAbort(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "cherry-pick aborted")
				return nil
			}

			if skipFlag {
				if len(args) > 0 {
					return fmt.Errorf("cherry-pick --skip takes no arguments")
				}
				if err := r.CherryPickSkip(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "cherry-pick skipped")
				return nil
			}

			// Normal cherry-pick: requires exactly one argument.
			if len(args) != 1 {
				return fmt.Errorf("cherry-pick requires exactly one commit argument")
			}

			targetHash, err := resolveCherryPickTarget(r, args[0])
			if err != nil {
				return err
			}

			if entitySelector != "" {
				// Entity-level cherry-pick.
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
			}

			// Commit-level cherry-pick.
			result, cpErr := r.CherryPick(targetHash)
			if cpErr != nil {
				// If it's a conflict error, print the message and return the error.
				var conflictErr *repo.ErrCherryPickConflict
				if errors.As(cpErr, &conflictErr) {
					return cpErr
				}
				return cpErr
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
	cmd.Flags().BoolVar(&continueFlag, "continue", false, "continue after conflict resolution")
	cmd.Flags().BoolVar(&abortFlag, "abort", false, "abort cherry-pick in progress")
	cmd.Flags().BoolVar(&skipFlag, "skip", false, "skip current cherry-pick")

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
