package repo

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// GrepResult represents a single grep match in a tracked file.
type GrepResult struct {
	Path    string
	Line    int
	Content string
}

// GrepOptions configures the grep search.
type GrepOptions struct {
	Pattern         string
	CaseInsensitive bool
	FixedString     bool   // literal string match, not regex
	PathPattern     string // filter to files matching this glob
}

// Grep searches tracked files for pattern matches. It reads the staging index,
// then for each tracked file reads content from disk and searches line by line.
// Results are sorted by path then line number.
func (r *Repo) Grep(opts GrepOptions) ([]GrepResult, error) {
	if opts.Pattern == "" {
		return nil, fmt.Errorf("grep: pattern must not be empty")
	}

	stg, err := r.ReadStaging()
	if err != nil {
		return nil, fmt.Errorf("grep: %w", err)
	}

	// Collect tracked paths sorted.
	paths := make([]string, 0, len(stg.Entries))
	for p := range stg.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Build matcher.
	matcher, err := buildGrepMatcher(opts)
	if err != nil {
		return nil, fmt.Errorf("grep: %w", err)
	}

	var results []GrepResult

	for _, p := range paths {
		// Apply path filter if specified.
		if opts.PathPattern != "" {
			matched, err := filepath.Match(opts.PathPattern, p)
			if err != nil {
				return nil, fmt.Errorf("grep: invalid path pattern %q: %w", opts.PathPattern, err)
			}
			if !matched {
				// Also try matching against the base name.
				matched, _ = filepath.Match(opts.PathPattern, filepath.Base(p))
			}
			if !matched {
				continue
			}
		}

		absPath := filepath.Join(r.RootDir, filepath.FromSlash(p))
		data, err := os.ReadFile(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // file deleted from disk but still tracked
			}
			return nil, fmt.Errorf("grep: read %s: %w", p, err)
		}

		scanner := bufio.NewScanner(bytes.NewReader(data))
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if matcher(line) {
				results = append(results, GrepResult{
					Path:    p,
					Line:    lineNum,
					Content: line,
				})
			}
		}
	}

	return results, nil
}

// buildGrepMatcher returns a function that tests whether a line matches the
// grep pattern according to the given options.
func buildGrepMatcher(opts GrepOptions) (func(string) bool, error) {
	if opts.FixedString {
		pattern := opts.Pattern
		if opts.CaseInsensitive {
			pattern = strings.ToLower(pattern)
			return func(line string) bool {
				return strings.Contains(strings.ToLower(line), pattern)
			}, nil
		}
		return func(line string) bool {
			return strings.Contains(line, pattern)
		}, nil
	}

	// Regex mode.
	pattern := opts.Pattern
	if opts.CaseInsensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", opts.Pattern, err)
	}
	return func(line string) bool {
		return re.MatchString(line)
	}, nil
}
