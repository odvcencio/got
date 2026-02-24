package merge

import (
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/diff3"
)

// MergeImports performs set-based merge of import blocks.
// Returns merged bytes and whether there was an unresolvable conflict.
func MergeImports(base, ours, theirs []byte, language string) ([]byte, bool) {
	// Safe structural import merge is currently implemented for Go only.
	// For other languages, preserve syntax by delegating to line-level diff3.
	if language != "go" {
		result := diff3.Merge(base, ours, theirs)
		return result.Merged, result.HasConflicts
	}

	baseImports := parseImportLines(string(base), language)
	oursImports := parseImportLines(string(ours), language)
	theirsImports := parseImportLines(string(theirs), language)

	baseSet := toSet(baseImports)
	oursSet := toSet(oursImports)
	theirsSet := toSet(theirsImports)

	// Union of ours and theirs, minus anything both removed from base
	merged := map[string]bool{}
	for imp := range oursSet {
		merged[imp] = true
	}
	for imp := range theirsSet {
		merged[imp] = true
	}

	// Remove imports that were in base but removed by BOTH sides
	for imp := range baseSet {
		removedByOurs := !oursSet[imp]
		removedByTheirs := !theirsSet[imp]
		if removedByOurs && removedByTheirs {
			delete(merged, imp)
		}
	}

	// Sort for deterministic output
	result := make([]string, 0, len(merged))
	for imp := range merged {
		result = append(result, imp)
	}
	sort.Strings(result)

	return formatImports(result, language), false
}

func parseImportLines(src string, language string) []string {
	var imports []string
	lines := strings.Split(src, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "import" || line == "(" || line == ")" {
			continue
		}
		// Strip leading "import " keyword if present
		imp := strings.TrimPrefix(line, "import ")
		imp = strings.TrimPrefix(imp, "from ")
		imp = strings.TrimSpace(imp)
		if imp == "" || imp == "(" || imp == ")" {
			continue
		}
		imports = append(imports, imp)
	}
	return imports
}

func formatImports(imports []string, language string) []byte {
	if len(imports) == 0 {
		return nil
	}

	switch language {
	case "go":
		if len(imports) == 1 {
			return []byte("import " + imports[0])
		}
		var b strings.Builder
		b.WriteString("import (\n")
		for _, imp := range imports {
			b.WriteString("\t")
			b.WriteString(imp)
			b.WriteString("\n")
		}
		b.WriteString(")")
		return []byte(b.String())
	default:
		return nil
	}
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}
