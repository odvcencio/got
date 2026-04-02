package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/graft/pkg/repo"
)

// mcpCodeintelToolDefs returns tool definitions for code intelligence.
func mcpCodeintelToolDefs() []mcpTool {
	return []mcpTool{
		{
			Name:        "graft_ci_entities",
			Description: "Extract structural entities (functions, types, classes, methods) from a source file using tree-sitter. Returns each entity's name, kind, signature, and line range.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"file": {Type: "string", Description: "file path relative to repo root (required)"},
				},
				Required: []string{"file"},
			}.toMap(),
		},
		{
			Name:        "graft_ci_symbols",
			Description: "Search for symbol definitions by name substring across the repository working tree. Returns matching declarations with file, line, kind, and signature.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"pattern": {Type: "string", Description: "name substring to search for, case-insensitive (required)"},
					"kind":    {Type: "string", Description: "filter by entity kind: function, type, method, var, const (optional)"},
					"limit":   {Type: "string", Description: "max results (default 50)"},
				},
				Required: []string{"pattern"},
			}.toMap(),
		},
		{
			Name:        "graft_ci_references",
			Description: "Find all call sites and references to a qualified symbol name. Returns file, line, and enclosing function for each reference. Builds xref index on first call.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"name": {Type: "string", Description: "qualified symbol name, e.g. 'module/pkg.Function' (required)"},
				},
				Required: []string{"name"},
			}.toMap(),
		},
		{
			Name:        "graft_ci_exports",
			Description: "List all exported (public) symbols from a Go package. Shows functions, types, vars, and consts with their signatures.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"package": {Type: "string", Description: "package directory relative to repo root, e.g. 'pkg/coord' (required)"},
				},
				Required: []string{"package"},
			}.toMap(),
		},
		{
			Name:        "graft_ci_callers",
			Description: "Find all functions/methods that call a given symbol. Groups references by calling entity to show the call graph.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"name": {Type: "string", Description: "qualified symbol name, e.g. 'module/pkg.Function' (required)"},
				},
				Required: []string{"name"},
			}.toMap(),
		},
	}
}

// mcpDispatchCodeintelTool routes a code intelligence tool call to its handler.
func mcpDispatchCodeintelTool(name string, args map[string]any) (any, error) {
	switch name {
	case "graft_ci_entities":
		return mcpToolCIEntities(args)
	case "graft_ci_symbols":
		return mcpToolCISymbols(args)
	case "graft_ci_references":
		return mcpToolCIReferences(args)
	case "graft_ci_exports":
		return mcpToolCIExports(args)
	case "graft_ci_callers":
		return mcpToolCICallers(args)
	default:
		return nil, fmt.Errorf("unknown codeintel tool %q", name)
	}
}

// --- Tool implementations ---

type ciEntity struct {
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind"`
	DeclKind  string `json:"decl_kind,omitempty"`
	Receiver  string `json:"receiver,omitempty"`
	Signature string `json:"signature,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Key       string `json:"key"`
}

func mcpToolCIEntities(args map[string]any) (any, error) {
	file := mcpArgString(args, "file")
	if file == "" {
		return nil, fmt.Errorf("file is required")
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

	var entities []ciEntity
	for _, e := range el.Entities {
		if e.Kind == entity.KindInterstitial {
			continue
		}
		entities = append(entities, ciEntity{
			Name:      e.Name,
			Kind:      e.Kind.String(),
			DeclKind:  e.DeclKind,
			Receiver:  e.Receiver,
			Signature: e.Signature,
			StartLine: e.StartLine,
			EndLine:   e.EndLine,
			Key:       e.IdentityKey(),
		})
	}

	return map[string]any{
		"file":     file,
		"language": el.Language,
		"count":    len(entities),
		"entities": entities,
	}, nil
}

type ciSymbolMatch struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	DeclKind  string `json:"decl_kind,omitempty"`
	Receiver  string `json:"receiver,omitempty"`
	Signature string `json:"signature,omitempty"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func mcpToolCISymbols(args map[string]any) (any, error) {
	pattern := mcpArgString(args, "pattern")
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	kindFilter := mcpArgString(args, "kind")
	limitStr := mcpArgString(args, "limit")
	limit := 50
	if limitStr != "" {
		if n, err := fmt.Sscanf(limitStr, "%d", &limit); n != 1 || err != nil {
			limit = 50
		}
	}

	r, err := repo.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	patternLower := strings.ToLower(pattern)
	var matches []ciSymbolMatch

	err = filepath.Walk(r.RootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		base := filepath.Base(path)
		if info.IsDir() {
			if strings.HasPrefix(base, ".") || base == "vendor" || base == "node_modules" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip files that tree-sitter can't parse.
		ext := filepath.Ext(path)
		switch ext {
		case ".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".rs", ".c", ".h", ".cpp", ".hpp", ".java", ".rb", ".cs":
			// supported
		default:
			return nil
		}
		if len(matches) >= limit {
			return filepath.SkipAll
		}

		relPath, err := filepath.Rel(r.RootDir, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		el, err := entity.Extract(relPath, source)
		if err != nil {
			return nil
		}

		for _, e := range el.Entities {
			if e.Kind != entity.KindDeclaration || e.Name == "" {
				continue
			}
			if !strings.Contains(strings.ToLower(e.Name), patternLower) {
				continue
			}
			if kindFilter != "" && !matchesKindFilter(e, kindFilter) {
				continue
			}
			matches = append(matches, ciSymbolMatch{
				Name:      e.Name,
				Kind:      e.Kind.String(),
				DeclKind:  e.DeclKind,
				Receiver:  e.Receiver,
				Signature: e.Signature,
				File:      relPath,
				StartLine: e.StartLine,
				EndLine:   e.EndLine,
			})
			if len(matches) >= limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return nil, fmt.Errorf("walk repo: %w", err)
	}

	return map[string]any{
		"pattern": pattern,
		"count":   len(matches),
		"matches": matches,
	}, nil
}

func matchesKindFilter(e entity.Entity, filter string) bool {
	switch strings.ToLower(filter) {
	case "function", "func":
		return e.DeclKind == "function_declaration" || e.DeclKind == "function_definition" || e.DeclKind == "function_item"
	case "type":
		return e.DeclKind == "type_declaration" || e.DeclKind == "type_spec"
	case "method":
		return e.DeclKind == "method_declaration" || e.DeclKind == "method_definition"
	case "var":
		return e.DeclKind == "var_declaration"
	case "const":
		return e.DeclKind == "const_declaration"
	case "class":
		return e.DeclKind == "class_declaration" || e.DeclKind == "class_definition"
	case "struct":
		return e.DeclKind == "struct_item" || e.DeclKind == "struct_declaration"
	case "interface":
		return e.DeclKind == "interface_declaration"
	}
	return strings.Contains(strings.ToLower(e.DeclKind), strings.ToLower(filter))
}

func loadOrBuildXrefIndex(c *coord.Coordinator) (*coord.XrefIndex, error) {
	idx, err := c.LoadXrefIndex()
	if err != nil {
		modulePath := ""
		gomodPath := filepath.Join(c.Repo.RootDir, "go.mod")
		if deps, parseErr := coord.ParseGoModDeps(gomodPath); parseErr == nil {
			modulePath = deps.Module
		}
		idx, err = coord.BuildXrefIndex(c.Repo.RootDir, modulePath)
		if err != nil {
			return nil, fmt.Errorf("build xref index: %w", err)
		}
		_ = c.SaveXrefIndex(idx)
	}
	return idx, nil
}

func mcpToolCIReferences(args map[string]any) (any, error) {
	name := mcpArgString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	idx, err := loadOrBuildXrefIndex(c)
	if err != nil {
		return nil, err
	}

	sites, ok := idx.Refs[name]
	if !ok {
		// Try partial match.
		var partialMatches []string
		nameLower := strings.ToLower(name)
		for key := range idx.Refs {
			if strings.Contains(strings.ToLower(key), nameLower) {
				partialMatches = append(partialMatches, key)
			}
		}
		if len(partialMatches) == 0 {
			return map[string]any{
				"name":       name,
				"count":      0,
				"references": []coord.XrefCallSite{},
			}, nil
		}
		sort.Strings(partialMatches)
		// Return partial matches as suggestions.
		return map[string]any{
			"name":        name,
			"count":       0,
			"references":  []coord.XrefCallSite{},
			"suggestions": partialMatches,
		}, nil
	}

	return map[string]any{
		"name":       name,
		"count":      len(sites),
		"references": sites,
	}, nil
}

func mcpToolCIExports(args map[string]any) (any, error) {
	pkg := mcpArgString(args, "package")
	if pkg == "" {
		return nil, fmt.Errorf("package is required")
	}
	// Normalize path separators.
	pkg = filepath.ToSlash(strings.TrimSuffix(pkg, "/"))

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	idx, err := c.LoadExportIndex()
	if err != nil {
		idx, err = coord.BuildExportIndex(c.Repo)
		if err != nil {
			return nil, fmt.Errorf("build export index: %w", err)
		}
		_ = c.SaveExportIndex(idx)
	}

	exports, ok := idx.Packages[pkg]
	if !ok {
		// Try to find a matching package.
		var candidates []string
		for p := range idx.Packages {
			if strings.HasSuffix(p, pkg) || strings.Contains(p, pkg) {
				candidates = append(candidates, p)
			}
		}
		if len(candidates) == 0 {
			return map[string]any{
				"package": pkg,
				"count":   0,
				"exports": map[string]coord.ExportedEntity{},
			}, nil
		}
		if len(candidates) == 1 {
			exports = idx.Packages[candidates[0]]
			pkg = candidates[0]
		} else {
			return map[string]any{
				"package":    pkg,
				"count":      0,
				"exports":    map[string]coord.ExportedEntity{},
				"candidates": candidates,
			}, nil
		}
	}

	return map[string]any{
		"package": pkg,
		"count":   len(exports),
		"exports": exports,
	}, nil
}

type ciCaller struct {
	Entity string `json:"entity"`
	File   string `json:"file"`
	Lines  []int  `json:"lines"`
}

func mcpToolCICallers(args map[string]any) (any, error) {
	name := mcpArgString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	idx, err := loadOrBuildXrefIndex(c)
	if err != nil {
		return nil, err
	}

	sites, ok := idx.Refs[name]
	if !ok {
		return map[string]any{
			"name":    name,
			"count":   0,
			"callers": []ciCaller{},
		}, nil
	}

	// Group by calling entity.
	grouped := make(map[string]*ciCaller)
	for _, site := range sites {
		key := site.File + ":" + site.Entity
		if c, ok := grouped[key]; ok {
			c.Lines = append(c.Lines, site.Line)
		} else {
			grouped[key] = &ciCaller{
				Entity: site.Entity,
				File:   site.File,
				Lines:  []int{site.Line},
			}
		}
	}

	var callers []ciCaller
	for _, c := range grouped {
		callers = append(callers, *c)
	}

	return map[string]any{
		"name":    name,
		"count":   len(callers),
		"callers": callers,
	}, nil
}
