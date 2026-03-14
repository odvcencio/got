package merge

import (
	"bytes"
	"fmt"
)

// GoInterfaceImplRule warns when a method is added to a Go interface,
// since all implementations will need to add the method.
type GoInterfaceImplRule struct{}

func (r *GoInterfaceImplRule) Language() string { return "go" }

func (r *GoInterfaceImplRule) Apply(ctx *MergeRuleContext) []Diagnostic {
	var diags []Diagnostic

	for _, m := range ctx.Matched {
		if m.Disposition != OursOnly && m.Disposition != TheirsOnly {
			continue
		}
		if m.Base == nil {
			continue
		}
		if !isInterfaceBody(m.Base.DeclKind, "go", m.Base.Body) {
			continue
		}

		modified := m.Ours
		side := "ours"
		if m.Disposition == TheirsOnly {
			modified = m.Theirs
			side = "theirs"
		}
		if modified == nil {
			continue
		}

		baseMethods := countInterfaceMethods(m.Base.Body)
		modifiedMethods := countInterfaceMethods(modified.Body)

		if modifiedMethods > baseMethods {
			added := modifiedMethods - baseMethods
			diags = append(diags, Diagnostic{
				Severity: DiagWarning,
				Entity:   m.Key,
				Message:  fmt.Sprintf("%s: %s added %d method(s) to interface — implementors may need updating", m.Base.Name, side, added),
				Rule:     "go-interface-impl",
			})
		}
	}

	return diags
}

// countInterfaceMethods counts lines that look like method signatures
// inside a Go interface body. This is a heuristic — not a full parse.
func countInterfaceMethods(body []byte) int {
	count := 0
	lines := bytes.Split(body, []byte("\n"))
	inBody := false
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.Contains(trimmed, []byte("interface {")) || bytes.Contains(trimmed, []byte("interface{")) {
			inBody = true
			continue
		}
		if inBody && len(trimmed) > 0 && trimmed[0] != '}' && !bytes.HasPrefix(trimmed, []byte("//")) {
			count++
		}
	}
	return count
}
