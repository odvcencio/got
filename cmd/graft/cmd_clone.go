package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newCloneCmd() *cobra.Command {
	var remoteName string
	var branch string
	var bootstrapGot bool
	var depth int

	cmd := &cobra.Command{
		Use:   "clone <remote-url> [directory]",
		Short: "Clone a repository from Graft/Git endpoints or local path",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := strings.TrimSpace(args[0])
			localSourceRoot, isLocalSource, err := resolveLocalCloneSource(source)
			if err != nil {
				return err
			}
			remoteSource := source
			remoteKind := remoteTransportGraft
			if !isLocalSource {
				remoteKind, remoteSource, err = parseRemoteSpec(source)
				if err != nil {
					return fmt.Errorf("invalid remote URL %q: %w", source, err)
				}
			}

			dest := ""
			if len(args) == 2 {
				dest = args[1]
			} else if isLocalSource {
				dest = filepath.Base(localSourceRoot)
			} else if remoteKind == remoteTransportGit {
				dest = inferRepoNameFromRemote(remoteSource)
			} else {
				client, err := remote.NewClient(remoteSource)
				if err != nil {
					return err
				}
				dest = client.Endpoint().Repo
			}
			if strings.TrimSpace(dest) == "" {
				return fmt.Errorf("destination directory is required")
			}
			absDest, err := filepath.Abs(dest)
			if err != nil {
				return fmt.Errorf("resolve destination: %w", err)
			}
			if err := ensureEmptyDir(absDest); err != nil {
				return err
			}

			if depth > 0 && isLocalSource {
				return fmt.Errorf("--depth is not supported for local clone sources")
			}

			if isLocalSource {
				return cloneFromLocalSource(cmd, localSourceRoot, source, absDest, remoteName, branch)
			}
			if remoteKind == remoteTransportGit {
				return cloneFromGitRemote(cmd, remoteSource, absDest, remoteName, branch, bootstrapGot)
			}

			client, err := remote.NewClient(remoteSource)
			if err != nil {
				return err
			}
			r, err := repo.Init(absDest)
			if err != nil {
				return err
			}
			if err := r.SetRemote(remoteName, remoteSource); err != nil {
				return err
			}

			remoteRefs, err := client.ListRefs(cmd.Context())
			if err != nil {
				return err
			}

			// Fetch all advertised refs so clone has complete object coverage.
			wants := make([]object.Hash, 0, len(remoteRefs))
			for _, h := range remoteRefs {
				if strings.TrimSpace(string(h)) != "" {
					wants = append(wants, h)
				}
			}
			if len(wants) > 0 {
				cfg := remote.FetchConfig{
					Depth: depth,
				}
				result, err := remote.FetchIntoStoreShallow(cmd.Context(), client, r.Store, wants, nil, cfg)
				if err != nil {
					return err
				}
				// Write shallow boundaries if this is a shallow clone.
				if depth > 0 && result.ShallowState != nil && result.ShallowState.Len() > 0 {
					if err := remote.WriteShallowFile(r.GraftDir, result.ShallowState); err != nil {
						return fmt.Errorf("write shallow file: %w", err)
					}
				}
			}

			for name, h := range remoteRefs {
				if err := r.UpdateRef(remoteTrackingRefName(remoteName, name), h); err != nil {
					return err
				}
			}

			if len(remoteRefs) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "cloned empty repository into %s\n", absDest)
				return nil
			}

			selectedBranch := strings.TrimSpace(branch)
			var selectedHash object.Hash
			if selectedBranch == "" {
				var ok bool
				selectedBranch, selectedHash, ok = chooseDefaultBranch(remoteRefs)
				if !ok {
					fmt.Fprintf(cmd.OutOrStdout(), "cloned repository into %s (no branch heads found)\n", absDest)
					return nil
				}
			} else {
				h, ok := remoteRefs["heads/"+selectedBranch]
				if !ok || strings.TrimSpace(string(h)) == "" {
					return fmt.Errorf("remote branch %q not found", selectedBranch)
				}
				selectedHash = h
			}

			// First checkout by commit hash while HEAD still points to an
			// unborn branch, so clean-tree checks do not fail on initial clone.
			if err := r.Checkout(string(selectedHash)); err != nil {
				return err
			}
			if err := r.UpdateRef("refs/heads/"+selectedBranch, selectedHash); err != nil {
				return err
			}
			if err := writeSymbolicHead(r, selectedBranch); err != nil {
				return err
			}

			// Fetch any LFS objects referenced by the checked-out tree.
			lfsClient := remote.NewLFSClient(client)
			lfsCount, lfsErr := r.FetchLFSObjects(cmd.Context(), lfsClient)
			if lfsErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: LFS fetch failed: %v\n", lfsErr)
			} else if lfsCount > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "fetched %d LFS objects\n", lfsCount)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "cloned %s into %s\n", remoteSource, absDest)
			return nil
		},
	}

	cmd.Flags().StringVar(&remoteName, "remote-name", "origin", "name to assign to the cloned remote")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "branch to checkout after clone")
	cmd.Flags().BoolVar(&bootstrapGot, "bootstrap-graft", true, "initialize .graft repository from cloned git HEAD snapshot")
	cmd.Flags().IntVar(&depth, "depth", 0, "create a shallow clone with history truncated to the specified number of commits")
	return cmd
}

func resolveLocalCloneSource(source string) (string, bool, error) {
	if looksLikeRemoteURL(source) {
		return "", false, nil
	}
	absSource, err := filepath.Abs(source)
	if err != nil {
		return "", false, fmt.Errorf("resolve source: %w", err)
	}
	srcRepo, err := repo.Open(absSource)
	if err != nil {
		return "", false, nil
	}
	return srcRepo.RootDir, true, nil
}

func cloneFromLocalSource(cmd *cobra.Command, sourceRoot, sourceSpec, absDest, remoteName, branch string) error {
	srcGraftDir := filepath.Join(sourceRoot, ".graft")
	dstGraftDir := filepath.Join(absDest, ".graft")
	if err := copyDir(srcGraftDir, dstGraftDir); err != nil {
		return err
	}

	r, err := repo.Open(absDest)
	if err != nil {
		return err
	}
	if err := r.SetRemote(remoteName, sourceSpec); err != nil {
		return err
	}

	selectedBranch := strings.TrimSpace(branch)
	if selectedBranch != "" {
		if _, err := r.ResolveRef("refs/heads/" + selectedBranch); err != nil {
			return fmt.Errorf("local branch %q not found", selectedBranch)
		}
		if err := r.Checkout(selectedBranch); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "cloned %s into %s\n", sourceSpec, absDest)
		return nil
	}

	if currentBranch, err := r.CurrentBranch(); err == nil && strings.TrimSpace(currentBranch) != "" {
		if err := r.Checkout(currentBranch); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "cloned %s into %s\n", sourceSpec, absDest)
		return nil
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}
	if err := r.Checkout(string(headHash)); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "cloned %s into %s\n", sourceSpec, absDest)
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
