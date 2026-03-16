package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/gotreesitter/grammars"
	tsgrep "github.com/odvcencio/gotreesitter/grep"
)

// mcpGrepToolDefs returns tool definitions for structural grep operations.
func mcpGrepToolDefs() []mcpTool {
	return []mcpTool{
		{
			Name:        "graft_grep",
			Description: "Structural pattern search using tree-sitter AST-aware matching. Finds code patterns with metavariable captures and resolves enclosing entity context for each match.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"pattern":      {Type: "string", Description: "code pattern with metavariables, e.g. 'fmt.Errorf($$$ARGS)' (required)"},
					"path_pattern": {Type: "string", Description: "glob filter on file path, e.g. '*.go' (optional)"},
				},
				Required: []string{"pattern"},
			}.toMap(),
		},
		{
			Name:        "graft_grep_replace",
			Description: "Structural search and replace with preview. Finds pattern matches and computes replacement edits using a template with capture references ($NAME). Shows before/after for each file. Set apply=true to write changes.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"pattern":      {Type: "string", Description: "code pattern with metavariables (required)"},
					"replacement":  {Type: "string", Description: "replacement template with capture references, e.g. 'errors.Wrap($$$ARGS)' (required)"},
					"path_pattern": {Type: "string", Description: "glob filter on file path (optional)"},
					"apply":        {Type: "boolean", Description: "if true, write changes to disk; otherwise preview only (default false)"},
				},
				Required: []string{"pattern", "replacement"},
			}.toMap(),
		},
		{
			Name:        "graft_entity_edit",
			Description: "Symbol-level edit operations on source entities. Operates on entities by identity key (from graft_ci_entities). Supports replacing an entity body, inserting content before/after an entity, or deleting an entity.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"file":       {Type: "string", Description: "file path relative to repo root (required)"},
					"entity_key": {Type: "string", Description: "entity identity key, e.g. 'decl:function_declaration::MyFunc:...' (required)"},
					"operation":  {Type: "string", Description: "one of: replace_body, insert_after, insert_before, delete (required)"},
					"content":    {Type: "string", Description: "new content for replace_body, insert_after, or insert_before (required except for delete)"},
				},
				Required: []string{"file", "entity_key", "operation"},
			}.toMap(),
		},
	}
}

// mcpDispatchGrepTool routes a structural grep tool call to its handler.
func mcpDispatchGrepTool(name string, args map[string]any) (any, error) {
	switch name {
	case "graft_grep":
		return mcpToolGrep(args)
	case "graft_grep_replace":
		return mcpToolGrepReplace(args)
	case "graft_entity_edit":
		return mcpToolEntityEdit(args)
	default:
		return nil, fmt.Errorf("unknown grep tool %q", name)
	}
}

// --- Tool implementations ---

type grepMatch struct {
	Path        string            `json:"path"`
	StartLine   int               `json:"start_line"`
	EndLine     int               `json:"end_line"`
	MatchedText string            `json:"matched_text"`
	Captures    map[string]string `json:"captures,omitempty"`
	EntityName  string            `json:"entity_name,omitempty"`
	EntityKind  string            `json:"entity_kind,omitempty"`
	EntityKey   string            `json:"entity_key,omitempty"`
}

func mcpToolGrep(args map[string]any) (any, error) {
	pattern := mcpArgString(args, "pattern")
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	pathPattern := mcpArgString(args, "path_pattern")

	r, err := repo.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	results, err := r.StructuralGrep(repo.StructuralGrepOptions{
		Pattern:     pattern,
		PathPattern: pathPattern,
	})
	if err != nil {
		return nil, fmt.Errorf("structural grep: %w", err)
	}

	matches := make([]grepMatch, 0, len(results))
	for _, res := range results {
		m := grepMatch{
			Path:        res.Path,
			StartLine:   res.StartLine,
			EndLine:     res.EndLine,
			MatchedText: res.MatchedText,
			EntityName:  res.EntityName,
			EntityKind:  res.EntityKind,
			EntityKey:   res.EntityKey,
		}
		if len(res.Captures) > 0 {
			m.Captures = res.Captures
		}
		matches = append(matches, m)
	}

	return map[string]any{
		"pattern": pattern,
		"count":   len(matches),
		"matches": matches,
	}, nil
}

type grepReplaceEdit struct {
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Before    string `json:"before"`
	After     string `json:"after"`
}

type grepReplaceFile struct {
	Path        string            `json:"path"`
	Edits       []grepReplaceEdit `json:"edits"`
	Diagnostics []string          `json:"diagnostics,omitempty"`
}

func mcpToolGrepReplace(args map[string]any) (any, error) {
	pattern := mcpArgString(args, "pattern")
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	replacement := mcpArgString(args, "replacement")
	if replacement == "" {
		return nil, fmt.Errorf("replacement is required")
	}
	pathPattern := mcpArgString(args, "path_pattern")
	apply := mcpArgBool(args, "apply")

	r, err := repo.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	// Walk files the same way StructuralGrep does.
	var files []grepReplaceFile
	totalEdits := 0

	err = filepath.WalkDir(r.RootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".graft" || name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(r.RootDir, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		// Apply path filter.
		if pathPattern != "" {
			matched, err := filepath.Match(pathPattern, relPath)
			if err != nil {
				return fmt.Errorf("invalid path pattern %q: %w", pathPattern, err)
			}
			if !matched {
				matched, _ = filepath.Match(pathPattern, filepath.Base(relPath))
			}
			if !matched {
				return nil
			}
		}

		// Detect language.
		entry := grammars.DetectLanguage(d.Name())
		if entry == nil {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if len(source) == 0 {
			return nil
		}

		lang := entry.Language()
		result, err := tsgrep.Replace(lang, pattern, replacement, source)
		if err != nil {
			// Pattern may not apply to this language; skip silently.
			return nil
		}
		if len(result.Edits) == 0 {
			return nil
		}

		var edits []grepReplaceEdit
		for _, edit := range result.Edits {
			startLine := lineNumberAt(source, edit.StartByte)
			endLine := lineNumberAt(source, edit.EndByte)
			before := ""
			if int(edit.EndByte) <= len(source) {
				before = string(source[edit.StartByte:edit.EndByte])
			}
			edits = append(edits, grepReplaceEdit{
				StartLine: startLine,
				EndLine:   endLine,
				Before:    before,
				After:     string(edit.Replacement),
			})
		}

		var diags []string
		for _, d := range result.Diagnostics {
			diags = append(diags, d.Message)
		}

		files = append(files, grepReplaceFile{
			Path:        relPath,
			Edits:       edits,
			Diagnostics: diags,
		})
		totalEdits += len(edits)

		// If applying, compute the new source and write it.
		if apply {
			newSource := applyEdits(source, result.Edits)
			if writeErr := os.WriteFile(path, newSource, 0o644); writeErr != nil {
				return fmt.Errorf("write %s: %w", relPath, writeErr)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("grep replace: %w", err)
	}

	return map[string]any{
		"pattern":     pattern,
		"replacement": replacement,
		"applied":     apply,
		"files":       len(files),
		"total_edits": totalEdits,
		"results":     files,
	}, nil
}

// applyEdits applies byte-range edits to source in reverse order to preserve offsets.
func applyEdits(source []byte, edits []tsgrep.Edit) []byte {
	// Apply in reverse byte order so earlier offsets remain valid.
	sorted := make([]tsgrep.Edit, len(edits))
	copy(sorted, edits)
	for i := len(sorted)/2 - 1; i >= 0; i-- {
		j := len(sorted) - 1 - i
		sorted[i], sorted[j] = sorted[j], sorted[i]
	}

	result := make([]byte, len(source))
	copy(result, source)

	for _, edit := range sorted {
		before := result[:edit.StartByte]
		after := result[edit.EndByte:]
		result = make([]byte, 0, len(before)+len(edit.Replacement)+len(after))
		result = append(result, before...)
		result = append(result, edit.Replacement...)
		result = append(result, after...)
	}
	return result
}

// lineNumberAt returns the 1-based line number for a byte offset.
// Duplicated from structural_grep.go since that lives in pkg/repo.
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

func mcpToolEntityEdit(args map[string]any) (any, error) {
	file := mcpArgString(args, "file")
	if file == "" {
		return nil, fmt.Errorf("file is required")
	}
	entityKey := mcpArgString(args, "entity_key")
	if entityKey == "" {
		return nil, fmt.Errorf("entity_key is required")
	}
	operation := mcpArgString(args, "operation")
	if operation == "" {
		return nil, fmt.Errorf("operation is required")
	}
	content := mcpArgString(args, "content")

	// Validate operation.
	switch operation {
	case "replace_body", "insert_after", "insert_before":
		if content == "" {
			return nil, fmt.Errorf("content is required for operation %q", operation)
		}
	case "delete":
		// content not needed
	default:
		return nil, fmt.Errorf("unknown operation %q; must be one of: replace_body, insert_after, insert_before, delete", operation)
	}

	r, err := repo.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	absPath := filepath.Join(r.RootDir, file)
	// Prevent path traversal.
	if !strings.HasPrefix(absPath, r.RootDir+string(filepath.Separator)) && absPath != r.RootDir {
		return nil, fmt.Errorf("file path escapes repository root")
	}

	source, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	el, err := entity.Extract(file, source)
	if err != nil {
		return nil, fmt.Errorf("extract entities: %w", err)
	}

	// Find the target entity by key.
	var target *entity.Entity
	var targetIdx int
	for i := range el.Entities {
		if el.Entities[i].IdentityKey() == entityKey {
			target = &el.Entities[i]
			targetIdx = i
			break
		}
	}
	if target == nil {
		// Collect available keys for the error message.
		var available []string
		for _, e := range el.Entities {
			if e.Kind == entity.KindDeclaration {
				available = append(available, e.IdentityKey())
			}
		}
		return nil, fmt.Errorf("entity key %q not found; available keys: %v", entityKey, available)
	}

	// Build the new source by applying the operation.
	var newSource []byte
	switch operation {
	case "replace_body":
		newSource = make([]byte, 0, len(source)-len(target.Body)+len(content))
		newSource = append(newSource, source[:target.StartByte]...)
		newSource = append(newSource, []byte(content)...)
		newSource = append(newSource, source[target.EndByte:]...)

	case "insert_before":
		newSource = make([]byte, 0, len(source)+len(content)+1)
		newSource = append(newSource, source[:target.StartByte]...)
		newSource = append(newSource, []byte(content)...)
		// Ensure a newline separates the inserted content from the entity.
		if len(content) > 0 && content[len(content)-1] != '\n' {
			newSource = append(newSource, '\n')
		}
		newSource = append(newSource, source[target.StartByte:]...)

	case "insert_after":
		newSource = make([]byte, 0, len(source)+len(content)+1)
		newSource = append(newSource, source[:target.EndByte]...)
		// Ensure a newline separates the entity from the inserted content.
		if target.EndByte > 0 && source[target.EndByte-1] != '\n' {
			newSource = append(newSource, '\n')
		}
		newSource = append(newSource, []byte(content)...)
		newSource = append(newSource, source[target.EndByte:]...)

	case "delete":
		newSource = make([]byte, 0, len(source)-len(target.Body))
		newSource = append(newSource, source[:target.StartByte]...)
		newSource = append(newSource, source[target.EndByte:]...)
	}

	// Write the modified file.
	if err := os.WriteFile(absPath, newSource, 0o644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return map[string]any{
		"status":     "ok",
		"file":       file,
		"operation":  operation,
		"entity_key": entityKey,
		"entity": map[string]any{
			"name":       target.Name,
			"kind":       target.Kind.String(),
			"decl_kind":  target.DeclKind,
			"start_line": target.StartLine,
			"end_line":   target.EndLine,
			"index":      targetIdx,
		},
	}, nil
}
