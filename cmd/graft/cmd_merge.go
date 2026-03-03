package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/odvcencio/graft/pkg/merge"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

func newMergeCmd() *cobra.Command {
	var abortFlag bool
	var dryRunFlag bool
	var jsonFlag bool
	var aiResolveFlag bool

	cmd := &cobra.Command{
		Use:   "merge <branch>",
		Short: "Merge a branch into the current branch",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if abortFlag {
				if len(args) > 0 {
					return fmt.Errorf("--abort takes no positional arguments")
				}
				if dryRunFlag {
					return fmt.Errorf("--abort and --dry-run are mutually exclusive")
				}
				if err := r.MergeAbort(); err != nil {
					return err
				}
				if jsonFlag {
					return writeJSON(out, JSONMergeOutput{
						Action:  "abort",
						Message: "merge aborted, working tree restored",
					})
				}
				fmt.Fprintln(out, "merge aborted, working tree restored")
				return nil
			}

			if len(args) < 1 {
				return fmt.Errorf("required argument <branch> not provided")
			}
			branchName := args[0]

			current, err := r.CurrentBranch()
			if err != nil {
				return err
			}

			if dryRunFlag {
				if jsonFlag {
					return runMergePreviewJSON(r, cmd, branchName, current)
				}
				return runMergePreview(r, out, branchName, current)
			}

			if !jsonFlag {
				fmt.Fprintf(out, "merging %s into %s...\n", branchName, current)
			}

			report, err := r.Merge(branchName)
			if err != nil {
				return err
			}

			if jsonFlag {
				return mergeReportToJSON(cmd, report, "merge", branchName, current)
			}

			if report.IsFastForward {
				short := string(report.MergeCommit)
				if len(short) > 8 {
					short = short[:8]
				}
				fmt.Fprintf(out, "fast-forward %s to %s\n", current, short)
				return nil
			}

			for _, f := range report.Files {
				printFileReport(out, f)
			}

			if report.HasConflicts && aiResolveFlag {
				resolved, failed := runAIResolveAll(cmd, r, out)
				if resolved > 0 {
					fmt.Fprintf(out, "AI resolved %d conflict", resolved)
					if resolved != 1 {
						fmt.Fprint(out, "s")
					}
					fmt.Fprintln(out)
				}
				if failed > 0 {
					fmt.Fprintf(out, "AI could not resolve %d conflict", failed)
					if failed != 1 {
						fmt.Fprint(out, "s")
					}
					fmt.Fprintln(out)
					fmt.Fprintln(out, "fix remaining conflicts and run graft commit")
				} else {
					fmt.Fprintln(out, "all conflicts resolved by AI — review and run graft commit")
				}
				return nil
			}

			if report.HasConflicts {
				fmt.Fprintf(out, "merge completed with %d conflict", report.TotalConflicts)
				if report.TotalConflicts != 1 {
					fmt.Fprint(out, "s")
				}
				fmt.Fprintln(out)
				fmt.Fprintln(out, "fix conflicts and run graft commit")
			} else {
				fmt.Fprintln(out, "merge completed cleanly")
				short := string(report.MergeCommit)
				if len(short) > 8 {
					short = short[:8]
				}
				fmt.Fprintf(out, "[%s %s] Merge branch '%s'\n", current, short, branchName)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&abortFlag, "abort", false, "abort the current merge and restore original state")
	cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "preview what a merge would do without modifying anything")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().BoolVar(&aiResolveFlag, "ai-resolve", false, "attempt to resolve conflicts using AI")

	return cmd
}

// runMergePreview handles the --dry-run flag: it calls MergePreview and
// prints the report without modifying the working tree, staging, or refs.
func runMergePreview(r *repo.Repo, out io.Writer, branchName, current string) error {
	fmt.Fprintf(out, "previewing merge of %s into %s...\n", branchName, current)

	report, err := r.MergePreview(branchName)
	if err != nil {
		return err
	}

	if report.IsFastForward {
		fmt.Fprintf(out, "merge would fast-forward %s\n", current)
		return nil
	}

	for _, f := range report.Files {
		printFileReport(out, f)
	}

	if report.HasConflicts {
		fmt.Fprintf(out, "merge would produce %d conflict", report.TotalConflicts)
		if report.TotalConflicts != 1 {
			fmt.Fprint(out, "s")
		}
		fmt.Fprintln(out)
	} else {
		fmt.Fprintln(out, "merge would complete cleanly")
	}

	return nil
}

func printFileReport(out io.Writer, f repo.FileMergeReport) {
	switch f.Status {
	case "conflict":
		fmt.Fprintf(out, "  %s: CONFLICT — %d conflict", f.Path, f.ConflictCount)
		if f.ConflictCount != 1 {
			fmt.Fprint(out, "s")
		}
		fmt.Fprintln(out)
		for _, ec := range f.EntityConflicts {
			fmt.Fprintf(out, "    %s: %s\n", ec.Name, humanConflictType(ec.Type))
		}
	case "added":
		fmt.Fprintf(out, "  %s: %d entities (added)\n", f.Path, f.EntityCount)
	case "deleted":
		fmt.Fprintf(out, "  %s: deleted\n", f.Path)
	default: // "clean"
		fmt.Fprintf(out, "  %s: clean\n", f.Path)
	}
}

// humanConflictType returns a human-readable label for a conflict type string.
// Conflict type values are defined as merge.ConflictTypeBothModified and
// merge.ConflictTypeDeleteVsModify in pkg/merge/match.go.
func humanConflictType(ct string) string {
	switch ct {
	case "both_modified":
		return "both modified"
	case "delete_vs_modify":
		return "delete vs modify"
	case "rename_conflict":
		return "rename conflict"
	default:
		return ct
	}
}

// mergeReportToJSON converts a MergeReport to JSON output.
func mergeReportToJSON(cmd *cobra.Command, report *repo.MergeReport, action, source, target string) error {
	result := JSONMergeOutput{
		Action:         action,
		Source:         source,
		Target:         target,
		IsFastForward:  report.IsFastForward,
		HasConflicts:   report.HasConflicts,
		TotalConflicts: report.TotalConflicts,
		MergeCommit:    string(report.MergeCommit),
		Files:          make([]JSONMergeFile, 0),
	}

	for _, f := range report.Files {
		jf := JSONMergeFile{
			Path:          f.Path,
			Status:        f.Status,
			EntityCount:   f.EntityCount,
			ConflictCount: f.ConflictCount,
		}
		for _, ec := range f.EntityConflicts {
			jf.EntityConflicts = append(jf.EntityConflicts, JSONEntityConflict{
				Name: ec.Name,
				Type: ec.Type,
			})
		}
		result.Files = append(result.Files, jf)
	}

	return writeJSON(cmd.OutOrStdout(), result)
}

// runMergePreviewJSON handles --dry-run --json: runs MergePreview and writes JSON.
func runMergePreviewJSON(r *repo.Repo, cmd *cobra.Command, branchName, current string) error {
	report, err := r.MergePreview(branchName)
	if err != nil {
		return err
	}
	return mergeReportToJSON(cmd, report, "preview", branchName, current)
}

// runAIResolveAll attempts to resolve all conflicts in the working tree using
// AI. It returns counts of resolved and failed conflicts.
func runAIResolveAll(cmd *cobra.Command, r *repo.Repo, out io.Writer) (resolved, failed int) {
	resolver, err := newAIResolverFromConfig()
	if err != nil {
		fmt.Fprintf(out, "AI resolve: %v\n", err)
		return 0, 0
	}

	conflicts, err := r.ListConflicts()
	if err != nil {
		fmt.Fprintf(out, "AI resolve: list conflicts: %v\n", err)
		return 0, 0
	}

	ctx := context.Background()
	for _, c := range conflicts {
		if c.EntityName == "" {
			// Skip non-structural (text) conflicts — no entity context.
			failed++
			continue
		}

		ok := aiResolveConflict(ctx, resolver, r, c, out)
		if ok {
			resolved++
		} else {
			failed++
		}
	}

	return resolved, failed
}

// aiResolveConflict attempts to resolve a single conflict entry using AI.
// Returns true if the conflict was resolved and the file updated.
func aiResolveConflict(ctx context.Context, resolver merge.AIResolver, r *repo.Repo, c repo.ConflictEntry, out io.Writer) bool {
	// Read the conflicted file to extract base/ours/theirs from staging.
	baseBody, oursBody, theirsBody, err := r.ReadConflictBodies(c.Path)
	if err != nil {
		fmt.Fprintf(out, "  %s (%s): skip — %v\n", c.Path, c.EntityName, err)
		return false
	}

	language := merge.DetectLanguage(c.Path)

	req := merge.AIResolveRequest{
		FilePath:   c.Path,
		EntityKey:  c.EntityKey,
		EntityKind: c.EntityKind,
		Language:   language,
		BaseBody:   baseBody,
		OursBody:   oursBody,
		TheirsBody: theirsBody,
	}

	result, err := resolver.Resolve(ctx, req)
	if err != nil {
		fmt.Fprintf(out, "  %s (%s): AI error — %v\n", c.Path, c.EntityName, err)
		return false
	}

	fmt.Fprintf(out, "  %s (%s): resolved (confidence: %.0f%%) — %s\n",
		c.Path, c.EntityName, result.Confidence*100, result.Explanation)

	// Write the resolved content back by replacing the conflict markers in the file.
	if err := r.ApplyAIResolution(c.Path, c.EntityName, result.ResolvedBody); err != nil {
		fmt.Fprintf(out, "  %s (%s): apply failed — %v\n", c.Path, c.EntityName, err)
		return false
	}

	return true
}

// newAIResolverFromConfig creates an AIResolver from user config and environment.
func newAIResolverFromConfig() (merge.AIResolver, error) {
	cfg, err := userconfig.Load()
	if err != nil {
		// Fall back to env var only.
		cfg = &userconfig.Config{}
	}

	provider := cfg.AIProvider
	if provider == "" {
		provider = os.Getenv("GRAFT_AI_PROVIDER")
	}
	if provider == "" {
		provider = "claude"
	}

	switch strings.ToLower(provider) {
	case "claude", "anthropic", "":
		return merge.NewClaudeResolver(cfg.AIAPIKey, cfg.AIModel)
	default:
		return nil, fmt.Errorf("unsupported AI provider: %s", provider)
	}
}
