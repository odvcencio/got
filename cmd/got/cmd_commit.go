package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/odvcencio/got/pkg/repo"
	"github.com/spf13/cobra"
)

func newCommitCmd() *cobra.Command {
	var message string
	var author string
	var sign bool
	var signKey string

	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Record changes to the repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" {
				return fmt.Errorf("commit message is required (-m)")
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if author == "" {
				author = os.Getenv("USER")
				if author == "" {
					author = "unknown"
				}
			}

			var (
				h          string
				commitErr  error
				signedWith string
			)
			if sign {
				signer, keyPath, signErr := newSSHCommitSigner(signKey)
				if signErr != nil {
					return signErr
				}
				signedWith = keyPath
				commitHash, cErr := r.CommitWithSigner(message, author, signer)
				h = string(commitHash)
				commitErr = cErr
			} else {
				commitHash, cErr := r.Commit(message, author)
				h = string(commitHash)
				commitErr = cErr
			}
			if commitErr != nil {
				return commitErr
			}

			// Determine current branch name for output.
			branch := "HEAD"
			head, err := r.Head()
			if err == nil && strings.HasPrefix(head, "refs/heads/") {
				branch = strings.TrimPrefix(head, "refs/heads/")
			}

			// Short hash: first 8 characters.
			short := h
			if len(short) > 8 {
				short = short[:8]
			}

			fmt.Fprintf(cmd.OutOrStdout(), "[%s %s] %s\n", branch, short, message)
			if sign {
				fmt.Fprintf(cmd.OutOrStdout(), "signed with %s\n", signedWith)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "commit message")
	cmd.Flags().StringVar(&author, "author", "", "override author (default: $USER)")
	cmd.Flags().BoolVar(&sign, "sign", false, "sign commit with SSH private key")
	cmd.Flags().StringVar(&signKey, "sign-key", "", "path to SSH private key (defaults to ~/.ssh/id_ed25519, id_ecdsa, id_rsa)")

	return cmd
}
