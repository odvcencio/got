package coord

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/odvcencio/graft/pkg/repo"
)

// ExportIndex maps package paths to their exported entities.
type ExportIndex struct {
	Packages map[string]map[string]ExportedEntity `json:"packages"`
}

// ExportedEntity describes a single exported symbol.
type ExportedEntity struct {
	Key       string `json:"key"`
	Signature string `json:"signature"`
	File      string `json:"file"`
	Hash      string `json:"hash"`
}

// BuildExportIndex scans the HEAD commit tree for Go source files and builds
// an index of all exported symbols (capitalized names). It uses go/parser
// for lightweight AST extraction rather than tree-sitter.
func BuildExportIndex(r *repo.Repo) (*ExportIndex, error) {
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve HEAD: %w", err)
	}

	commit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("read HEAD commit: %w", err)
	}

	entries, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("flatten tree: %w", err)
	}

	idx := &ExportIndex{
		Packages: make(map[string]map[string]ExportedEntity),
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Path, ".go") {
			continue
		}
		if strings.HasSuffix(entry.Path, "_test.go") {
			continue
		}

		blob, err := r.Store.ReadBlob(entry.BlobHash)
		if err != nil {
			continue // skip unreadable blobs
		}

		pkgDir := path.Dir(entry.Path)
		if pkgDir == "." {
			pkgDir = ""
		}

		exported, err := extractExportedSymbols(entry.Path, blob.Data, string(entry.BlobHash))
		if err != nil {
			continue // skip unparseable files
		}

		if len(exported) == 0 {
			continue
		}

		if _, ok := idx.Packages[pkgDir]; !ok {
			idx.Packages[pkgDir] = make(map[string]ExportedEntity)
		}

		for _, exp := range exported {
			exp.File = entry.Path
			idx.Packages[pkgDir][exp.Key] = exp
		}
	}

	return idx, nil
}

// extractExportedSymbols parses a Go file and returns exported function, type,
// var, and const declarations with their signatures.
func extractExportedSymbols(filename string, source []byte, blobHash string) ([]ExportedEntity, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, source, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var result []ExportedEntity

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if !isExported(name) {
				continue
			}
			sig := funcSignature(d)
			key := funcKey(d)
			result = append(result, ExportedEntity{
				Key:       key,
				Signature: sig,
				Hash:      blobHash,
			})

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					name := s.Name.Name
					if !isExported(name) {
						continue
					}
					key := "type:" + name
					sig := "type " + name
					result = append(result, ExportedEntity{
						Key:       key,
						Signature: sig,
						Hash:      blobHash,
					})

				case *ast.ValueSpec:
					for _, ident := range s.Names {
						if !isExported(ident.Name) {
							continue
						}
						kind := "var"
						if d.Tok == token.CONST {
							kind = "const"
						}
						key := kind + ":" + ident.Name
						sig := kind + " " + ident.Name
						result = append(result, ExportedEntity{
							Key:       key,
							Signature: sig,
							Hash:      blobHash,
						})
					}
				}
			}
		}
	}

	return result, nil
}

// isExported returns true if the name starts with an uppercase letter.
func isExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// funcSignature returns a human-readable signature for a function or method.
func funcSignature(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		b.WriteString("(")
		for i, field := range d.Recv.List {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(typeString(field.Type))
		}
		b.WriteString(") ")
	}
	b.WriteString(d.Name.Name)
	b.WriteString("(")
	if d.Type.Params != nil {
		writeFieldList(&b, d.Type.Params.List)
	}
	b.WriteString(")")
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		b.WriteString(" ")
		if len(d.Type.Results.List) == 1 && len(d.Type.Results.List[0].Names) == 0 {
			b.WriteString(typeString(d.Type.Results.List[0].Type))
		} else {
			b.WriteString("(")
			writeFieldList(&b, d.Type.Results.List)
			b.WriteString(")")
		}
	}
	return b.String()
}

// funcKey returns a unique key for a function or method declaration.
func funcKey(d *ast.FuncDecl) string {
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := typeString(d.Recv.List[0].Type)
		// Strip pointer prefix for key consistency.
		recv = strings.TrimPrefix(recv, "*")
		return "method:" + recv + "." + d.Name.Name
	}
	return "func:" + d.Name.Name
}

// typeString returns a string representation of a Go AST type expression.
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeString(t.Elt)
		}
		return "[...]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + typeString(t.Elt)
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		return "chan " + typeString(t.Value)
	default:
		return "?"
	}
}

// writeFieldList writes a comma-separated list of fields.
func writeFieldList(b *strings.Builder, fields []*ast.Field) {
	first := true
	for _, field := range fields {
		ts := typeString(field.Type)
		if len(field.Names) == 0 {
			if !first {
				b.WriteString(", ")
			}
			b.WriteString(ts)
			first = false
		} else {
			for _, name := range field.Names {
				if !first {
					b.WriteString(", ")
				}
				b.WriteString(name.Name + " " + ts)
				first = false
			}
		}
	}
}

// SaveExportIndex serializes the export index to a JSON blob and stores it
// at refs/coord/meta/exports.
func (c *Coordinator) SaveExportIndex(idx *ExportIndex) error {
	h, err := c.writeJSONBlob(idx)
	if err != nil {
		return fmt.Errorf("save export index: %w", err)
	}
	ref := refPath("meta", "exports")
	return c.Repo.UpdateRef(ref, h)
}

// LoadExportIndex reads the export index from refs/coord/meta/exports.
func (c *Coordinator) LoadExportIndex() (*ExportIndex, error) {
	ref := refPath("meta", "exports")
	h, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return nil, fmt.Errorf("export index not found: %w", err)
	}
	var idx ExportIndex
	if err := c.readJSONBlob(h, &idx); err != nil {
		return nil, fmt.Errorf("read export index: %w", err)
	}
	return &idx, nil
}
