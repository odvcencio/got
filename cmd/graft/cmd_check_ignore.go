package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/odvcencio/graft/pkg/gitbridge"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

type checkIgnoreResult struct {
	Path  string                  `json:"path"`
	Graft *repo.IgnoreExplanation `json:"graft,omitempty"`
	Git   *repo.IgnoreExplanation `json:"git,omitempty"`
}

type checkIgnoreOutput struct {
	Results []checkIgnoreResult `json:"results"`
}

func newCheckIgnoreCmd() *cobra.Command {
	var jsonFlag bool
	var verbose bool
	var graftOnly bool
	var gitOnly bool

	cmd := &cobra.Command{
		Use:   "check-ignore <path> [path...]",
		Short: "Explain which ignore rules apply to one or more paths",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if graftOnly && gitOnly {
				return fmt.Errorf("--graft-only and --git-only cannot be used together")
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			includeGraft := !gitOnly
			includeGit := !graftOnly && gitbridge.DetectGitRepo(r.RootDir)
			if gitOnly && !includeGit {
				return fmt.Errorf("git diagnostics require a colocated .git repository")
			}

			var checker *repo.IgnoreChecker
			if includeGraft {
				checker = repo.NewIgnoreChecker(r.RootDir)
			}

			results := make([]checkIgnoreResult, 0, len(args))
			for _, input := range args {
				rel, err := resolveRepoRelativePath(r.RootDir, input)
				if err != nil {
					return err
				}

				result := checkIgnoreResult{Path: rel}
				if includeGraft {
					explanation := graftIgnoreExplanation(checker, rel)
					result.Graft = &explanation
				}
				if includeGit {
					explanation, err := gitIgnoreExplanation(r.RootDir, rel)
					if err != nil {
						return err
					}
					result.Git = explanation
				}
				results = append(results, result)
			}

			if jsonFlag {
				return writeJSON(cmd.OutOrStdout(), checkIgnoreOutput{Results: results})
			}

			for i, result := range results {
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				fmt.Fprintf(cmd.OutOrStdout(), "path: %s\n", result.Path)
				if result.Graft != nil {
					printIgnoreExplanation(cmd.OutOrStdout(), "graft", *result.Graft, verbose)
				}
				if result.Git != nil {
					printIgnoreExplanation(cmd.OutOrStdout(), "git", *result.Git, verbose)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output in JSON format")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show every matching rule instead of only the final decision")
	cmd.Flags().BoolVar(&graftOnly, "graft-only", false, "only show graft ignore rules")
	cmd.Flags().BoolVar(&gitOnly, "git-only", false, "only show git ignore rules")
	return cmd
}

func printIgnoreExplanation(w io.Writer, engine string, explanation repo.IgnoreExplanation, verbose bool) {
	state := "not ignored"
	if explanation.Ignored {
		state = "ignored"
	}
	fmt.Fprintf(w, "%s: %s\n", engine, state)

	if explanation.Final != nil {
		fmt.Fprintf(w, "  final: %s\n", formatIgnoreMatch(*explanation.Final))
	} else {
		fmt.Fprintln(w, "  final: no matching rule")
	}
	if explanation.MatchedPath != "" && explanation.MatchedPath != explanation.Path {
		fmt.Fprintf(w, "  matched path: %s\n", explanation.MatchedPath)
	}
	if verbose && len(explanation.Matches) > 1 {
		fmt.Fprintln(w, "  matches:")
		for _, match := range explanation.Matches {
			fmt.Fprintf(w, "    %s\n", formatIgnoreMatch(match))
		}
	}
}

func formatIgnoreMatch(match repo.IgnoreMatch) string {
	location := match.Source
	if strings.TrimSpace(location) == "" {
		location = "unknown"
	}
	if match.Line > 0 {
		location = fmt.Sprintf("%s:%d", location, match.Line)
	}
	return fmt.Sprintf("%s %q", location, match.Pattern)
}

func resolveRepoRelativePath(rootDir, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("path is required")
	}

	target := input
	if !filepath.IsAbs(target) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		target = filepath.Join(cwd, target)
	}
	target = filepath.Clean(target)

	rel, err := filepath.Rel(rootDir, target)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", input, err)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return rel, nil
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path %q is outside repository", input)
	}
	return rel, nil
}

func graftIgnoreExplanation(checker *repo.IgnoreChecker, relPath string) repo.IgnoreExplanation {
	explanation := checker.Explain(relPath)
	if explanation.Ignored {
		return explanation
	}

	dir := filepath.ToSlash(filepath.Dir(relPath))
	for dir != "." && dir != "" {
		dirExplanation := checker.Explain(dir)
		if dirExplanation.Ignored {
			dirExplanation.Path = relPath
			dirExplanation.MatchedPath = dir
			return dirExplanation
		}
		next := filepath.ToSlash(filepath.Dir(dir))
		if next == dir {
			break
		}
		dir = next
	}

	return explanation
}

func gitIgnoreExplanation(rootDir, relPath string) (*repo.IgnoreExplanation, error) {
	explanation := &repo.IgnoreExplanation{Path: relPath}

	if err := runGitStreamingWithLabel(context.Background(), rootDir, io.Discard, io.Discard, "git-check-ignore:quiet", "check-ignore", "-q", "--", relPath); err != nil {
		var exitCoder interface{ ExitCode() int }
		if errors.As(err, &exitCoder) && exitCoder.ExitCode() == 1 {
			return explanation, nil
		}
		return nil, fmt.Errorf("git check-ignore %q: %w", relPath, err)
	}

	output, err := runGitCaptureWithLabel(context.Background(), rootDir, "git-check-ignore:verbose", "check-ignore", "-v", "--", relPath)
	if err != nil {
		return nil, fmt.Errorf("git check-ignore -v %q: %w", relPath, err)
	}
	match, err := parseGitCheckIgnoreVerbose(bytes.TrimSpace(output))
	if err != nil {
		return nil, err
	}

	explanation.Ignored = true
	explanation.Matches = []repo.IgnoreMatch{match}
	explanation.Final = &explanation.Matches[0]
	return explanation, nil
}

func parseGitCheckIgnoreVerbose(output []byte) (repo.IgnoreMatch, error) {
	line := string(output)
	tab := strings.IndexByte(line, '\t')
	if tab < 0 {
		return repo.IgnoreMatch{}, fmt.Errorf("parse git check-ignore output %q: missing tab separator", line)
	}

	left := line[:tab]
	parts := strings.SplitN(left, ":", 3)
	if len(parts) != 3 {
		return repo.IgnoreMatch{}, fmt.Errorf("parse git check-ignore output %q: expected source:line:pattern", line)
	}

	lineNo, err := strconv.Atoi(parts[1])
	if err != nil {
		return repo.IgnoreMatch{}, fmt.Errorf("parse git check-ignore line number %q: %w", parts[1], err)
	}

	return repo.IgnoreMatch{
		Pattern: parts[2],
		Source:  parts[0],
		Line:    lineNo,
	}, nil
}
