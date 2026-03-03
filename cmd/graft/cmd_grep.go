package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newGrepCmd() *cobra.Command {
	var caseInsensitive bool
	var fixedString bool
	var entityMode bool
	var kindFilter string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "grep [-i] [-F] [--entity] [--kind <kind>] [--json] <pattern> [<pathspec>...]",
		Short: "Search tracked files for a pattern",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// --kind without --entity is an error.
			if kindFilter != "" && !entityMode {
				return fmt.Errorf("--kind requires --entity")
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if entityMode {
				return runEntitySearch(cmd, r, args, caseInsensitive, kindFilter, jsonOutput)
			}

			return runLineGrep(cmd, r, args, caseInsensitive, fixedString, jsonOutput)
		},
	}

	cmd.Flags().BoolVarP(&caseInsensitive, "ignore-case", "i", false, "case insensitive matching")
	cmd.Flags().BoolVarP(&fixedString, "fixed-strings", "F", false, "interpret pattern as a fixed string")
	cmd.Flags().BoolVar(&entityMode, "entity", false, "search entity names instead of file content")
	cmd.Flags().StringVar(&kindFilter, "kind", "", "filter by entity kind (e.g. declaration, preamble)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output results as JSON")

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
