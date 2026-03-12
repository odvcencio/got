package coord

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// XrefIndex maps qualified symbol names to call sites within a repository.
// The key format is "module/pkg.Function" (fully qualified).
type XrefIndex struct {
	Refs map[string][]XrefCallSite `json:"refs"`
}

// XrefCallSite records where a symbol is referenced.
type XrefCallSite struct {
	File   string `json:"file"`
	Entity string `json:"entity"`
	Line   int    `json:"line"`
}

// BuildXrefIndex scans Go source files in repoDir for import declarations
// and function call expressions. It builds a reverse mapping from qualified
// symbol names to call sites. modulePath is the module path of the repo
// being scanned (used to exclude self-references if desired).
func BuildXrefIndex(repoDir string, modulePath string) (*XrefIndex, error) {
	idx := &XrefIndex{
		Refs: make(map[string][]XrefCallSite),
	}

	absDir, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}

	err = filepath.Walk(absDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // skip errors
		}

		// Skip hidden directories and vendor.
		base := filepath.Base(path)
		if info.IsDir() {
			if strings.HasPrefix(base, ".") || base == "vendor" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(absDir, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		refs, err := extractXrefs(relPath, source)
		if err != nil {
			return nil // skip unparseable files
		}

		for qualName, sites := range refs {
			idx.Refs[qualName] = append(idx.Refs[qualName], sites...)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk repo: %w", err)
	}

	return idx, nil
}

// extractXrefs parses a Go file and extracts cross-references: for each
// imported package, it finds function calls using that package's alias and
// builds qualified references.
func extractXrefs(filename string, source []byte) (map[string][]XrefCallSite, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, source, 0)
	if err != nil {
		return nil, err
	}

	// Build import alias -> import path mapping.
	imports := make(map[string]string) // alias -> import path
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)

		var alias string
		if imp.Name != nil {
			alias = imp.Name.Name
			if alias == "_" || alias == "." {
				continue // skip blank and dot imports
			}
		} else {
			// Default alias is the last path component.
			parts := strings.Split(importPath, "/")
			alias = parts[len(parts)-1]
		}
		imports[alias] = importPath
	}

	if len(imports) == 0 {
		return nil, nil
	}

	// Find the enclosing function for a position.
	funcName := buildFuncPositionMap(f, fset)

	refs := make(map[string][]XrefCallSite)

	// Walk the AST looking for selector expressions that match imported packages.
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		importPath, ok := imports[ident.Name]
		if !ok {
			return true
		}

		qualName := importPath + "." + sel.Sel.Name
		pos := fset.Position(sel.Pos())

		entity := funcName(pos)

		refs[qualName] = append(refs[qualName], XrefCallSite{
			File:   filename,
			Entity: entity,
			Line:   pos.Line,
		})

		return true
	})

	return refs, nil
}

// buildFuncPositionMap returns a function that maps a position to the name
// of the enclosing function/method declaration.
func buildFuncPositionMap(f *ast.File, fset *token.FileSet) func(token.Position) string {
	type funcRange struct {
		name  string
		start int
		end   int
	}
	var ranges []funcRange

	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := fd.Name.Name
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			recv := typeString(fd.Recv.List[0].Type)
			recv = strings.TrimPrefix(recv, "*")
			name = recv + "." + name
		}
		start := fset.Position(fd.Pos()).Line
		end := fset.Position(fd.End()).Line
		ranges = append(ranges, funcRange{name: name, start: start, end: end})
	}

	return func(pos token.Position) string {
		for _, r := range ranges {
			if pos.Line >= r.start && pos.Line <= r.end {
				return r.name
			}
		}
		return "<top-level>"
	}
}

// SaveXrefIndex serializes the xref index to a JSON blob and stores it
// at refs/coord/meta/xrefs.
func (c *Coordinator) SaveXrefIndex(idx *XrefIndex) error {
	h, err := c.writeJSONBlob(idx)
	if err != nil {
		return fmt.Errorf("save xref index: %w", err)
	}
	ref := refPath("meta", "xrefs")
	return c.Repo.UpdateRef(ref, h)
}

// LoadXrefIndex reads the xref index from refs/coord/meta/xrefs.
func (c *Coordinator) LoadXrefIndex() (*XrefIndex, error) {
	ref := refPath("meta", "xrefs")
	h, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return nil, fmt.Errorf("xref index not found: %w", err)
	}
	var idx XrefIndex
	if err := c.readJSONBlob(h, &idx); err != nil {
		return nil, fmt.Errorf("read xref index: %w", err)
	}
	return &idx, nil
}
