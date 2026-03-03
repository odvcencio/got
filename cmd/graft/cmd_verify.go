package main

import (
	"fmt"

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
