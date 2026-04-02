package repo

import (
	"context"
	"fmt"
	"strings"
)

// parseGrepPattern splits a hook grep pattern into a language filter and
// the structural pattern. Patterns use the form "lang::pattern" where lang
// is a language name (e.g. "go", "rust", "javascript"). If no language
// prefix is present the pattern is used as-is with language auto-detection.
func parseGrepPattern(raw string) (langFilter string, pattern string) {
	// Look for "lang::" prefix. The language name must be a simple
	// identifier (no spaces, no colons) followed by "::".
	if idx := strings.Index(raw, "::"); idx > 0 {
		candidate := raw[:idx]
		// Validate that the candidate looks like a language name
		// (no spaces, non-empty, no path separators).
		if !strings.ContainsAny(candidate, " \t/\\") {
			return strings.ToLower(candidate), raw[idx+2:]
		}
	}
	return "", raw
}

// langToGlob maps a language filter name to a file glob pattern.
var langToGlob = map[string]string{
	"go":         "*.go",
	"rust":       "*.rs",
	"javascript": "*.js",
	"typescript": "*.ts",
	"python":     "*.py",
	"java":       "*.java",
	"c":          "*.c",
	"cpp":        "*.cpp",
	"ruby":       "*.rb",
	"swift":      "*.swift",
	"kotlin":     "*.kt",
	"css":        "*.css",
	"html":       "*.html",
}

// runGrepHook executes a structural grep hook against the working tree.
// If matches are found, behaviour depends on entry.Action:
//   - "block" (default): return an error to abort the hook chain
//   - "warn": print matches to stdout and return nil
func runGrepHook(_ context.Context, r *Repo, entry HookEntry) error {
	langFilter, pattern := parseGrepPattern(entry.Grep)
	if pattern == "" {
		return fmt.Errorf("hook %s.%s: empty grep pattern", entry.Point, entry.Name)
	}

	opts := StructuralGrepOptions{
		Pattern: pattern,
	}
	if langFilter != "" {
		if glob, ok := langToGlob[langFilter]; ok {
			opts.PathPattern = glob
		}
		// If language is not in the map we still run; the tree-sitter
		// language detection will decide which files apply.
	}

	results, err := r.StructuralGrep(opts)
	if err != nil {
		// Pattern compilation failures are reported but non-fatal
		// to avoid blocking on misconfigured patterns.
		return fmt.Errorf("hook %s.%s: grep error: %w", entry.Point, entry.Name, err)
	}

	if len(results) == 0 {
		// No matches — hook passes.
		return nil
	}

	// Format match details.
	msg := entry.Message
	if msg == "" {
		msg = fmt.Sprintf("structural grep matched %q", entry.Grep)
	}

	var detail strings.Builder
	detail.WriteString(fmt.Sprintf("hook %s.%s: %s\n", entry.Point, entry.Name, msg))
	for _, m := range results {
		entityCtx := ""
		if m.EntityName != "" {
			entityCtx = fmt.Sprintf(" in %s %s", m.EntityKind, m.EntityName)
		}
		matchPreview := m.MatchedText
		if len(matchPreview) > 120 {
			matchPreview = matchPreview[:117] + "..."
		}
		// Replace newlines in preview for single-line display.
		matchPreview = strings.ReplaceAll(matchPreview, "\n", "\\n")
		detail.WriteString(fmt.Sprintf("  %s:%d%s: %s\n", m.Path, m.StartLine, entityCtx, matchPreview))
	}
	detail.WriteString(fmt.Sprintf("  %d match(es) found\n", len(results)))

	action := strings.ToLower(strings.TrimSpace(entry.Action))
	if action == "" {
		action = "block"
	}

	switch action {
	case "block":
		return fmt.Errorf("%s", detail.String())
	case "warn":
		// Print warning but allow the operation to proceed.
		fmt.Print(detail.String())
		return nil
	default:
		return fmt.Errorf("hook %s.%s: unknown action %q (expected \"block\" or \"warn\")", entry.Point, entry.Name, entry.Action)
	}
}
