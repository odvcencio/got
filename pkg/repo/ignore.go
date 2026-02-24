package repo

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// IgnoreChecker determines if a path should be ignored.
type IgnoreChecker struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	pattern  string
	negated  bool
	dirOnly  bool
	hasSlash bool // pattern contains a slash, so match against full path
	regex    *regexp.Regexp
}

// NewIgnoreChecker creates an IgnoreChecker for the given repository root.
// It always ignores .got/ and .git/. If a .gotignore file exists in repoRoot,
// its patterns are parsed and applied.
func NewIgnoreChecker(repoRoot string) *IgnoreChecker {
	ic := &IgnoreChecker{}

	// Hardcoded patterns: always ignore .got/ and .git/.
	ic.patterns = append(ic.patterns,
		ignorePattern{pattern: ".got", dirOnly: false, hasSlash: false},
		ignorePattern{pattern: ".git", dirOnly: false, hasSlash: false},
	)

	// Read .gotignore if it exists.
	ignorePath := filepath.Join(repoRoot, ".gotignore")
	f, err := os.Open(ignorePath)
	if err != nil {
		// No .gotignore — only hardcoded patterns apply.
		return ic
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		p := parseLine(line)
		if p != nil {
			ic.patterns = append(ic.patterns, *p)
		}
	}

	return ic
}

// parseLine parses a single line from a .gotignore file. Returns nil if the
// line is empty or a comment.
func parseLine(line string) *ignorePattern {
	// Trim trailing whitespace.
	line = strings.TrimRight(line, " \t")

	// Empty lines are skipped.
	if line == "" {
		return nil
	}

	// Comment lines are skipped.
	if strings.HasPrefix(line, "#") {
		return nil
	}

	p := &ignorePattern{}

	// Negation: lines starting with ! un-ignore a pattern.
	if strings.HasPrefix(line, "!") {
		p.negated = true
		line = line[1:]
	}

	// Directory-only: lines ending with / match directories only.
	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimRight(line, "/")
	}

	// If the pattern contains a slash, match against the full relative path.
	p.hasSlash = strings.Contains(line, "/")

	p.pattern = line
	if strings.Contains(line, "**") {
		if re, err := regexp.Compile(globToRegex(line)); err == nil {
			p.regex = re
		}
	}
	return p
}

// IsIgnored checks whether a relative path should be ignored. The path should
// use forward slashes and be relative to the repository root.
//
// Last matching pattern wins (to support negation).
func (ic *IgnoreChecker) IsIgnored(path string) bool {
	// Normalise to forward slashes.
	path = filepath.ToSlash(path)

	ignored := false
	for _, p := range ic.patterns {
		if p.matches(path) {
			ignored = !p.negated
		}
	}
	return ignored
}

// matches checks if the given relative path matches this ignore pattern.
func (p *ignorePattern) matches(path string) bool {
	// For hardcoded patterns (.got, .git) and dir-only patterns,
	// check if the path is the directory itself or starts with it.
	if p.dirOnly || p.pattern == ".got" || p.pattern == ".git" {
		// Check exact match or prefix match (path is under this directory).
		if path == p.pattern || strings.HasPrefix(path, p.pattern+"/") {
			return true
		}
	}

	// If dirOnly and the above didn't match, nothing else to check.
	if p.dirOnly {
		return false
	}

	if p.hasSlash {
		// Pattern contains a slash: match against the full relative path.
		return p.match(path)
	}

	// Pattern without a slash: match against the filename component only.
	filename := filepath.Base(path)
	return p.match(filename)
}

func (p *ignorePattern) match(target string) bool {
	if p.regex != nil {
		return p.regex.MatchString(target)
	}
	matched, _ := filepath.Match(p.pattern, target)
	return matched
}

func globToRegex(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		if ch == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					// Globstar directory segment: match zero or more path segments.
					b.WriteString("(?:.*/)?")
					i += 2
				} else {
					b.WriteString(".*")
					i++
				}
				continue
			}
			b.WriteString("[^/]*")
			continue
		}
		if ch == '?' {
			b.WriteString("[^/]")
			continue
		}
		if strings.ContainsRune(`.+()|[]{}^$\\`, rune(ch)) {
			b.WriteByte('\\')
		}
		b.WriteByte(ch)
	}
	b.WriteString("$")
	return b.String()
}
