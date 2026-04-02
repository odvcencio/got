package merge

import (
	"sort"
	"strings"
)

// MergeStructFields performs set-union merge of struct/type fields.
// Returns merged bytes and whether there was an unresolvable conflict.
// Supports Go structs (type_declaration), Rust structs (struct_item).
func MergeStructFields(base, ours, theirs []byte, language string) ([]byte, bool) {
	switch language {
	case "go":
		return mergeGoStructFields(base, ours, theirs)
	case "rust":
		return mergeRustStructFields(base, ours, theirs)
	default:
		// Unsupported language — signal conflict so caller falls back to diff3.
		return nil, true
	}
}

// structField represents a single field inside a struct.
type structField struct {
	name     string // field name (the identity key)
	typExpr  string // type expression (for conflict detection on type changes)
	line     string // full original line text (preserved for output)
	tag      string // Go struct tag, if any
	embedded bool   // true if this is an embedded/anonymous field (Go)
}

// --- Go struct merge ---

func mergeGoStructFields(base, ours, theirs []byte) ([]byte, bool) {
	baseName, baseFields := parseGoStruct(string(base))
	oursName, oursFields := parseGoStruct(string(ours))
	theirsName, theirsFields := parseGoStruct(string(theirs))

	// Use ours name (it may have been renamed upstream).
	name := oursName
	if name == "" {
		name = baseName
	}
	if name == "" {
		name = theirsName
	}

	merged, conflict := mergeFieldSets(baseFields, oursFields, theirsFields)
	if conflict {
		return nil, true
	}

	return formatGoStruct(name, merged), false
}

// parseGoStruct parses a Go type declaration with a struct body.
// It handles both:
//
//	type Foo struct {
//	    Name string
//	}
//
// and the body-only form (just the fields extracted from within the entity body).
func parseGoStruct(src string) (string, []structField) {
	lines := strings.Split(src, "\n")
	var fields []structField
	name := ""
	inBody := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Detect struct header: "type Foo struct {"
		if !inBody && strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, "struct") {
			// Extract name between "type " and " struct"
			rest := strings.TrimPrefix(trimmed, "type ")
			idx := strings.Index(rest, " struct")
			if idx > 0 {
				name = strings.TrimSpace(rest[:idx])
			}
			if strings.HasSuffix(trimmed, "{") {
				inBody = true
			}
			continue
		}

		// Skip opening/closing braces
		if trimmed == "{" {
			inBody = true
			continue
		}
		if trimmed == "}" {
			inBody = false
			continue
		}

		if !inBody && name == "" {
			// If no header yet and no body marker, treat as raw fields
			// (when the entity body is just the struct declaration itself)
			inBody = true
		}

		if inBody {
			field := parseGoStructField(trimmed)
			if field.name != "" {
				fields = append(fields, field)
			}
		}
	}

	return name, fields
}

// parseGoStructField parses a single Go struct field line.
// Examples:
//
//	Name string `json:"name"`
//	Age  int
//	io.Writer           (embedded)
//	*http.Client        (embedded pointer)
func parseGoStructField(line string) structField {
	// Strip inline comments
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if line == "" {
		return structField{}
	}

	// Extract struct tag (backtick-delimited)
	tag := ""
	if idx := strings.Index(line, "`"); idx >= 0 {
		endIdx := strings.LastIndex(line, "`")
		if endIdx > idx {
			tag = line[idx : endIdx+1]
			line = strings.TrimSpace(line[:idx])
		}
	}

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return structField{}
	}

	// Single token = embedded type (e.g., "io.Writer", "*Mutex")
	if len(parts) == 1 {
		return structField{
			name:     parts[0],
			typExpr:  parts[0],
			line:     strings.TrimSpace(line),
			tag:      tag,
			embedded: true,
		}
	}

	// Multiple tokens: first is name, rest is type
	fieldName := parts[0]
	typExpr := strings.Join(parts[1:], " ")

	return structField{
		name:    fieldName,
		typExpr: typExpr,
		line:    strings.TrimSpace(line),
		tag:     tag,
	}
}

func formatGoStruct(name string, fields []structField) []byte {
	var b strings.Builder
	b.WriteString("type ")
	b.WriteString(name)
	b.WriteString(" struct {\n")
	for _, f := range fields {
		b.WriteString("\t")
		b.WriteString(f.line)
		if f.tag != "" {
			b.WriteString(" ")
			b.WriteString(f.tag)
		}
		b.WriteString("\n")
	}
	b.WriteString("}")
	return []byte(b.String())
}

// --- Rust struct merge ---

func mergeRustStructFields(base, ours, theirs []byte) ([]byte, bool) {
	baseName, baseVis, baseFields := parseRustStruct(string(base))
	oursName, oursVis, oursFields := parseRustStruct(string(ours))
	theirsName, _, theirsFields := parseRustStruct(string(theirs))

	name := oursName
	if name == "" {
		name = baseName
	}
	if name == "" {
		name = theirsName
	}

	vis := oursVis
	if vis == "" {
		vis = baseVis
	}

	merged, conflict := mergeFieldSets(baseFields, oursFields, theirsFields)
	if conflict {
		return nil, true
	}

	return formatRustStruct(name, vis, merged), false
}

// parseRustStruct parses a Rust struct declaration.
// Examples:
//
//	pub struct Config {
//	    pub name: String,
//	    pub port: u16,
//	}
//
//	struct Point {
//	    x: f64,
//	    y: f64,
//	}
func parseRustStruct(src string) (name, visibility string, fields []structField) {
	lines := strings.Split(src, "\n")
	inBody := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Detect struct header: "pub struct Foo {" or "struct Foo {"
		if !inBody && strings.Contains(trimmed, "struct ") {
			vis, rest := extractRustVisibility(trimmed)
			visibility = vis
			rest = strings.TrimPrefix(rest, "struct ")
			// Extract name (everything before { or whitespace)
			rest = strings.TrimSpace(rest)
			if idx := strings.IndexAny(rest, " {"); idx > 0 {
				name = rest[:idx]
			} else {
				name = rest
			}
			if strings.Contains(trimmed, "{") {
				inBody = true
			}
			continue
		}

		if trimmed == "{" {
			inBody = true
			continue
		}
		if trimmed == "}" {
			inBody = false
			continue
		}

		if inBody {
			field := parseRustStructField(trimmed)
			if field.name != "" {
				fields = append(fields, field)
			}
		}
	}

	return name, visibility, fields
}

func extractRustVisibility(line string) (vis, rest string) {
	if strings.HasPrefix(line, "pub(crate) ") {
		return "pub(crate)", strings.TrimPrefix(line, "pub(crate) ")
	}
	if strings.HasPrefix(line, "pub(super) ") {
		return "pub(super)", strings.TrimPrefix(line, "pub(super) ")
	}
	if strings.HasPrefix(line, "pub ") {
		return "pub", strings.TrimPrefix(line, "pub ")
	}
	return "", line
}

// parseRustStructField parses a single Rust struct field.
// Examples:
//
//	pub name: String,
//	port: u16,
func parseRustStructField(line string) structField {
	// Strip inline comments
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	// Remove trailing comma
	line = strings.TrimSuffix(strings.TrimSpace(line), ",")
	line = strings.TrimSpace(line)
	if line == "" {
		return structField{}
	}

	// Handle visibility prefix
	vis, rest := extractRustVisibility(line)

	// Split on first colon for name: type
	colonIdx := strings.Index(rest, ":")
	if colonIdx < 0 {
		return structField{}
	}

	fieldName := strings.TrimSpace(rest[:colonIdx])
	typExpr := strings.TrimSpace(rest[colonIdx+1:])

	// Reconstruct the original line for output
	fullLine := fieldName + ": " + typExpr
	if vis != "" {
		fullLine = vis + " " + fullLine
	}

	return structField{
		name:    fieldName,
		typExpr: typExpr,
		line:    fullLine,
	}
}

func formatRustStruct(name, visibility string, fields []structField) []byte {
	var b strings.Builder
	if visibility != "" {
		b.WriteString(visibility)
		b.WriteString(" ")
	}
	b.WriteString("struct ")
	b.WriteString(name)
	b.WriteString(" {\n")
	for _, f := range fields {
		b.WriteString("    ")
		b.WriteString(f.line)
		b.WriteString(",\n")
	}
	b.WriteString("}")
	return []byte(b.String())
}

// --- Common field set-union logic ---

// mergeFieldSets performs set-union merge on struct fields.
// Union of ours and theirs, minus one-sided deletions from base.
// Type changes on the same field name = conflict.
func mergeFieldSets(baseFields, oursFields, theirsFields []structField) ([]structField, bool) {
	baseMap := fieldMap(baseFields)
	oursMap := fieldMap(oursFields)
	theirsMap := fieldMap(theirsFields)

	// Check for type conflicts: same field name, different type in ours vs theirs.
	for name, oursField := range oursMap {
		if theirsField, ok := theirsMap[name]; ok {
			if oursField.typExpr != theirsField.typExpr {
				// Both sides changed the type differently — check if it's a real conflict.
				if baseField, inBase := baseMap[name]; inBase {
					// If one side kept base type and the other changed, that's not a conflict —
					// take the changed version.
					if oursField.typExpr == baseField.typExpr || theirsField.typExpr == baseField.typExpr {
						continue
					}
				}
				// Both sides changed type differently from base (or field is new on both sides
				// with different types) — real conflict.
				return nil, true
			}
		}
	}

	// Build merged set: start with union of ours + theirs.
	merged := make(map[string]structField, len(oursMap)+len(theirsMap))
	for name, f := range oursMap {
		merged[name] = f
	}
	for name, f := range theirsMap {
		if _, exists := merged[name]; !exists {
			merged[name] = f
		} else {
			// Both have it — prefer ours unless theirs changed from base.
			oursField := oursMap[name]
			if baseField, inBase := baseMap[name]; inBase {
				if oursField.typExpr == baseField.typExpr && f.typExpr != baseField.typExpr {
					// Theirs changed the type, ours kept base — take theirs.
					merged[name] = f
				}
				// If both changed identically, or ours changed, keep ours (already set).
			}
		}
	}

	// Remove fields that were in base but removed by one side
	// (when the other side kept them unchanged from base).
	for name := range baseMap {
		_, inOurs := oursMap[name]
		_, inTheirs := theirsMap[name]
		if !inOurs && inTheirs {
			// Ours removed, theirs kept from base -> honor deletion.
			delete(merged, name)
		} else if inOurs && !inTheirs {
			// Theirs removed, ours kept from base -> honor deletion.
			delete(merged, name)
		}
	}

	// Sort fields for deterministic output.
	// Preserve relative ordering: base fields first (in base order),
	// then new fields from ours (in ours order), then new from theirs (in theirs order).
	result := orderFields(merged, baseFields, oursFields, theirsFields)

	return result, false
}

// fieldMap builds a name -> structField map.
func fieldMap(fields []structField) map[string]structField {
	m := make(map[string]structField, len(fields))
	for _, f := range fields {
		m[f.name] = f
	}
	return m
}

// orderFields produces a stable ordering for the merged field set.
// Fields present in base come first in their base order, then new fields
// from ours in ours order, then new fields from theirs in theirs order.
func orderFields(merged map[string]structField, base, ours, theirs []structField) []structField {
	seen := make(map[string]bool, len(merged))
	var result []structField

	// First: fields from base in base order (if still in merged).
	for _, f := range base {
		if _, ok := merged[f.name]; ok && !seen[f.name] {
			result = append(result, merged[f.name])
			seen[f.name] = true
		}
	}

	// Second: new fields from ours in ours order.
	for _, f := range ours {
		if _, ok := merged[f.name]; ok && !seen[f.name] {
			result = append(result, merged[f.name])
			seen[f.name] = true
		}
	}

	// Third: new fields from theirs in theirs order.
	for _, f := range theirs {
		if _, ok := merged[f.name]; ok && !seen[f.name] {
			result = append(result, merged[f.name])
			seen[f.name] = true
		}
	}

	// Safety: any remaining fields (shouldn't happen, but be safe).
	if len(result) < len(merged) {
		remaining := make([]string, 0, len(merged)-len(result))
		for name := range merged {
			if !seen[name] {
				remaining = append(remaining, name)
			}
		}
		sort.Strings(remaining)
		for _, name := range remaining {
			result = append(result, merged[name])
		}
	}

	return result
}
