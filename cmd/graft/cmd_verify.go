package main

import (
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newVerifyCmd() *cobra.Command {
	var signatures bool
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify object integrity and commit signatures",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if signatures {
				return verifyBranchSignatures(cmd, r, jsonFlag)
			}

			// Default: verify object store integrity.
			report, err := r.Store.Verify()
			if err != nil {
				return err
			}

			if jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONVerifyOutput{
					LooseObjects: report.LooseObjects,
					PackFiles:    report.PackFiles,
					PackObjects:  report.PackObjects,
				})
			}

			fmt.Fprintf(
				cmd.OutOrStdout(),
				"ok: verified %d loose object(s), %d pack file(s), %d packed object(s)\n",
				report.LooseObjects,
				report.PackFiles,
				report.PackObjects,
			)
			return nil
		},
	}

	cmd.Flags().BoolVar(&signatures, "signatures", false, "Verify commit signatures on current branch (up to 100)")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")

	// Add the "commit" subcommand.
	cmd.AddCommand(newVerifyCommitCmd())
	cmd.AddCommand(newVerifyPushLimitsCmd())

	return cmd
}

func newVerifyCommitCmd() *cobra.Command {
	var jsonFlag bool

	cmd := &cobra.Command{
		Use:   "commit <hash>",
		Short: "Verify a single commit's signature",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			hash := object.Hash(args[0])
			result, err := r.VerifyCommitSignature(hash)
			if err != nil {
				return err
			}

			if jsonFlag {
				return writeJSON(cmd.OutOrStdout(), JSONVerifyOutput{
					Results: []JSONVerifyResult{verifyResultToJSON(result)},
				})
			}

			printVerificationResult(cmd, result)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")

	return cmd
}

func newVerifyPushLimitsCmd() *cobra.Command {
	var jsonFlag bool
	var remoteName string

	cmd := &cobra.Command{
		Use:   "push-limits [ref]",
		Short: "Check whether a ref can be pushed under the graft object-size limits",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			refName := ""
			if len(args) > 0 {
				refName = args[0]
			}
			pushTarget, localRef, remoteRef, err := resolvePushRefNames(r, refName)
			if err != nil {
				return err
			}

			remoteURL := ""
			if strings.TrimSpace(remoteName) != "" {
				name, url, transport, err := resolveRemoteNameAndSpec(r, remoteName)
				if err != nil {
					return err
				}
				if transport == remoteTransportGit {
					return fmt.Errorf("verify push-limits currently supports orchard/graft remotes only")
				}
				remoteName = name
				remoteURL = url
			}

			report, err := collectPushLimitReport(cmd.Context(), r, pushTarget, localRef, remoteName, remoteURL, remoteRef)
			if err != nil {
				return err
			}

			if jsonFlag {
				return writeJSON(cmd.OutOrStdout(), jsonVerifyPushLimitReport(report))
			}

			if err := pushLimitError(report); err != nil {
				return err
			}
			printPushLimitSummary(cmd.OutOrStdout(), report)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().StringVar(&remoteName, "remote", "", "remote name or orchard/graft URL used to compute the push set")
	return cmd
}

func verifyBranchSignatures(cmd *cobra.Command, r *repo.Repo, jsonFlag bool) error {
	results, err := r.VerifyBranchSignatures(100)
	if err != nil {
		return err
	}

	if jsonFlag {
		jsonResults := make([]JSONVerifyResult, len(results))
		for i := range results {
			jsonResults[i] = verifyResultToJSON(&results[i])
		}
		return writeJSON(cmd.OutOrStdout(), JSONVerifyOutput{
			Results: jsonResults,
		})
	}

	for i := range results {
		printVerificationResult(cmd, &results[i])
	}
	return nil
}

func verifyResultToJSON(result *repo.VerificationResult) JSONVerifyResult {
	return JSONVerifyResult{
		CommitHash: string(result.CommitHash),
		Valid:      result.Valid,
		Unsigned:   result.Unsigned,
		SignerKey:  result.SignerKey,
		Algorithm:  result.Algorithm,
		Error:      result.Error,
	}
}

func printVerificationResult(cmd *cobra.Command, result *repo.VerificationResult) {
	short := string(result.CommitHash)
	if len(short) > 8 {
		short = short[:8]
	}

	if result.Unsigned {
		fmt.Fprintf(cmd.OutOrStdout(), "No signature on commit %s\n", short)
		return
	}

	if result.Valid {
		fmt.Fprintf(cmd.OutOrStdout(), "Good signature (%s) on commit %s\n", result.Algorithm, short)
		return
	}

	fmt.Fprintf(cmd.OutOrStdout(), "BAD signature on commit %s: %s\n", short, result.Error)
}
