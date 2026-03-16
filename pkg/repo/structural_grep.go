package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/gotreesitter/grammars"
	tsgrep "github.com/odvcencio/gotreesitter/grep"
)

// StructuralGrepOptions configures a structural (AST-aware) code search.
type StructuralGrepOptions struct {
	Pattern     string // code pattern with metavariables
	PathPattern string // glob filter on file path
}

// StructuralGrepResult represents a single structural match with entity context.
type StructuralGrepResult struct {
	Path        string            // file path relative to repo root
	StartLine   int               // 1-based
	EndLine     int
	StartByte   uint32
	EndByte     uint32
	Captures    map[string]string // capture name -> matched text
	EntityName  string            // enclosing entity name
	EntityKind  string            // enclosing entity kind
	EntityKey   string            // enclosing entity identity key
	MatchedText string            // full matched source text
}

// skipDirs are directory names that StructuralGrep skips during tree walking.
var skipDirs = map[string]bool{
	".graft":       true,
	".git":         true,
	"vendor":       true,
	"node_modules": true,
}

// StructuralGrep searches working tree files for structural pattern matches
// using tree-sitter AST-aware matching. For each match it resolves the
// enclosing entity to provide declaration context. Results are sorted by
// path then start line.
func (r *Repo) StructuralGrep(opts StructuralGrepOptions) ([]StructuralGrepResult, error) {
	if opts.Pattern == "" {
		return nil, fmt.Errorf("structural grep: pattern must not be empty")
	}

	var results []StructuralGrepResult

	err := filepath.WalkDir(r.RootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded directories.
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// Compute relative path.
		relPath, err := filepath.Rel(r.RootDir, path)
		if err != nil {
			return nil
		}
		// Normalize to forward slashes for consistency.
		relPath = filepath.ToSlash(relPath)

		// Apply path filter if specified.
		if opts.PathPattern != "" {
			matched, err := filepath.Match(opts.PathPattern, relPath)
			if err != nil {
				return fmt.Errorf("structural grep: invalid path pattern %q: %w", opts.PathPattern, err)
			}
			if !matched {
				// Also try matching against the base name.
				matched, _ = filepath.Match(opts.PathPattern, filepath.Base(relPath))
			}
			if !matched {
				return nil
			}
		}

		// Detect language from filename.
		entry := grammars.DetectLanguage(d.Name())
		if entry == nil {
			return nil
		}

		// Read source.
		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if len(source) == 0 {
			return nil
		}

		// Run structural match.
		lang := entry.Language()
		matches, err := tsgrep.Match(lang, opts.Pattern, source)
		if err != nil {
			// Pattern may not apply to this language; skip silently.
			return nil
		}
		if len(matches) == 0 {
			return nil
		}

		// Extract entities for context (best-effort).
		var entities []entity.Entity
		el, extractErr := entity.Extract(relPath, source)
		if extractErr == nil && el != nil {
			entities = el.Entities
		}

		for _, m := range matches {
			res := StructuralGrepResult{
				Path:      relPath,
				StartByte: m.StartByte,
				EndByte:   m.EndByte,
				StartLine: lineNumberAt(source, m.StartByte),
				EndLine:   lineNumberAt(source, m.EndByte),
				Captures:  make(map[string]string, len(m.Captures)),
			}

			// Extract matched text.
			if int(m.EndByte) <= len(source) {
				res.MatchedText = string(source[m.StartByte:m.EndByte])
			}

			// Convert captures.
			for name, cap := range m.Captures {
				res.Captures[name] = string(cap.Text)
			}

			// Find enclosing entity.
			if len(entities) > 0 {
				if ent := findEnclosingEntity(entities, m.StartByte, m.EndByte); ent != nil {
					res.EntityName = ent.Name
					res.EntityKind = ent.Kind.String()
					res.EntityKey = ent.IdentityKey()
				}
			}

			results = append(results, res)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("structural grep: %w", err)
	}

	// Sort by path, then by start line.
	sort.Slice(results, func(i, j int) bool {
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		return results[i].StartLine < results[j].StartLine
	})

	return results, nil
}

// lineNumberAt returns the 1-based line number for the given byte offset in source.
func lineNumberAt(source []byte, bytePos uint32) int {
	if bytePos == 0 {
		return 1
	}
	if int(bytePos) > len(source) {
		bytePos = uint32(len(source))
	}
	line := 1
	for i := uint32(0); i < bytePos; i++ {
		if source[i] == '\n' {
			line++
		}
	}
	return line
}

// findEnclosingEntity finds the smallest entity that fully contains the match range.
func findEnclosingEntity(entities []entity.Entity, startByte, endByte uint32) *entity.Entity {
	var best *entity.Entity
	bestSize := uint32(0)

	for i := range entities {
		e := &entities[i]
		if e.StartByte <= startByte && e.EndByte >= endByte {
			size := e.EndByte - e.StartByte
			if best == nil || size < bestSize {
				best = e
				bestSize = size
			}
		}
	}
	return best
}

