package main

import (
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newFetchCmd() *cobra.Command {
	var depth int
	var deepen int

	cmd := &cobra.Command{
		Use:   "fetch [remote]",
		Short: "Download objects and refs from a remote",
		Long:  "Fetch downloads objects and refs from a remote without modifying the working tree or current branch. Remote refs are stored under refs/remotes/<remote>/.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			remoteName := "origin"
			if len(args) == 1 {
				remoteName = args[0]
			}

			if depth > 0 || deepen > 0 {
				return fetchShallow(cmd, r, remoteName, depth, deepen)
			}

			result, err := r.FetchContext(cmd.Context(), remoteName)
			if err != nil {
				return err
			}

			if len(result.UpdatedRefs) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "already up to date\n")
				return nil
			}

			for _, ru := range result.UpdatedRefs {
				if ru.OldHash == "" {
					fmt.Fprintf(cmd.OutOrStdout(), " * [new ref] %s -> %s\n", shortHash(ru.NewHash), ru.Name)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "   %s..%s %s\n", shortHash(ru.OldHash), shortHash(ru.NewHash), ru.Name)
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "fetched %d objects from %s\n", result.ObjectCount, result.RemoteName)

			// Fetch any LFS objects referenced by the staging index.
			remoteURL, urlErr := r.RemoteURL(remoteName)
			if urlErr == nil {
				if client, clientErr := remote.NewClient(remoteURL); clientErr == nil {
					lfsClient := remote.NewLFSClient(client)
					lfsCount, lfsErr := r.FetchLFSObjects(cmd.Context(), lfsClient)
					if lfsErr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: LFS fetch failed: %v\n", lfsErr)
					} else if lfsCount > 0 {
						fmt.Fprintf(cmd.OutOrStdout(), "fetched %d LFS objects\n", lfsCount)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&depth, "depth", 0, "limit fetching to the specified number of commits from tip")
	cmd.Flags().IntVar(&deepen, "deepen", 0, "deepen a shallow clone by the specified number of commits")

	return cmd
}

func fetchShallow(cmd *cobra.Command, r *repo.Repo, remoteName string, depth, deepenN int) error {
	remoteURL, err := r.RemoteURL(remoteName)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	client, err := remote.NewClient(remoteURL)
	if err != nil {
		return fmt.Errorf("fetch: create client: %w", err)
	}

	remoteRefs, err := client.ListRefs(cmd.Context())
	if err != nil {
		return fmt.Errorf("fetch: list remote refs: %w", err)
	}

	if len(remoteRefs) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "already up to date\n")
		return nil
	}

	// Read existing shallow state.
	shallowState, err := remote.ReadShallowFile(r.GraftDir)
	if err != nil {
		return fmt.Errorf("fetch: read shallow state: %w", err)
	}

	wants := make([]object.Hash, 0, len(remoteRefs))
	for _, h := range remoteRefs {
		if strings.TrimSpace(string(h)) != "" {
			wants = append(wants, h)
		}
	}

	haves, err := localRefTips(r)
	if err != nil {
		return fmt.Errorf("fetch: collect local refs: %w", err)
	}

	cfg := remote.FetchConfig{
		Depth:        depth,
		Deepen:       deepenN,
		ShallowState: shallowState,
	}

	result, err := remote.FetchIntoStoreShallow(cmd.Context(), client, r.Store, wants, haves, cfg)
	if err != nil {
		return fmt.Errorf("fetch: download objects: %w", err)
	}

	// Update shallow file.
	if result.ShallowState != nil && result.ShallowState.Len() > 0 {
		if err := remote.WriteShallowFile(r.GraftDir, result.ShallowState); err != nil {
			return fmt.Errorf("fetch: write shallow state: %w", err)
		}
	}

	// Update tracking refs.
	var updatedRefs []repo.RefUpdate
	for refName, h := range remoteRefs {
		trackingRef := fmt.Sprintf("refs/remotes/%s/%s", remoteName, strings.TrimPrefix(refName, "/"))
		oldHash, _ := r.ResolveRef(trackingRef)
		if oldHash == h {
			continue
		}
		if err := r.UpdateRef(trackingRef, h); err != nil {
			return fmt.Errorf("fetch: update tracking ref %q: %w", trackingRef, err)
		}
		updatedRefs = append(updatedRefs, repo.RefUpdate{
			Name:    trackingRef,
			OldHash: oldHash,
			NewHash: h,
		})
	}

	if len(updatedRefs) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "already up to date\n")
		return nil
	}

	for _, ru := range updatedRefs {
		if ru.OldHash == "" {
			fmt.Fprintf(cmd.OutOrStdout(), " * [new ref] %s -> %s\n", shortHash(ru.NewHash), ru.Name)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "   %s..%s %s\n", shortHash(ru.OldHash), shortHash(ru.NewHash), ru.Name)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "fetched %d objects from %s\n", result.Written, remoteName)
	return nil
}
