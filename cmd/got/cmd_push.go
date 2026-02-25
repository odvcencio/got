package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/got/pkg/remote"
	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newPushCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "push [remote] [branch]",
		Short: "Push a local branch or ref to a remote",
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
				return pushViaGit(cmd, r, remoteURL, branch, force)
			}
			return pushBranchGot(cmd, r, remoteName, remoteURL, branch, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "allow non-fast-forward update")
	return cmd
}

func pushBranchGot(cmd *cobra.Command, r *repo.Repo, remoteName, remoteURL, branch string, force bool) error {
	pushTarget, localRef, remoteRef, err := resolvePushRefNames(r, branch)
	if err != nil {
		return err
	}
	localHash, err := r.ResolveRef(localRef)
	if err != nil {
		return fmt.Errorf("resolve local ref %q: %w", localRef, err)
	}

	client, err := remote.NewClient(remoteURL)
	if err != nil {
		return err
	}
	remoteRefs, err := client.ListRefs(cmd.Context())
	if err != nil {
		return err
	}

	remoteHash, hasRemote := remoteRefs[remoteRef]
	if hasRemote && strings.TrimSpace(string(remoteHash)) == "" {
		hasRemote = false
	}

	if hasRemote && remoteHash == localHash {
		_ = r.UpdateRef(remoteTrackingRefName(remoteName, remoteRef), remoteHash)
		fmt.Fprintf(cmd.OutOrStdout(), "everything up-to-date (%s)\n", shortHash(localHash))
		return nil
	}

	if hasRemote && !force {
		if strings.HasPrefix(remoteRef, "heads/") {
			if !r.Store.Has(remoteHash) {
				haves, err := localRefTips(r)
				if err != nil {
					return err
				}
				if _, err := remote.FetchIntoStore(cmd.Context(), client, r.Store, []object.Hash{remoteHash}, haves); err != nil {
					return fmt.Errorf("push safety check failed fetching remote head: %w", err)
				}
			}
			base, err := r.FindMergeBase(localHash, remoteHash)
			if err != nil {
				return fmt.Errorf("push safety check failed: %w", err)
			}
			if base != remoteHash {
				return fmt.Errorf("push rejected: non-fast-forward (local %s does not contain remote %s)", shortHash(localHash), shortHash(remoteHash))
			}
		} else if remoteHash != localHash {
			return fmt.Errorf("push rejected: remote %s already exists at %s (use --force to overwrite)", remoteRef, shortHash(remoteHash))
		}
	}

	stopRoots := make([]object.Hash, 0, len(remoteRefs))
	for _, h := range remoteRefs {
		if strings.TrimSpace(string(h)) == "" {
			continue
		}
		if r.Store.Has(h) {
			stopRoots = append(stopRoots, h)
		}
	}

	objectsToPush, err := remote.CollectObjectsForPush(r.Store, []object.Hash{localHash}, stopRoots)
	if err != nil {
		return err
	}
	uploaded, err := pushObjectsChunked(cmd.Context(), client, objectsToPush)
	if err != nil {
		return err
	}

	old := object.Hash("")
	if hasRemote {
		old = remoteHash
	}
	newHash := localHash
	updated, err := client.UpdateRefs(cmd.Context(), []remote.RefUpdate{{
		Name: remoteRef,
		Old:  &old,
		New:  &newHash,
	}})
	if err != nil {
		return err
	}

	finalHash := localHash
	if h, ok := updated[remoteRef]; ok && strings.TrimSpace(string(h)) != "" {
		finalHash = h
	}
	if err := r.UpdateRef(remoteTrackingRefName(remoteName, remoteRef), finalHash); err != nil {
		return err
	}

	if hasRemote {
		fmt.Fprintf(cmd.OutOrStdout(), "pushed %s: %s -> %s (%d objects)\n", pushTarget, shortHash(remoteHash), shortHash(finalHash), uploaded)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "pushed new %s at %s (%d objects)\n", pushTarget, shortHash(finalHash), uploaded)
	return nil
}

func resolvePushRefNames(r *repo.Repo, branchArg string) (display string, localRef string, remoteRef string, err error) {
	branchArg = strings.TrimSpace(branchArg)
	if branchArg == "" {
		branchArg, err = r.CurrentBranch()
		if err != nil {
			return "", "", "", err
		}
		if branchArg == "" {
			return "", "", "", fmt.Errorf("cannot infer branch while HEAD is detached; specify branch or full ref")
		}
	}

	if strings.HasPrefix(branchArg, "refs/heads/") {
		name := strings.TrimPrefix(branchArg, "refs/heads/")
		if strings.TrimSpace(name) == "" {
			return "", "", "", fmt.Errorf("invalid branch ref %q", branchArg)
		}
		return "branch " + name, branchArg, "heads/" + name, nil
	}
	if strings.HasPrefix(branchArg, "refs/tags/") {
		name := strings.TrimPrefix(branchArg, "refs/tags/")
		if strings.TrimSpace(name) == "" {
			return "", "", "", fmt.Errorf("invalid tag ref %q", branchArg)
		}
		return "tag " + name, branchArg, "tags/" + name, nil
	}
	if strings.HasPrefix(branchArg, "refs/") {
		return "", "", "", fmt.Errorf("unsupported ref %q (only refs/heads/* and refs/tags/* are supported)", branchArg)
	}
	return "branch " + branchArg, "refs/heads/" + branchArg, "heads/" + branchArg, nil
}

func pushObjectsChunked(ctx context.Context, client *remote.Client, objects []remote.ObjectRecord) (int, error) {
	const (
		maxChunkObjects = 2000
		maxChunkBytes   = 32 << 20
		maxObjectBytes  = 16 << 20
	)
	if len(objects) == 0 {
		return 0, nil
	}

	chunk := make([]remote.ObjectRecord, 0, maxChunkObjects)
	chunkBytes := 0
	uploaded := 0

	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}
		if err := client.PushObjects(ctx, chunk); err != nil {
			return err
		}
		uploaded += len(chunk)
		chunk = chunk[:0]
		chunkBytes = 0
		return nil
	}

	for _, obj := range objects {
		if len(obj.Data) > maxObjectBytes {
			return uploaded, fmt.Errorf("object %s exceeds %d-byte push limit", shortHash(obj.Hash), maxObjectBytes)
		}
		recBytes := len(obj.Data) + 128
		if len(chunk) > 0 && (len(chunk) >= maxChunkObjects || chunkBytes+recBytes > maxChunkBytes) {
			if err := flush(); err != nil {
				return uploaded, err
			}
		}
		chunk = append(chunk, obj)
		chunkBytes += recBytes
	}
	if err := flush(); err != nil {
		return uploaded, err
	}
	return uploaded, nil
}
