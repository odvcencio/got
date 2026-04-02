package merge

import (
	"sort"
	"strings"
)

// MergeInterfaceMembers performs set-union merge of interface/trait members.
// Returns merged bytes and whether there was an unresolvable conflict.
// Supports Go interfaces (type_declaration) and TypeScript interfaces (interface_declaration).
func MergeInterfaceMembers(base, ours, theirs []byte, language string) ([]byte, bool) {
	switch language {
	case "go":
		return mergeGoInterfaceMembers(base, ours, theirs)
	case "typescript", "javascript":
		return mergeTSInterfaceMembers(base, ours, theirs)
	default:
		// Unsupported language — signal conflict so caller falls back to diff3.
		return nil, true
	}
}

// interfaceMember represents a single member inside an interface.
type interfaceMember struct {
	name    string // member name (the identity key)
	sigExpr string // type/signature expression (for conflict detection)
	line    string // full original line text (preserved for output)
}

// --- Go interface merge ---

func mergeGoInterfaceMembers(base, ours, theirs []byte) ([]byte, bool) {
	baseName, baseMembers := parseGoInterface(string(base))
	oursName, oursMembers := parseGoInterface(string(ours))
	theirsName, theirsMembers := parseGoInterface(string(theirs))

	name := oursName
	if name == "" {
		name = baseName
	}
	if name == "" {
		name = theirsName
	}

	merged, conflict := mergeMemberSets(baseMembers, oursMembers, theirsMembers)
	if conflict {
		return nil, true
	}

	return formatGoInterface(name, merged), false
}

// parseGoInterface parses a Go type declaration with an interface body.
// Examples:
//
//	type Reader interface {
//	    Read(p []byte) (n int, err error)
//	}
func parseGoInterface(src string) (string, []interfaceMember) {
	lines := strings.Split(src, "\n")
	var members []interfaceMember
	name := ""
	inBody := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Detect interface header: "type Foo interface {"
		if !inBody && strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, "interface") {
			rest := strings.TrimPrefix(trimmed, "type ")
			idx := strings.Index(rest, " interface")
			if idx > 0 {
				name = strings.TrimSpace(rest[:idx])
			}
			if strings.HasSuffix(trimmed, "{") {
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
			member := parseGoInterfaceMember(trimmed)
			if member.name != "" {
				members = append(members, member)
			}
		}
	}

	return name, members
}

// parseGoInterfaceMember parses a single Go interface member line.
// Examples:
//
//	Read(p []byte) (n int, err error)
//	io.Writer                              (embedded interface)
//	Close() error
func parseGoInterfaceMember(line string) interfaceMember {
	// Strip inline comments
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if line == "" {
		return interfaceMember{}
	}

	// Method: name followed by (
	if idx := strings.Index(line, "("); idx > 0 {
		name := strings.TrimSpace(line[:idx])
		// The signature is everything from ( onwards
		sigExpr := strings.TrimSpace(line[idx:])
		return interfaceMember{
			name:    name,
			sigExpr: sigExpr,
			line:    line,
		}
	}

	// Embedded interface (e.g., "io.Writer", "fmt.Stringer")
	return interfaceMember{
		name:    line,
		sigExpr: line,
		line:    line,
	}
}

func formatGoInterface(name string, members []interfaceMember) []byte {
	var b strings.Builder
	b.WriteString("type ")
	b.WriteString(name)
	b.WriteString(" interface {\n")
	for _, m := range members {
		b.WriteString("\t")
		b.WriteString(m.line)
		b.WriteString("\n")
	}
	b.WriteString("}")
	return []byte(b.String())
}

// --- TypeScript interface merge ---

func mergeTSInterfaceMembers(base, ours, theirs []byte) ([]byte, bool) {
	baseName, _, baseMembers := parseTSInterface(string(base))
	oursName, oursExported, oursMembers := parseTSInterface(string(ours))
	theirsName, theirsExported, theirsMembers := parseTSInterface(string(theirs))

	name := oursName
	if name == "" {
		name = baseName
	}
	if name == "" {
		name = theirsName
	}

	exported := oursExported || theirsExported

	merged, conflict := mergeMemberSets(baseMembers, oursMembers, theirsMembers)
	if conflict {
		return nil, true
	}

	return formatTSInterface(name, exported, merged), false
}

// parseTSInterface parses a TypeScript interface declaration.
// Examples:
//
//	interface Config {
//	    host: string;
//	    port: number;
//	}
//
//	export interface Config {
//	    host: string;
//	}
func parseTSInterface(src string) (name string, exported bool, members []interfaceMember) {
	lines := strings.Split(src, "\n")
	inBody := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Detect interface header
		if !inBody && strings.Contains(trimmed, "interface ") {
			if strings.HasPrefix(trimmed, "export ") {
				exported = true
				trimmed = strings.TrimPrefix(trimmed, "export ")
				trimmed = strings.TrimSpace(trimmed)
			}
			rest := strings.TrimPrefix(trimmed, "interface ")
			// Handle "interface Foo extends Bar {"
			if idx := strings.IndexAny(rest, " {"); idx > 0 {
				name = strings.TrimSpace(rest[:idx])
			} else {
				name = strings.TrimSpace(rest)
			}
			if strings.Contains(line, "{") {
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
			member := parseTSInterfaceMember(trimmed)
			if member.name != "" {
				members = append(members, member)
			}
		}
	}

	return name, exported, members
}

// parseTSInterfaceMember parses a single TypeScript interface member.
// Examples:
//
//	host: string;
//	port: number;
//	readonly name: string;
//	greet(name: string): void;
//	[key: string]: any;
func parseTSInterfaceMember(line string) interfaceMember {
	// Strip inline comments
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	// Remove trailing semicolon
	line = strings.TrimSuffix(strings.TrimSpace(line), ";")
	line = strings.TrimSpace(line)
	if line == "" {
		return interfaceMember{}
	}

	// Handle readonly prefix
	cleanLine := line
	if strings.HasPrefix(cleanLine, "readonly ") {
		cleanLine = strings.TrimPrefix(cleanLine, "readonly ")
		cleanLine = strings.TrimSpace(cleanLine)
	}

	// Handle optional marker
	cleanLine = strings.Replace(cleanLine, "?:", ":", 1)

	// Method signature: name followed by (
	if idx := strings.Index(cleanLine, "("); idx > 0 {
		name := strings.TrimSpace(cleanLine[:idx])
		sigExpr := strings.TrimSpace(cleanLine[idx:])
		return interfaceMember{
			name:    name,
			sigExpr: sigExpr,
			line:    line,
		}
	}

	// Index signature: [key: string]: any
	if strings.HasPrefix(cleanLine, "[") {
		return interfaceMember{
			name:    cleanLine,
			sigExpr: cleanLine,
			line:    line,
		}
	}

	// Property: name: type
	if colonIdx := strings.Index(cleanLine, ":"); colonIdx > 0 {
		name := strings.TrimSpace(cleanLine[:colonIdx])
		sigExpr := strings.TrimSpace(cleanLine[colonIdx+1:])
		return interfaceMember{
			name:    name,
			sigExpr: sigExpr,
			line:    line,
		}
	}

	// Bare name (shouldn't happen often in TS interfaces, but handle it)
	return interfaceMember{
		name:    cleanLine,
		sigExpr: cleanLine,
		line:    line,
	}
}

func formatTSInterface(name string, exported bool, members []interfaceMember) []byte {
	var b strings.Builder
	if exported {
		b.WriteString("export ")
	}
	b.WriteString("interface ")
	b.WriteString(name)
	b.WriteString(" {\n")
	for _, m := range members {
		b.WriteString("  ")
		b.WriteString(m.line)
		b.WriteString(";\n")
	}
	b.WriteString("}")
	return []byte(b.String())
}

// --- Common member set-union logic ---

// mergeMemberSets performs set-union merge on interface members.
// Union of ours and theirs, minus one-sided deletions from base.
// Signature changes on the same member name = conflict.
func mergeMemberSets(baseMembers, oursMembers, theirsMembers []interfaceMember) ([]interfaceMember, bool) {
	baseMap := memberMap(baseMembers)
	oursMap := memberMap(oursMembers)
	theirsMap := memberMap(theirsMembers)

	// Check for signature conflicts: same member name, different signature.
	for name, oursMember := range oursMap {
		if theirsMember, ok := theirsMap[name]; ok {
			if oursMember.sigExpr != theirsMember.sigExpr {
				if baseMember, inBase := baseMap[name]; inBase {
					if oursMember.sigExpr == baseMember.sigExpr || theirsMember.sigExpr == baseMember.sigExpr {
						continue
					}
				}
				return nil, true
			}
		}
	}

	// Build merged set: start with union of ours + theirs.
	merged := make(map[string]interfaceMember, len(oursMap)+len(theirsMap))
	for name, m := range oursMap {
		merged[name] = m
	}
	for name, m := range theirsMap {
		if _, exists := merged[name]; !exists {
			merged[name] = m
		} else {
			// Both have it — prefer ours unless theirs changed from base.
			oursMember := oursMap[name]
			if baseMember, inBase := baseMap[name]; inBase {
				if oursMember.sigExpr == baseMember.sigExpr && m.sigExpr != baseMember.sigExpr {
					merged[name] = m
				}
			}
		}
	}

	// Remove members that were in base but removed by one side.
	for name := range baseMap {
		_, inOurs := oursMap[name]
		_, inTheirs := theirsMap[name]
		if !inOurs && inTheirs {
			delete(merged, name)
		} else if inOurs && !inTheirs {
			delete(merged, name)
		}
	}

	// Order: base members first, then ours additions, then theirs additions.
	result := orderMembers(merged, baseMembers, oursMembers, theirsMembers)

	return result, false
}

func memberMap(members []interfaceMember) map[string]interfaceMember {
	m := make(map[string]interfaceMember, len(members))
	for _, member := range members {
		m[member.name] = member
	}
	return m
}

func orderMembers(merged map[string]interfaceMember, base, ours, theirs []interfaceMember) []interfaceMember {
	seen := make(map[string]bool, len(merged))
	var result []interfaceMember

	for _, m := range base {
		if _, ok := merged[m.name]; ok && !seen[m.name] {
			result = append(result, merged[m.name])
			seen[m.name] = true
		}
	}
	for _, m := range ours {
		if _, ok := merged[m.name]; ok && !seen[m.name] {
			result = append(result, merged[m.name])
			seen[m.name] = true
		}
	}
	for _, m := range theirs {
		if _, ok := merged[m.name]; ok && !seen[m.name] {
			result = append(result, merged[m.name])
			seen[m.name] = true
		}
	}

	// Safety: any remaining.
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
