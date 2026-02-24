package merge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/diff3"
)

// MergeImports performs set-based merge of import blocks.
// Returns merged bytes and whether there was an unresolvable conflict.
func MergeImports(base, ours, theirs []byte, language string) ([]byte, bool) {
	switch language {
	case "go":
		return mergeGoImports(base, ours, theirs)
	case "python":
		return mergePythonImports(base, ours, theirs)
	case "javascript", "typescript":
		return mergeJSImportBlocks(base, ours, theirs)
	case "rust":
		return mergeRustImports(base, ours, theirs)
	default:
		result := diff3.Merge(base, ours, theirs)
		return result.Merged, result.HasConflicts
	}
}

func mergeGoImports(base, ours, theirs []byte) ([]byte, bool) {
	baseImports := parseGoImportSpecs(string(base))
	oursImports := parseGoImportSpecs(string(ours))
	theirsImports := parseGoImportSpecs(string(theirs))

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

	return formatGoImports(result), false
}

func parseGoImportSpecs(src string) []string {
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

func formatGoImports(imports []string) []byte {
	if len(imports) == 0 {
		return nil
	}

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
}

type importEntry struct {
	key  string
	line string
}

func mergePythonImports(base, ours, theirs []byte) ([]byte, bool) {
	return mergeEntryImports(parsePythonImportEntries(string(base)), parsePythonImportEntries(string(ours)), parsePythonImportEntries(string(theirs)))
}

func parsePythonImportEntries(src string) []importEntry {
	var out []importEntry
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, ";"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "from ") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "from "))
			module, namesRaw, ok := strings.Cut(rest, " import ")
			if !ok {
				continue
			}
			module = strings.TrimSpace(module)
			namesRaw = strings.TrimSpace(namesRaw)
			for _, name := range splitCSV(namesRaw) {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				key := "from:" + module + ":" + normalizeImportAliasKey(name)
				out = append(out, importEntry{key: key, line: "from " + module + " import " + name})
			}
			continue
		}
		if strings.HasPrefix(line, "import ") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "import "))
			for _, segment := range splitCSV(rest) {
				segment = strings.TrimSpace(segment)
				if segment == "" {
					continue
				}
				key := "import:" + normalizeImportAliasKey(segment)
				out = append(out, importEntry{key: key, line: "import " + segment})
			}
		}
	}
	return out
}

type jsImport struct {
	module     string
	defaultAs  string
	namespace  string
	named      map[string]struct{}
	sideEffect bool
	typeOnly   bool
}

func mergeJSImportBlocks(base, ours, theirs []byte) ([]byte, bool) {
	baseMap := parseJSImportMap(string(base))
	oursMap := parseJSImportMap(string(ours))
	theirsMap := parseJSImportMap(string(theirs))

	baseSet := moduleSet(baseMap)
	oursSet := moduleSet(oursMap)
	theirsSet := moduleSet(theirsMap)

	mergedModules := make(map[string]struct{}, len(oursSet)+len(theirsSet))
	for module := range oursSet {
		mergedModules[module] = struct{}{}
	}
	for module := range theirsSet {
		mergedModules[module] = struct{}{}
	}
	for module := range baseSet {
		if _, inOurs := oursSet[module]; inOurs {
			continue
		}
		if _, inTheirs := theirsSet[module]; inTheirs {
			continue
		}
		delete(mergedModules, module)
	}

	modules := make([]string, 0, len(mergedModules))
	for module := range mergedModules {
		modules = append(modules, module)
	}
	sort.Strings(modules)

	lines := make([]string, 0, len(modules))
	for _, module := range modules {
		merged, ok := mergeJSImportSpec(module, oursMap[module], theirsMap[module], baseMap[module])
		if !ok {
			return nil, true
		}
		lines = append(lines, renderJSImport(merged))
	}
	return joinImportLines(lines), false
}

func parseJSImportMap(src string) map[string]jsImport {
	out := map[string]jsImport{}
	for _, stmt := range splitImportStatements(src) {
		spec, ok := parseJSImportStatement(stmt)
		if !ok {
			continue
		}
		prev, exists := out[spec.module]
		if !exists {
			out[spec.module] = spec
			continue
		}
		merged, ok := mergeJSImportSpec(spec.module, prev, spec, jsImport{})
		if ok {
			out[spec.module] = merged
		}
	}
	return out
}

func splitImportStatements(src string) []string {
	src = strings.ReplaceAll(src, "\n", ";")
	parts := strings.Split(src, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "import ") {
			out = append(out, part)
		}
	}
	return out
}

func parseJSImportStatement(stmt string) (jsImport, bool) {
	stmt = strings.TrimSpace(strings.TrimSuffix(stmt, ";"))
	if !strings.HasPrefix(stmt, "import ") {
		return jsImport{}, false
	}

	module := ""
	quoted := extractQuoted(stmt)
	if len(quoted) > 0 {
		module = quoted[len(quoted)-1]
	}
	if module == "" {
		return jsImport{}, false
	}

	spec := jsImport{
		module: module,
		named:  map[string]struct{}{},
	}

	if !strings.Contains(stmt, " from ") {
		spec.sideEffect = true
		return spec, true
	}

	clause := strings.TrimSpace(strings.TrimPrefix(stmt, "import "))
	clause = strings.TrimSpace(clause[:strings.Index(clause, " from ")])
	if strings.HasPrefix(clause, "type ") {
		spec.typeOnly = true
		clause = strings.TrimSpace(strings.TrimPrefix(clause, "type "))
	}

	if clause == "" {
		spec.sideEffect = true
		return spec, true
	}

	segments := splitCSV(clause)
	if len(segments) == 0 {
		return spec, true
	}
	if len(segments) > 1 {
		spec.defaultAs = strings.TrimSpace(segments[0])
		segments = segments[1:]
	}
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		switch {
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
			for _, named := range splitCSV(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}"))) {
				named = strings.TrimSpace(named)
				if named != "" {
					spec.named[named] = struct{}{}
				}
			}
		case strings.HasPrefix(seg, "* as "):
			spec.namespace = strings.TrimSpace(strings.TrimPrefix(seg, "* as "))
		default:
			if spec.defaultAs == "" {
				spec.defaultAs = seg
			}
		}
	}
	return spec, true
}

func mergeJSImportSpec(module string, left, right, base jsImport) (jsImport, bool) {
	merged := jsImport{
		module: module,
		named:  map[string]struct{}{},
	}
	if left.module == "" {
		left.module = module
	}
	if right.module == "" {
		right.module = module
	}
	if base.module == "" {
		base.module = module
	}

	merged.sideEffect = left.sideEffect || right.sideEffect
	merged.typeOnly = (left.typeOnly || right.typeOnly) && !(left.defaultAs != "" || right.defaultAs != "" || left.namespace != "" || right.namespace != "")

	if left.defaultAs != "" && right.defaultAs != "" && left.defaultAs != right.defaultAs {
		return jsImport{}, false
	}
	if left.namespace != "" && right.namespace != "" && left.namespace != right.namespace {
		return jsImport{}, false
	}
	if left.defaultAs != "" {
		merged.defaultAs = left.defaultAs
	} else if right.defaultAs != "" {
		merged.defaultAs = right.defaultAs
	} else {
		merged.defaultAs = base.defaultAs
	}
	if left.namespace != "" {
		merged.namespace = left.namespace
	} else if right.namespace != "" {
		merged.namespace = right.namespace
	} else {
		merged.namespace = base.namespace
	}

	for name := range base.named {
		merged.named[name] = struct{}{}
	}
	for name := range left.named {
		merged.named[name] = struct{}{}
	}
	for name := range right.named {
		merged.named[name] = struct{}{}
	}

	if len(left.named) == 0 && len(right.named) == 0 && len(base.named) == 0 {
		merged.named = nil
	}
	return merged, true
}

func renderJSImport(spec jsImport) string {
	if spec.sideEffect && spec.defaultAs == "" && spec.namespace == "" && len(spec.named) == 0 {
		return fmt.Sprintf("import %q;", spec.module)
	}

	var parts []string
	if spec.defaultAs != "" {
		parts = append(parts, spec.defaultAs)
	}
	if spec.namespace != "" {
		parts = append(parts, "* as "+spec.namespace)
	}
	if len(spec.named) > 0 {
		named := make([]string, 0, len(spec.named))
		for name := range spec.named {
			named = append(named, name)
		}
		sort.Strings(named)
		parts = append(parts, "{ "+strings.Join(named, ", ")+" }")
	}
	if len(parts) == 0 {
		return fmt.Sprintf("import %q;", spec.module)
	}
	prefix := "import"
	if spec.typeOnly {
		prefix = "import type"
	}
	return fmt.Sprintf("%s %s from %q;", prefix, strings.Join(parts, ", "), spec.module)
}

func mergeRustImports(base, ours, theirs []byte) ([]byte, bool) {
	return mergeEntryImports(parseRustImportEntries(string(base)), parseRustImportEntries(string(ours)), parseRustImportEntries(string(theirs)))
}

func parseRustImportEntries(src string) []importEntry {
	var out []importEntry
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		prefix := ""
		switch {
		case strings.HasPrefix(line, "pub use "):
			prefix = "pub use "
		case strings.HasPrefix(line, "use "):
			prefix = "use "
		default:
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		path = strings.TrimSuffix(path, ";")
		if path == "" {
			continue
		}
		key := prefix + strings.ReplaceAll(path, " ", "")
		out = append(out, importEntry{key: key, line: prefix + path + ";"})
	}
	return out
}

func mergeEntryImports(base, ours, theirs []importEntry) ([]byte, bool) {
	baseMap := entryMap(base)
	oursMap := entryMap(ours)
	theirsMap := entryMap(theirs)

	baseSet := keySet(baseMap)
	oursSet := keySet(oursMap)
	theirsSet := keySet(theirsMap)

	merged := make(map[string]string, len(oursSet)+len(theirsSet))
	for key, line := range oursMap {
		merged[key] = line
	}
	for key, line := range theirsMap {
		if _, exists := merged[key]; !exists {
			merged[key] = line
		}
	}
	for key := range baseSet {
		if _, inOurs := oursSet[key]; inOurs {
			continue
		}
		if _, inTheirs := theirsSet[key]; inTheirs {
			continue
		}
		delete(merged, key)
	}

	if len(merged) == 0 {
		return nil, false
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, merged[key])
	}
	return joinImportLines(lines), false
}

func entryMap(entries []importEntry) map[string]string {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.key) == "" || strings.TrimSpace(entry.line) == "" {
			continue
		}
		out[entry.key] = entry.line
	}
	return out
}

func keySet(m map[string]string) map[string]bool {
	out := make(map[string]bool, len(m))
	for key := range m {
		out[key] = true
	}
	return out
}

func moduleSet(m map[string]jsImport) map[string]bool {
	out := make(map[string]bool, len(m))
	for module := range m {
		out[module] = true
	}
	return out
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeImportAliasKey(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if base, _, ok := strings.Cut(token, " as "); ok {
		token = strings.TrimSpace(base)
	}
	return token
}

func extractQuoted(raw string) []string {
	var out []string
	for i := 0; i < len(raw); i++ {
		quote := raw[i]
		if quote != '"' && quote != '\'' && quote != '`' {
			continue
		}
		start := i + 1
		for j := start; j < len(raw); j++ {
			if raw[j] == '\\' {
				j++
				continue
			}
			if raw[j] == quote {
				out = append(out, raw[start:j])
				i = j
				break
			}
		}
	}
	return out
}

func joinImportLines(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	b.WriteByte('\n')
	return []byte(b.String())
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}
