package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newRebaseCmd() *cobra.Command {
	var ontoFlag string
	var continueFlag bool
	var abortFlag bool
	var skipFlag bool
	var interactiveFlag bool

	cmd := &cobra.Command{
		Use:   "rebase [<upstream>]",
		Short: "Reapply commits on top of another base tip",
		Long: `Rebase the current branch onto upstream (or --onto newbase).

Use -i/--interactive to edit the list of commits before replaying.
Use --continue after resolving conflicts, --abort to cancel, or --skip to skip a commit.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate mutual exclusivity of control flags.
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
				return fmt.Errorf("only one of --continue, --abort, --skip may be used")
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			// Handle control flags (no positional args expected).
			if continueFlag {
				if len(args) > 0 {
					return fmt.Errorf("--continue takes no positional arguments")
				}
				return rebaseContinue(r, out)
			}
			if abortFlag {
				if len(args) > 0 {
					return fmt.Errorf("--abort takes no positional arguments")
				}
				return rebaseAbort(r, out)
			}
			if skipFlag {
				if len(args) > 0 {
					return fmt.Errorf("--skip takes no positional arguments")
				}
				return rebaseSkip(r, out)
			}

			// Starting a new rebase: require <upstream>.
			if len(args) < 1 {
				return fmt.Errorf("required argument <upstream> not provided")
			}
			upstream := args[0]

			if interactiveFlag {
				return rebaseInteractiveStart(r, out, upstream)
			}
			if ontoFlag != "" {
				return rebaseOnto(r, out, ontoFlag, upstream)
			}
			return rebaseStart(r, out, upstream)
		},
	}

	cmd.Flags().StringVar(&ontoFlag, "onto", "", "rebase onto arbitrary base")
	cmd.Flags().BoolVar(&continueFlag, "continue", false, "continue rebase after conflict resolution")
	cmd.Flags().BoolVar(&abortFlag, "abort", false, "abort and restore original state")
	cmd.Flags().BoolVar(&skipFlag, "skip", false, "skip the conflicting commit")
	cmd.Flags().BoolVarP(&interactiveFlag, "interactive", "i", false, "interactive rebase: edit the todo list before replaying")

	return cmd
}

// rebaseInteractiveStart handles: graft rebase -i <upstream>
func rebaseInteractiveStart(r *repo.Repo, out io.Writer, upstream string) error {
	ontoHash, err := resolveTarget(r, upstream)
	if err != nil {
		return err
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return err
	}
	mergeBase, err := r.FindMergeBase(headHash, ontoHash)
	if err != nil {
		return err
	}
	count, err := countCommits(r, mergeBase, headHash)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Interactive rebase onto %s... %d commit(s)\n", shortHash(ontoHash), count)

	err = r.RebaseInteractive(upstream)
	return handleRebaseResult(r, out, ontoHash, err)
}

// rebaseStart handles: graft rebase <upstream>
func rebaseStart(r *repo.Repo, out io.Writer, upstream string) error {
	// Resolve upstream to get the onto hash for output.
	ontoHash, err := resolveTarget(r, upstream)
	if err != nil {
		return err
	}

	// Count commits to replay: merge-base..HEAD
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return err
	}
	mergeBase, err := r.FindMergeBase(headHash, ontoHash)
	if err != nil {
		return err
	}
	count, err := countCommits(r, mergeBase, headHash)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Rebasing onto %s... Replaying %d commit(s)\n", shortHash(ontoHash), count)

	err = r.Rebase(upstream)
	return handleRebaseResult(r, out, ontoHash, err)
}

// rebaseOnto handles: graft rebase --onto <newbase> <upstream>
func rebaseOnto(r *repo.Repo, out io.Writer, newbase, upstream string) error {
	ontoHash, err := resolveTarget(r, newbase)
	if err != nil {
		return err
	}

	// Count commits to replay: upstream..HEAD
	upstreamHash, err := resolveTarget(r, upstream)
	if err != nil {
		return err
	}
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return err
	}
	count, err := countCommits(r, upstreamHash, headHash)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Rebasing onto %s... Replaying %d commit(s)\n", shortHash(ontoHash), count)

	err = r.RebaseOnto(newbase, upstream)
	return handleRebaseResult(r, out, ontoHash, err)
}

// rebaseContinue handles: graft rebase --continue
func rebaseContinue(r *repo.Repo, out io.Writer) error {
	// Read onto hash from sequencer state for output.
	ontoHash := readSequencerOnto(r)

	err := r.RebaseContinue()
	return handleRebaseResult(r, out, ontoHash, err)
}

// rebaseAbort handles: graft rebase --abort
func rebaseAbort(r *repo.Repo, out io.Writer) error {
	// Read orig-head from sequencer state before abort cleans it up.
	origHead := readSequencerOrigHead(r)

	err := r.RebaseAbort()
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Rebase aborted. Restored to %s\n", shortHash(origHead))
	return nil
}

// rebaseSkip handles: graft rebase --skip
func rebaseSkip(r *repo.Repo, out io.Writer) error {
	// Read stopped-sha for output before skip removes it.
	stoppedHash := readSequencerStopped(r)
	ontoHash := readSequencerOnto(r)

	fmt.Fprintf(out, "Skipped %s\n", shortHash(stoppedHash))

	err := r.RebaseSkip()
	return handleRebaseResult(r, out, ontoHash, err)
}

// handleRebaseResult processes the result of a rebase operation, printing
// appropriate messages for conflicts or success.
func handleRebaseResult(r *repo.Repo, out io.Writer, ontoHash object.Hash, err error) error {
	if err == nil {
		fmt.Fprintf(out, "Successfully rebased onto %s\n", shortHash(ontoHash))
		return nil
	}

	var conflictErr *repo.ErrRebaseConflict
	if errors.As(err, &conflictErr) {
		// Parse the conflict details to extract individual paths.
		// Details format: "conflict in: path1, path2, ..."
		details := conflictErr.Details
		details = strings.TrimPrefix(details, "conflict in: ")
		paths := strings.Split(details, ", ")
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p != "" {
				fmt.Fprintf(out, "CONFLICT in %s. Fix conflicts and run: graft rebase --continue\n", p)
			}
		}
		return nil
	}

	return err
}

// resolveTarget resolves a branch name or hash to a commit hash using the
// same resolution order as the repo package: refs/heads/<name>, then full
// ref path, then raw hash.
func resolveTarget(r *repo.Repo, target string) (object.Hash, error) {
	// Try as branch ref first.
	h, err := r.ResolveRef("refs/heads/" + target)
	if err == nil {
		return h, nil
	}
	// Try as full ref path.
	h, err = r.ResolveRef(target)
	if err == nil {
		return h, nil
	}
	// Try as raw hash.
	_, err = r.Store.ReadCommit(object.Hash(target))
	if err == nil {
		return object.Hash(target), nil
	}
	return "", fmt.Errorf("cannot resolve %q to a commit", target)
}

// countCommits counts the number of commits in the range (stop, tip] by
// walking first-parent links.
func countCommits(r *repo.Repo, stop, tip object.Hash) (int, error) {
	count := 0
	current := tip
	for current != stop && current != "" {
		count++
		c, err := r.Store.ReadCommit(current)
		if err != nil {
			return 0, fmt.Errorf("count commits: read %s: %w", current, err)
		}
		if len(c.Parents) == 0 {
			break
		}
		current = c.Parents[0]
	}
	return count, nil
}

// readSequencerOnto reads the "onto" hash from the rebase sequencer state.
func readSequencerOnto(r *repo.Repo) object.Hash {
	data, err := os.ReadFile(filepath.Join(r.GraftDir, "rebase-merge", "onto"))
	if err != nil {
		return ""
	}
	return object.Hash(strings.TrimSpace(string(data)))
}

// readSequencerOrigHead reads the "orig-head" hash from the rebase sequencer state.
func readSequencerOrigHead(r *repo.Repo) object.Hash {
	data, err := os.ReadFile(filepath.Join(r.GraftDir, "rebase-merge", "orig-head"))
	if err != nil {
		return ""
	}
	return object.Hash(strings.TrimSpace(string(data)))
}

// readSequencerStopped reads the "stopped-sha" from the rebase sequencer state.
func readSequencerStopped(r *repo.Repo) object.Hash {
	data, err := os.ReadFile(filepath.Join(r.GraftDir, "rebase-merge", "stopped-sha"))
	if err != nil {
		return ""
	}
	return object.Hash(strings.TrimSpace(string(data)))
}
