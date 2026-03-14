package merge

import (
	"testing"
)

type testRule struct {
	lang  string
	diags []Diagnostic
}

func (r *testRule) Language() string { return r.lang }
func (r *testRule) Apply(ctx *MergeRuleContext) []Diagnostic {
	return r.diags
}

func TestRuleRegistryDispatch(t *testing.T) {
	reg := NewRuleRegistry()
	rule := &testRule{
		lang: "go",
		diags: []Diagnostic{
			{Severity: DiagWarning, Entity: "decl:function_definition::Foo", Message: "test warning", Rule: "test-rule"},
		},
	}
	reg.Register(rule)

	rules := reg.RulesFor("go")
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule for go, got %d", len(rules))
	}

	rules = reg.RulesFor("python")
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules for python, got %d", len(rules))
	}
}

type callTracker struct {
	lang   string
	called *bool
}

func (c *callTracker) Language() string { return c.lang }
func (c *callTracker) Apply(ctx *MergeRuleContext) []Diagnostic {
	*c.called = true
	return nil
}

func TestMergeFilesRunsRules(t *testing.T) {
	base := []byte("package main\n\nfunc A() { return }\n")
	ours := []byte("package main\n\nfunc A() { return 1 }\n")
	theirs := []byte("package main\n\nfunc A() { return 2 }\n")

	called := false
	DefaultRegistry.Register(&callTracker{lang: "go", called: &called})
	defer func() { DefaultRegistry = NewRuleRegistry() }()

	result, err := MergeFiles("test.go", base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("expected rule to be called during merge")
	}
	_ = result
}

func TestDiagnosticSeverityString(t *testing.T) {
	tests := []struct {
		sev  DiagSeverity
		want string
	}{
		{DiagInfo, "info"},
		{DiagWarning, "warning"},
		{DiagError, "error"},
	}
	for _, tt := range tests {
		if got := tt.sev.String(); got != tt.want {
			t.Errorf("DiagSeverity(%d).String() = %q, want %q", tt.sev, got, tt.want)
		}
	}
}
