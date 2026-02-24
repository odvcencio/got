package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/got/pkg/remote"
	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newCloneCmd() *cobra.Command {
	var remoteName string
	var branch string

	cmd := &cobra.Command{
		Use:   "clone <remote-url> [directory]",
		Short: "Clone a repository from a Got protocol endpoint",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			remoteURL := args[0]
			client, err := remote.NewClient(remoteURL)
			if err != nil {
				return err
			}

			dest := ""
			if len(args) == 2 {
				dest = args[1]
			} else {
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

			r, err := repo.Init(absDest)
			if err != nil {
				return err
			}
			if err := r.SetRemote(remoteName, remoteURL); err != nil {
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
				if _, err := remote.FetchIntoStore(cmd.Context(), client, r.Store, wants, nil); err != nil {
					return err
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

			if err := r.UpdateRef("refs/heads/"+selectedBranch, selectedHash); err != nil {
				return err
			}
			if err := writeSymbolicHead(r, selectedBranch); err != nil {
				return err
			}
			if err := r.Checkout(selectedBranch); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "cloned %s into %s\n", remoteURL, absDest)
			return nil
		},
	}

	cmd.Flags().StringVar(&remoteName, "remote-name", "origin", "name to assign to the cloned remote")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "branch to checkout after clone")
	return cmd
}
