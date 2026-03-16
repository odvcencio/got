package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/gotreesitter/grammars"
	tsgrep "github.com/odvcencio/gotreesitter/grep"
)

// StructuralGrepOptions configures a structural (AST-aware) code search.
type StructuralGrepOptions struct {
	Pattern     string // code pattern with metavariables
	PathPattern string // glob filter on file path
	SExp        bool   // treat pattern as raw S-expression
	Rewrite     string // replacement template (empty = search only)
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
		var matches []tsgrep.Result
		if opts.SExp {
			matches, err = tsgrep.MatchSexp(lang, opts.Pattern, source)
		} else {
			matches, err = tsgrep.Match(lang, opts.Pattern, source)
		}
		if err != nil {
			// Pattern may not apply to this language; skip silently.
			return nil
		}
		if len(matches) == 0 {
			return nil
		}

		// If rewrite mode, apply replacements to this file and continue
		// (we still collect match results for reporting).
		if opts.Rewrite != "" {
			rr, replaceErr := tsgrep.Replace(lang, opts.Pattern, opts.Rewrite, source)
			if replaceErr == nil && len(rr.Edits) > 0 {
				newSource := tsgrep.ApplyEdits(source, rr.Edits)
				_ = os.WriteFile(path, newSource, 0644)
			}
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

// HistoryGrepOptions configures a structural search across commit history.
type HistoryGrepOptions struct {
	Pattern     string // code pattern with metavariables
	PathPattern string // glob filter on file path
	SExp        bool   // treat pattern as raw S-expression
	Since       string // ref to start from (oldest boundary)
	Until       string // ref to stop at (newest, default HEAD)
	MaxCommits  int    // limit number of commits to search
}

// HistoryGrepResult represents a single structural match found in a historical commit.
type HistoryGrepResult struct {
	CommitHash  string            // commit where the match was found
	CommitMsg   string            // first line of commit message
	Path        string            // file path relative to repo root
	StartLine   int               // 1-based
	EndLine     int
	MatchedText string            // full matched source text
	Captures    map[string]string // capture name -> matched text
	EntityName  string            // enclosing entity name
	EntityKind  string            // enclosing entity kind
	EntityKey   string            // enclosing entity identity key
}

// HistoryGrep searches structural patterns across commit history. It walks
// first-parent history from Until (default HEAD) backward, stopping at Since
// or after MaxCommits. For each commit it flattens the tree, reads blob
// content, and runs tree-sitter structural matching.
func (r *Repo) HistoryGrep(opts HistoryGrepOptions) ([]HistoryGrepResult, error) {
	if opts.Pattern == "" {
		return nil, fmt.Errorf("history grep: pattern must not be empty")
	}
	if opts.MaxCommits <= 0 {
		opts.MaxCommits = 1000
	}

	// Resolve Until ref.
	untilRef := opts.Until
	if untilRef == "" {
		untilRef = "HEAD"
	}
	untilHash, err := r.ResolveRef(untilRef)
	if err != nil {
		return nil, fmt.Errorf("history grep: resolve %q: %w", untilRef, err)
	}

	// Resolve Since ref if provided.
	var sinceHash object.Hash
	if opts.Since != "" {
		sinceHash, err = r.ResolveRef(opts.Since)
		if err != nil {
			return nil, fmt.Errorf("history grep: resolve --since %q: %w", opts.Since, err)
		}
	}

	shallow, _ := r.ShallowState()

	var results []HistoryGrepResult
	current := untilHash
	walked := 0

	for current != "" && walked < opts.MaxCommits {
		// If we've reached the Since boundary, stop.
		if sinceHash != "" && current == sinceHash {
			break
		}

		commit, err := r.Store.ReadCommit(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			return nil, fmt.Errorf("history grep: read commit %s: %w", current, err)
		}
		walked++

		title := commitTitle(commit.Message)

		// Flatten the commit tree to get all file entries.
		entries, err := r.FlattenTree(commit.TreeHash)
		if err != nil {
			return nil, fmt.Errorf("history grep: flatten tree for %s: %w", current, err)
		}

		for _, fe := range entries {
			relPath := fe.Path

			// Apply path filter if specified.
			if opts.PathPattern != "" {
				matched, matchErr := filepath.Match(opts.PathPattern, relPath)
				if matchErr != nil {
					return nil, fmt.Errorf("history grep: invalid path pattern %q: %w", opts.PathPattern, matchErr)
				}
				if !matched {
					matched, _ = filepath.Match(opts.PathPattern, filepath.Base(relPath))
				}
				if !matched {
					continue
				}
			}

			// Detect language from filename.
			langEntry := grammars.DetectLanguage(filepath.Base(relPath))
			if langEntry == nil {
				continue
			}

			// Read blob content from the object store.
			blob, err := r.Store.ReadBlob(fe.BlobHash)
			if err != nil {
				// Blob may be missing in shallow repos; skip.
				continue
			}
			source := blob.Data
			if len(source) == 0 {
				continue
			}

			// Run structural match.
			lang := langEntry.Language()
			var matches []tsgrep.Result
			if opts.SExp {
				matches, err = tsgrep.MatchSexp(lang, opts.Pattern, source)
			} else {
				matches, err = tsgrep.Match(lang, opts.Pattern, source)
			}
			if err != nil {
				// Pattern may not apply to this language; skip.
				continue
			}
			if len(matches) == 0 {
				continue
			}

			// Extract entities for context (best-effort).
			var entities []entity.Entity
			el, extractErr := entity.Extract(relPath, source)
			if extractErr == nil && el != nil {
				entities = el.Entities
			}

			for _, m := range matches {
				res := HistoryGrepResult{
					CommitHash: string(current),
					CommitMsg:  title,
					Path:       relPath,
					StartLine:  lineNumberAt(source, m.StartByte),
					EndLine:    lineNumberAt(source, m.EndByte),
					Captures:   make(map[string]string, len(m.Captures)),
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
		}

		// Follow first parent.
		if len(commit.Parents) == 0 {
			break
		}
		next := commit.Parents[0]
		if shallow != nil && shallow.IsShallow(next) {
			break
		}
		current = next
	}

	return results, nil
}

