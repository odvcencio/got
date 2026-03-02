package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newCommitCmd() *cobra.Command {
	var message string
	var author string
	var sign bool
	var signKey string
	var noSign bool
	var amend bool

	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Record changes to the repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" && !amend {
				return fmt.Errorf("commit message is required (-m)")
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if author == "" {
				author = r.ResolveAuthor()
			}

			// Determine whether to sign. Explicit --sign/--sign-key flags
			// take highest priority, then --no-sign disables, otherwise
			// fall back to user config auto-sign.
			shouldSign := sign
			resolvedKey := signKey
			autoSigned := false

			if !sign && !noSign {
				// Check user config for auto-signing.
				cfg := loadUserConfig()
				if cfg.AutoSign && cfg.SigningKeyPath != "" {
					if _, err := os.Stat(cfg.SigningKeyPath); err == nil {
						shouldSign = true
						resolvedKey = cfg.SigningKeyPath
						autoSigned = true
					}
				}
			}

			var (
				h          string
				commitErr  error
				signedWith string
			)
			if amend {
				if shouldSign {
					signer, keyPath, signErr := newSSHCommitSigner(resolvedKey)
					if signErr != nil {
						return signErr
					}
					signedWith = keyPath
					if autoSigned {
						signedWith = resolvedKey
					}
					commitHash, cErr := r.CommitAmendWithSigner(message, author, signer)
					h = string(commitHash)
					commitErr = cErr
				} else {
					commitHash, cErr := r.CommitAmend(message, author)
					h = string(commitHash)
					commitErr = cErr
				}
			} else if shouldSign {
				signer, keyPath, signErr := newSSHCommitSigner(resolvedKey)
				if signErr != nil {
					return signErr
				}
				signedWith = keyPath
				if autoSigned {
					signedWith = resolvedKey
				}
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

			// For amend with empty message, read back the actual message.
			if amend && message == "" {
				headHash, err := r.ResolveRef("HEAD")
				if err == nil {
					if c, err := r.Store.ReadCommit(headHash); err == nil {
						message = c.Message
					}
				}
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
			if shouldSign {
				fmt.Fprintf(cmd.OutOrStdout(), "signed with %s\n", signedWith)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "commit message")
	cmd.Flags().StringVar(&author, "author", "", "override author (default: from config)")
	cmd.Flags().BoolVar(&sign, "sign", false, "sign commit with SSH private key")
	cmd.Flags().StringVar(&signKey, "sign-key", "", "path to SSH private key (defaults to ~/.ssh/id_ed25519, id_ecdsa, id_rsa)")
	cmd.Flags().BoolVar(&noSign, "no-sign", false, "disable auto-signing even if configured")
	cmd.Flags().BoolVar(&amend, "amend", false, "replace the tip of the current branch by creating a new commit")

	return cmd
}
