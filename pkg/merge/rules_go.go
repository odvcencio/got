package merge

import (
	"bytes"
	"fmt"
)

func init() {
	DefaultRegistry.Register(&GoInterfaceImplRule{})
	DefaultRegistry.Register(&GoConstVarBlockRule{})
	DefaultRegistry.Register(&GoInitFuncRule{})
}

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

// GoConstVarBlockRule detects when both sides add entries to a const or
// var block, and suggests set-union merge if there's a conflict.
type GoConstVarBlockRule struct{}

func (r *GoConstVarBlockRule) Language() string { return "go" }

func (r *GoConstVarBlockRule) Apply(ctx *MergeRuleContext) []Diagnostic {
	var diags []Diagnostic
	for _, m := range ctx.Matched {
		if m.Disposition != Conflict {
			continue
		}
		if m.Base == nil || m.Ours == nil || m.Theirs == nil {
			continue
		}
		body := m.Base.Body
		trimmed := bytes.TrimSpace(body)
		if !bytes.HasPrefix(trimmed, []byte("const ")) && !bytes.HasPrefix(trimmed, []byte("const(")) &&
			!bytes.HasPrefix(trimmed, []byte("var ")) && !bytes.HasPrefix(trimmed, []byte("var(")) &&
			!bytes.HasPrefix(trimmed, []byte("const\t")) && !bytes.HasPrefix(trimmed, []byte("var\t")) {
			continue
		}
		diags = append(diags, Diagnostic{
			Severity: DiagInfo,
			Entity:   m.Key,
			Message:  m.Base.Name + ": both sides modified const/var block — consider set-union merge",
			Rule:     "go-const-var-block",
		})
	}
	return diags
}

// GoInitFuncRule warns when init() is modified on both sides,
// since init() ordering and side effects are sensitive.
type GoInitFuncRule struct{}

func (r *GoInitFuncRule) Language() string { return "go" }

func (r *GoInitFuncRule) Apply(ctx *MergeRuleContext) []Diagnostic {
	var diags []Diagnostic
	for _, m := range ctx.Matched {
		if m.Disposition != Conflict {
			continue
		}
		if m.Base == nil {
			continue
		}
		if m.Base.Name != "init" {
			continue
		}
		if m.Base.DeclKind != "function_declaration" && m.Base.DeclKind != "function_definition" {
			continue
		}
		diags = append(diags, Diagnostic{
			Severity: DiagWarning,
			Entity:   m.Key,
			Message:  "init() modified on both sides — review carefully, execution order may matter",
			Rule:     "go-init-func",
		})
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
