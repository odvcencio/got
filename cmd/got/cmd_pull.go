package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/got/pkg/remote"
	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	var allowMerge bool

	cmd := &cobra.Command{
		Use:   "pull [remote] [branch]",
		Short: "Fetch from remote and fast-forward (or merge with --merge)",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			remoteArg := ""
			branch := ""
			switch len(args) {
			case 1:
				candidate := strings.TrimSpace(args[0])
				if looksLikeRemoteURL(candidate) {
					remoteArg = candidate
				} else if _, err := r.RemoteURL(candidate); err == nil {
					remoteArg = candidate
				} else {
					branch = candidate
				}
			case 2:
				remoteArg = strings.TrimSpace(args[0])
				branch = strings.TrimSpace(args[1])
			}
			remoteName, remoteURL, transport, err := resolveRemoteNameAndSpec(r, remoteArg)
			if err != nil {
				return err
			}
			if transport == remoteTransportGit {
				return pullViaGit(cmd, r, remoteURL, branch, allowMerge)
			}

			currentBranch, err := r.CurrentBranch()
			if err != nil {
				return err
			}
			if branch == "" {
				branch = currentBranch
			}
			if branch == "" {
				return fmt.Errorf("cannot infer branch while HEAD is detached; specify branch")
			}

			client, err := remote.NewClient(remoteURL)
			if err != nil {
				return err
			}
			remoteRefs, err := client.ListRefs(cmd.Context())
			if err != nil {
				return err
			}

			remoteRef := "heads/" + branch
			remoteHash, ok := remoteRefs[remoteRef]
			if !ok || strings.TrimSpace(string(remoteHash)) == "" {
				return fmt.Errorf("remote branch %q not found", branch)
			}

			localRef := "refs/heads/" + branch
			localHash, err := r.ResolveRef(localRef)
			hasLocal := err == nil
			if err != nil && !os.IsNotExist(err) {
				return err
			}

			if currentBranch == branch {
				if err := ensureCleanWorkingTree(r); err != nil {
					return err
				}
			}

			haves, err := localRefTips(r)
			if err != nil {
				return err
			}
			fetched, err := remote.FetchIntoStore(cmd.Context(), client, r.Store, []object.Hash{remoteHash}, haves)
			if err != nil {
				return err
			}

			if hasLocal && localHash != remoteHash {
				base, err := r.FindMergeBase(localHash, remoteHash)
				if err != nil {
					return fmt.Errorf("pull: merge-base: %w", err)
				}
				// Local already contains remote commit(s).
				if base == remoteHash {
					if err := r.UpdateRef(remoteTrackingRefName(remoteName, remoteRef), remoteHash); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "already up to date (local %s is ahead of remote %s)\n", shortHash(localHash), shortHash(remoteHash))
					return nil
				}

				// Diverged: require explicit merge mode.
				if base != localHash {
					if !allowMerge {
						return fmt.Errorf("pull would not fast-forward %s (local %s, remote %s); retry with --merge", branch, shortHash(localHash), shortHash(remoteHash))
					}
					if currentBranch != branch {
						return fmt.Errorf("pull --merge requires checked out branch %q (current: %q)", branch, currentBranch)
					}

					tempBranch := temporaryPullBranch(branch)
					if err := r.UpdateRef("refs/heads/"+tempBranch, remoteHash); err != nil {
						return fmt.Errorf("pull: create temp branch: %w", err)
					}
					defer func() { _ = r.DeleteBranch(tempBranch) }()

					report, err := r.Merge(tempBranch)
					if err != nil {
						return fmt.Errorf("pull: merge: %w", err)
					}
					if err := r.UpdateRef(remoteTrackingRefName(remoteName, remoteRef), remoteHash); err != nil {
						return err
					}
					if report.HasConflicts {
						return fmt.Errorf("pull: merge completed with %d conflict(s); resolve conflicts and commit", report.TotalConflicts)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "merged %s into %s (%d objects fetched)\n", shortHash(remoteHash), branch, fetched)
					return nil
				}
			}

			needsWorktreeUpdate := currentBranch == branch && (!hasLocal || localHash != remoteHash)
			if needsWorktreeUpdate {
				// Checkout by commit hash before moving branch ref so clean-tree
				// checks compare against the pre-pull HEAD state.
				if err := r.Checkout(string(remoteHash)); err != nil {
					return err
				}
			}

			if err := r.UpdateRef(localRef, remoteHash); err != nil {
				return err
			}
			if err := r.UpdateRef(remoteTrackingRefName(remoteName, remoteRef), remoteHash); err != nil {
				return err
			}

			if needsWorktreeUpdate {
				if err := writeSymbolicHead(r, branch); err != nil {
					return err
				}
			}

			if hasLocal && localHash == remoteHash {
				fmt.Fprintf(cmd.OutOrStdout(), "already up to date (%s)\n", shortHash(remoteHash))
				return nil
			}
			if !hasLocal {
				fmt.Fprintf(cmd.OutOrStdout(), "created local branch %s at %s (%d objects fetched)\n", branch, shortHash(remoteHash), fetched)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "updated %s: %s -> %s (%d objects fetched)\n", branch, shortHash(localHash), shortHash(remoteHash), fetched)
			return nil
		},
	}
	cmd.Flags().BoolVar(&allowMerge, "merge", false, "allow a merge commit when fast-forward is not possible")
	return cmd
}

func shortHash(h object.Hash) string {
	s := string(h)
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

func temporaryPullBranch(branch string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-")
	safe := replacer.Replace(strings.TrimSpace(branch))
	if safe == "" {
		safe = "branch"
	}
	return fmt.Sprintf("__pull_%s_%d", safe, time.Now().UnixNano())
}
