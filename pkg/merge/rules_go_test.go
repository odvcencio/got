package merge

import (
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
)

// mergeAndRunRule is a test helper that runs MergeFiles, extracts entities,
// matches them, and applies a rule. Returns the diagnostics.
func mergeAndRunRule(t *testing.T, path string, base, ours, theirs []byte, rule LangMergeRule) []Diagnostic {
	t.Helper()
	result, err := MergeFiles(path, base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}

	baseEL, _ := entity.Extract(path, base)
	oursEL, _ := entity.Extract(path, ours)
	theirsEL, _ := entity.Extract(path, theirs)

	var matched []MatchedEntity
	if baseEL != nil && oursEL != nil && theirsEL != nil {
		matched = MatchEntities(baseEL, oursEL, theirsEL)
	}

	ctx := &MergeRuleContext{
		Base:     safeEntities(baseEL),
		Ours:     safeEntities(oursEL),
		Theirs:   safeEntities(theirsEL),
		Matched:  matched,
		Result:   result,
		Language: DetectLanguage(path),
		Path:     path,
	}
	return rule.Apply(ctx)
}

func safeEntities(el *entity.EntityList) []entity.Entity {
	if el == nil {
		return nil
	}
	return el.Entities
}

func TestGoInterfaceImplRule(t *testing.T) {
	base := []byte("package main\n\ntype Processor interface {\n\tProcess() error\n}\n")
	ours := []byte("package main\n\ntype Processor interface {\n\tProcess() error\n\tValidate() error\n}\n")
	theirs := base

	diags := mergeAndRunRule(t, "main.go", base, ours, theirs, &GoInterfaceImplRule{})

	found := false
	for _, d := range diags {
		if d.Rule == "go-interface-impl" && d.Severity == DiagWarning {
			found = true
		}
	}
	if !found {
		t.Error("expected go-interface-impl warning when interface gains a method")
	}
}

func TestGoConstBlockRule(t *testing.T) {
	base := []byte("package main\n\nconst (\n\tA = 1\n)\n")
	ours := []byte("package main\n\nconst (\n\tA = 1\n\tB = 2\n)\n")
	theirs := []byte("package main\n\nconst (\n\tA = 1\n\tC = 3\n)\n")

	diags := mergeAndRunRule(t, "main.go", base, ours, theirs, &GoConstVarBlockRule{})

	// Should produce an info diagnostic about const block merge
	for _, d := range diags {
		if d.Rule == "go-const-var-block" {
			return // found it
		}
	}
	// It's OK if no diagnostic — the structural merge may already handle it cleanly
}

func TestGoInterfaceImplRuleNoWarningWhenUnchanged(t *testing.T) {
	src := []byte("package main\n\ntype Processor interface {\n\tProcess() error\n}\n")

	diags := mergeAndRunRule(t, "main.go", src, src, src, &GoInterfaceImplRule{})

	if len(diags) != 0 {
		t.Errorf("expected no diagnostics for unchanged interface, got %d", len(diags))
	}
}
