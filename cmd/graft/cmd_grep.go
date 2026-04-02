package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newGrepCmd() *cobra.Command {
	var caseInsensitive bool
	var fixedString bool
	var entityMode bool
	var kindFilter string
	var jsonOutput bool
	var lineMode bool
	var structural bool
	var rewrite string
	var sexp bool
	var history bool
	var since string
	var until string
	var maxCommits int

	cmd := &cobra.Command{
		Use:   "grep [-L] [-S] [-i] [-F] [--entity] [--kind <kind>] [--json] [--rewrite <template>] [--sexp] [--history] [--since <ref>] [--until <ref>] [--max-commits <n>] <pattern> [<pathspec>...]",
		Short: "Search tracked files using structural (AST-aware) pattern matching",
		Long: `Search tracked files for a pattern using structural (AST-aware) matching.

By default, patterns are matched structurally using tree-sitter. Use
metavariables ($NAME, $$$ARGS, $_) to match AST nodes:

  graft grep 'func $NAME($$$) error'
  graft grep '$X != nil'

Use -L/--line to fall back to traditional line-level grep:

  graft grep -L "TODO"
  graft grep -Li "fixme"

Use --entity to search entity names:

  graft grep --entity "Process" --kind declaration

Use --history to search across commit history instead of the working tree:

  graft grep --history 'func $NAME($$$) error'
  graft grep --history --since v1.0 --until HEAD 'func $NAME($$$)'
  graft grep --history --max-commits 50 '$X != nil'

The -i and -F flags only apply to line mode (--line) and are silently
ignored in structural mode.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// --kind without --entity is an error.
			if kindFilter != "" && !entityMode {
				return fmt.Errorf("--kind requires --entity")
			}

			// --history is incompatible with --entity, --line, and --rewrite.
			if history {
				if entityMode {
					return fmt.Errorf("--history cannot be used with --entity")
				}
				if lineMode {
					return fmt.Errorf("--history cannot be used with --line")
				}
				if rewrite != "" {
					return fmt.Errorf("--history cannot be used with --rewrite")
				}
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			// Execution flow:
			// 1. --history → history grep (structural across commits)
			// 2. --entity  → entity search (unchanged)
			// 3. --line    → line-level grep (unchanged)
			// 4. default   → structural grep (new default)
			if history {
				return runHistoryGrep(cmd, r, args, sexp, jsonOutput, since, until, maxCommits)
			}

			if entityMode {
				return runEntitySearch(cmd, r, args, caseInsensitive, kindFilter, jsonOutput)
			}

			if lineMode {
				return runLineGrep(cmd, r, args, caseInsensitive, fixedString, jsonOutput)
			}

			return runStructuralGrep(cmd, r, args, structural, sexp, rewrite, jsonOutput)
		},
	}

	cmd.Flags().BoolVarP(&caseInsensitive, "ignore-case", "i", false, "case insensitive matching (line mode only)")
	cmd.Flags().BoolVarP(&fixedString, "fixed-strings", "F", false, "interpret pattern as a fixed string (line mode only)")
	cmd.Flags().BoolVar(&entityMode, "entity", false, "search entity names instead of file content")
	cmd.Flags().StringVar(&kindFilter, "kind", "", "filter by entity kind (e.g. declaration, preamble)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output results as JSON")
	cmd.Flags().BoolVarP(&lineMode, "line", "L", false, "force line-level grep instead of structural matching")
	cmd.Flags().BoolVarP(&structural, "structural", "S", false, "force structural mode (error instead of falling back to line grep)")
	cmd.Flags().StringVar(&rewrite, "rewrite", "", "replacement template for structural rewrite mode")
	cmd.Flags().BoolVar(&sexp, "sexp", false, "treat pattern as a raw tree-sitter S-expression")
	cmd.Flags().BoolVar(&history, "history", false, "search across commit history instead of the working tree")
	cmd.Flags().StringVar(&since, "since", "", "oldest ref boundary for --history (tag, branch, or commit)")
	cmd.Flags().StringVar(&until, "until", "", "newest ref for --history (default: HEAD)")
	cmd.Flags().IntVar(&maxCommits, "max-commits", 1000, "maximum number of commits to search with --history")

	return cmd
}

func runEntitySearch(cmd *cobra.Command, r *repo.Repo, args []string, caseInsensitive bool, kindFilter string, jsonOutput bool) error {
	opts := repo.EntitySearchOptions{
		CaseInsensitive: caseInsensitive,
		KindFilter:      kindFilter,
	}

	if len(args) > 1 {
		opts.PathPattern = args[1]
	}

	results, err := r.SearchEntities(args[0], opts)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	if jsonOutput {
		jsonResults := make([]JSONEntitySearchResult, len(results))
		for i, res := range results {
			jsonResults[i] = JSONEntitySearchResult{
				Path:     res.Path,
				Name:     res.Name,
				Kind:     res.Kind,
				DeclKind: res.DeclKind,
				Key:      res.Key,
			}
		}
		return writeJSON(out, JSONEntitySearchOutput{Results: jsonResults})
	}

	for _, res := range results {
		fmt.Fprintf(out, "%s:%s:%s\n", res.Path, res.Kind, res.Name)
	}
	return nil
}

func runLineGrep(cmd *cobra.Command, r *repo.Repo, args []string, caseInsensitive, fixedString, jsonOutput bool) error {
	opts := repo.GrepOptions{
		Pattern:         args[0],
		CaseInsensitive: caseInsensitive,
		FixedString:     fixedString,
	}

	// If additional args provided, use first as path pattern.
	if len(args) > 1 {
		opts.PathPattern = args[1]
	}

	results, err := r.Grep(opts)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	if jsonOutput {
		type JSONGrepResult struct {
			Path    string `json:"path"`
			Line    int    `json:"line"`
			Content string `json:"content"`
		}
		type JSONGrepOutput struct {
			Results []JSONGrepResult `json:"results"`
		}
		jsonResults := make([]JSONGrepResult, len(results))
		for i, res := range results {
			jsonResults[i] = JSONGrepResult{
				Path:    res.Path,
				Line:    res.Line,
				Content: res.Content,
			}
		}
		return writeJSON(out, JSONGrepOutput{Results: jsonResults})
	}

	for _, res := range results {
		fmt.Fprintf(out, "%s:%d:%s\n", res.Path, res.Line, res.Content)
	}
	return nil
}

// hasMetavariables returns true if the pattern contains $ metavariable syntax,
// which indicates the user intended a structural pattern.
func hasMetavariables(pattern string) bool {
	return strings.Contains(pattern, "$")
}

func runStructuralGrep(cmd *cobra.Command, r *repo.Repo, args []string, forceStructural, sexp bool, rewrite string, jsonOutput bool) error {
	pattern := args[0]

	opts := repo.StructuralGrepOptions{
		Pattern: pattern,
		SExp:    sexp,
		Rewrite: rewrite,
	}
	if len(args) > 1 {
		opts.PathPattern = args[1]
	}

	results, err := r.StructuralGrep(opts)
	if err != nil {
		// If --structural / -S was explicitly set, always error.
		if forceStructural {
			return fmt.Errorf("structural grep failed: %w", err)
		}

		// If the pattern has no metavariables, fall back to line grep
		// with a warning.
		if !hasMetavariables(pattern) {
			fmt.Fprintf(os.Stderr, "warning: structural match failed, falling back to line grep: %v\n", err)
			return runLineGrep(cmd, r, args, false, false, jsonOutput)
		}

		return fmt.Errorf("structural grep: %w", err)
	}

	// If no results and no metavariables and not forced, fall back to line grep.
	if len(results) == 0 && !forceStructural && !hasMetavariables(pattern) && !sexp {
		fmt.Fprintf(os.Stderr, "warning: no structural matches, falling back to line grep\n")
		return runLineGrep(cmd, r, args, false, false, jsonOutput)
	}

	out := cmd.OutOrStdout()

	if jsonOutput {
		return writeJSON(out, formatStructuralJSON(results, rewrite != ""))
	}

	// Text output.
	if rewrite != "" {
		// In rewrite mode, show which files were modified.
		modified := rewrittenFiles(results)
		for _, path := range modified {
			fmt.Fprintf(out, "rewritten: %s\n", path)
		}
		fmt.Fprintf(out, "%d match(es) rewritten across %d file(s)\n", len(results), len(modified))
		return nil
	}

	for _, res := range results {
		// Header line: path:line :: entity context
		entityCtx := ""
		if res.EntityName != "" {
			entityCtx = fmt.Sprintf(" :: %s %s (%s)", res.EntityKind, res.EntityName, res.EntityKey)
		}
		fmt.Fprintf(out, "%s:%d%s\n", res.Path, res.StartLine, entityCtx)

		// Print captures indented.
		if len(res.Captures) > 0 {
			// Sort capture names for deterministic output.
			capNames := make([]string, 0, len(res.Captures))
			for name := range res.Captures {
				capNames = append(capNames, name)
			}
			sort.Strings(capNames)
			for _, name := range capNames {
				fmt.Fprintf(out, "  $%s = %s\n", name, res.Captures[name])
			}
		}
	}

	return nil
}

// rewrittenFiles returns a deduplicated, sorted list of file paths from results.
func rewrittenFiles(results []repo.StructuralGrepResult) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, r := range results {
		if !seen[r.Path] {
			seen[r.Path] = true
			paths = append(paths, r.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

// --- JSON types for structural grep output ---

// JSONStructuralGrepOutput is the top-level JSON output for structural grep.
type JSONStructuralGrepOutput struct {
	Results   []JSONStructuralGrepResult `json:"results"`
	IsRewrite bool                       `json:"isRewrite,omitempty"`
	Rewritten []string                   `json:"rewritten,omitempty"`
}

// JSONStructuralGrepResult represents a single structural match in JSON.
type JSONStructuralGrepResult struct {
	Path        string            `json:"path"`
	StartLine   int               `json:"startLine"`
	EndLine     int               `json:"endLine"`
	MatchedText string            `json:"matchedText"`
	Captures    map[string]string `json:"captures,omitempty"`
	EntityName  string            `json:"entityName,omitempty"`
	EntityKind  string            `json:"entityKind,omitempty"`
	EntityKey   string            `json:"entityKey,omitempty"`
}

func formatStructuralJSON(results []repo.StructuralGrepResult, isRewrite bool) JSONStructuralGrepOutput {
	jsonResults := make([]JSONStructuralGrepResult, len(results))
	for i, res := range results {
		jsonResults[i] = JSONStructuralGrepResult{
			Path:        res.Path,
			StartLine:   res.StartLine,
			EndLine:     res.EndLine,
			MatchedText: res.MatchedText,
			Captures:    res.Captures,
			EntityName:  res.EntityName,
			EntityKind:  res.EntityKind,
			EntityKey:   res.EntityKey,
		}
	}

	out := JSONStructuralGrepOutput{
		Results:   jsonResults,
		IsRewrite: isRewrite,
	}
	if isRewrite {
		out.Rewritten = rewrittenFiles(results)
	}
	return out
}

// --- History grep ---

func runHistoryGrep(cmd *cobra.Command, r *repo.Repo, args []string, sexp, jsonOutput bool, since, until string, maxCommits int) error {
	pattern := args[0]

	opts := repo.HistoryGrepOptions{
		Pattern:    pattern,
		SExp:       sexp,
		Since:      since,
		Until:      until,
		MaxCommits: maxCommits,
	}
	if len(args) > 1 {
		opts.PathPattern = args[1]
	}

	results, err := r.HistoryGrep(opts)
	if err != nil {
		return fmt.Errorf("history grep: %w", err)
	}

	out := cmd.OutOrStdout()

	if jsonOutput {
		return writeJSON(out, formatHistoryGrepJSON(results))
	}

	for _, res := range results {
		// Format: commit_hash_short message
		//         path:line :: entity context
		shortHash := res.CommitHash
		if len(shortHash) > 10 {
			shortHash = shortHash[:10]
		}

		entityCtx := ""
		if res.EntityName != "" {
			entityCtx = fmt.Sprintf(" :: %s %s (%s)", res.EntityKind, res.EntityName, res.EntityKey)
		}
		fmt.Fprintf(out, "%s %s\n", shortHash, res.CommitMsg)
		fmt.Fprintf(out, "  %s:%d%s\n", res.Path, res.StartLine, entityCtx)

		// Print captures indented.
		if len(res.Captures) > 0 {
			capNames := make([]string, 0, len(res.Captures))
			for name := range res.Captures {
				capNames = append(capNames, name)
			}
			sort.Strings(capNames)
			for _, name := range capNames {
				fmt.Fprintf(out, "    $%s = %s\n", name, res.Captures[name])
			}
		}
	}

	return nil
}

// --- JSON types for history grep output ---

// JSONHistoryGrepOutput is the top-level JSON output for history grep.
type JSONHistoryGrepOutput struct {
	Results []JSONHistoryGrepResult `json:"results"`
}

// JSONHistoryGrepResult represents a single structural match in a historical commit.
type JSONHistoryGrepResult struct {
	CommitHash  string            `json:"commitHash"`
	CommitMsg   string            `json:"commitMsg"`
	Path        string            `json:"path"`
	StartLine   int               `json:"startLine"`
	EndLine     int               `json:"endLine"`
	MatchedText string            `json:"matchedText"`
	Captures    map[string]string `json:"captures,omitempty"`
	EntityName  string            `json:"entityName,omitempty"`
	EntityKind  string            `json:"entityKind,omitempty"`
	EntityKey   string            `json:"entityKey,omitempty"`
}

func formatHistoryGrepJSON(results []repo.HistoryGrepResult) JSONHistoryGrepOutput {
	jsonResults := make([]JSONHistoryGrepResult, len(results))
	for i, res := range results {
		jsonResults[i] = JSONHistoryGrepResult{
			CommitHash:  res.CommitHash,
			CommitMsg:   res.CommitMsg,
			Path:        res.Path,
			StartLine:   res.StartLine,
			EndLine:     res.EndLine,
			MatchedText: res.MatchedText,
			Captures:    res.Captures,
			EntityName:  res.EntityName,
			EntityKind:  res.EntityKind,
			EntityKey:   res.EntityKey,
		}
	}
	return JSONHistoryGrepOutput{Results: jsonResults}
}
