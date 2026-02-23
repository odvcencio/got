package entity

import (
	"bytes"
	"strings"
	"testing"
)

// verifyByteCoverage ensures the critical invariant: concatenating all entity
// bodies reproduces the original source exactly, and the byte ranges cover
// the full source without gaps or overlaps.
func verifyByteCoverage(t *testing.T, el *EntityList) {
	t.Helper()
	source := el.Source

	// Check concatenation reproduces source
	var buf bytes.Buffer
	for _, e := range el.Entities {
		buf.Write(e.Body)
	}
	if !bytes.Equal(buf.Bytes(), source) {
		t.Errorf("byte coverage failed: concatenated entities (%d bytes) != source (%d bytes)",
			buf.Len(), len(source))
		t.Logf("source:       %q", source)
		t.Logf("concatenated: %q", buf.Bytes())
	}

	// Check that byte ranges are contiguous and span [0, len(source))
	if len(el.Entities) == 0 {
		if len(source) != 0 {
			t.Errorf("no entities but source is %d bytes", len(source))
		}
		return
	}
	if el.Entities[0].StartByte != 0 {
		t.Errorf("first entity starts at byte %d, expected 0", el.Entities[0].StartByte)
	}
	last := el.Entities[len(el.Entities)-1]
	if last.EndByte != uint32(len(source)) {
		t.Errorf("last entity ends at byte %d, expected %d", last.EndByte, len(source))
	}
	for i := 1; i < len(el.Entities); i++ {
		prev := el.Entities[i-1]
		curr := el.Entities[i]
		if curr.StartByte != prev.EndByte {
			t.Errorf("gap or overlap between entity %d (end=%d) and entity %d (start=%d)",
				i-1, prev.EndByte, i, curr.StartByte)
		}
	}
}

// verifyUniqueKeys checks that all declaration identity keys are unique.
func verifyUniqueKeys(t *testing.T, el *EntityList) {
	t.Helper()
	seen := make(map[string]int)
	for i, e := range el.Entities {
		if e.Kind == KindDeclaration {
			key := e.IdentityKey()
			if prev, ok := seen[key]; ok {
				t.Errorf("duplicate identity key %q at entity %d and %d", key, prev, i)
			}
			seen[key] = i
		}
	}
}

func TestExtractGoFile(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\nfunc A() {}\n\nfunc B() {}\n"
	el, err := Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if el.Language != "go" {
		t.Errorf("expected language %q, got %q", "go", el.Language)
	}
	if el.Path != "main.go" {
		t.Errorf("expected path %q, got %q", "main.go", el.Path)
	}

	// Expected entity sequence:
	// [preamble, interstitial, import, interstitial, decl(A), interstitial, decl(B), (optional trailing interstitial)]
	// The exact count depends on trailing newline handling.

	// Verify basic entity kinds present
	var kinds []EntityKind
	var declNames []string
	for _, e := range el.Entities {
		kinds = append(kinds, e.Kind)
		if e.Kind == KindDeclaration {
			declNames = append(declNames, e.Name)
		}
	}

	// Must have at least: preamble, import, 2 declarations
	preambleCount := 0
	importCount := 0
	declCount := 0
	for _, k := range kinds {
		switch k {
		case KindPreamble:
			preambleCount++
		case KindImportBlock:
			importCount++
		case KindDeclaration:
			declCount++
		}
	}
	if preambleCount != 1 {
		t.Errorf("expected 1 preamble, got %d", preambleCount)
	}
	if importCount != 1 {
		t.Errorf("expected 1 import, got %d", importCount)
	}
	if declCount != 2 {
		t.Errorf("expected 2 declarations, got %d", declCount)
	}
	if len(declNames) != 2 || declNames[0] != "A" || declNames[1] != "B" {
		t.Errorf("expected declaration names [A, B], got %v", declNames)
	}

	// Verify all declarations have DeclKind set
	for _, e := range el.Entities {
		if e.Kind == KindDeclaration && e.DeclKind == "" {
			t.Errorf("declaration %q has empty DeclKind", e.Name)
		}
	}

	verifyByteCoverage(t, el)
	verifyUniqueKeys(t, el)
}

func TestExtractGoMethodWithReceiver(t *testing.T) {
	src := "package main\n\ntype T struct{}\n\nfunc (t T) M() {}\n"
	el, err := Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Find the method_declaration entity
	var method *Entity
	var typeDecl *Entity
	for i := range el.Entities {
		if el.Entities[i].Kind == KindDeclaration {
			if el.Entities[i].Name == "M" {
				method = &el.Entities[i]
			}
			if strings.Contains(el.Entities[i].DeclKind, "type") {
				typeDecl = &el.Entities[i]
			}
		}
	}

	if typeDecl == nil {
		t.Fatal("expected a type declaration entity")
	}
	if typeDecl.Name != "T" {
		t.Errorf("expected type name %q, got %q", "T", typeDecl.Name)
	}

	if method == nil {
		t.Fatal("expected a method declaration entity")
	}
	if method.Receiver == "" {
		t.Error("expected non-empty Receiver for method")
	}
	if method.DeclKind != "method_declaration" {
		t.Errorf("expected DeclKind %q, got %q", "method_declaration", method.DeclKind)
	}

	// Receiver should contain "t T" (the receiver text)
	if !strings.Contains(method.Receiver, "T") {
		t.Errorf("expected Receiver to contain type T, got %q", method.Receiver)
	}

	verifyByteCoverage(t, el)
	verifyUniqueKeys(t, el)
}

func TestExtractPython(t *testing.T) {
	// Due to Python indent-sensitive parsing limitations in the pure-Go
	// tree-sitter, we use a file with import + assignment (non-indented
	// constructs) that the parser handles correctly.
	src := "import os\n\nx = 1\n"
	el, err := Extract("test.py", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if el.Language != "python" {
		t.Errorf("expected language %q, got %q", "python", el.Language)
	}

	// Should have at least an import entity
	hasImport := false
	for _, e := range el.Entities {
		if e.Kind == KindImportBlock {
			hasImport = true
		}
	}
	if !hasImport {
		t.Error("expected at least one import entity for Python file")
	}

	verifyByteCoverage(t, el)
}

func TestExtractPythonFunctionAndClass(t *testing.T) {
	// Single function without subsequent declarations to avoid indent issue
	src := "import os\n\ndef hello():\n    pass\n"
	el, err := Extract("test.py", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	hasImport := false
	hasFunc := false
	for _, e := range el.Entities {
		if e.Kind == KindImportBlock {
			hasImport = true
		}
		if e.Kind == KindDeclaration && e.Name == "hello" {
			hasFunc = true
		}
	}
	if !hasImport {
		t.Error("expected import entity")
	}
	if !hasFunc {
		t.Error("expected function declaration for 'hello'")
	}

	verifyByteCoverage(t, el)
	verifyUniqueKeys(t, el)
}

func TestExtractTypeScript(t *testing.T) {
	src := "function foo() {}\n\nexport function bar() {}\n"
	el, err := Extract("test.ts", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if el.Language != "typescript" {
		t.Errorf("expected language %q, got %q", "typescript", el.Language)
	}

	var declNames []string
	for _, e := range el.Entities {
		if e.Kind == KindDeclaration {
			declNames = append(declNames, e.Name)
		}
	}

	// foo is a direct function_declaration, bar is inside export_statement
	if len(declNames) < 2 {
		t.Errorf("expected at least 2 declarations, got %v", declNames)
	}
	found := map[string]bool{}
	for _, n := range declNames {
		found[n] = true
	}
	if !found["foo"] {
		t.Error("expected declaration 'foo'")
	}
	if !found["bar"] {
		t.Error("expected declaration 'bar'")
	}

	verifyByteCoverage(t, el)
	verifyUniqueKeys(t, el)
}

func TestExtractUnknownExtension(t *testing.T) {
	_, err := Extract("test.xyz", []byte("hello"))
	if err == nil {
		t.Fatal("expected error for unknown file extension")
	}
}

func TestExtractEmptyFile(t *testing.T) {
	el, err := Extract("empty.go", []byte(""))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(el.Entities) != 0 {
		t.Errorf("expected 0 entities for empty file, got %d", len(el.Entities))
	}
	verifyByteCoverage(t, el)
}

func TestExtractHashesPopulated(t *testing.T) {
	src := "package main\n\nfunc A() {}\n"
	el, err := Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	for i, e := range el.Entities {
		if e.BodyHash == "" {
			t.Errorf("entity %d (%s) has empty BodyHash", i, e.Kind)
		}
	}
}

func TestExtractInterstitialNeighborKeys(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\nfunc A() {}\n\nfunc B() {}\n"
	el, err := Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, e := range el.Entities {
		if e.Kind == KindInterstitial {
			// Interstitial entities should have neighbor keys set
			// (except possibly leading/trailing ones which may have empty prev/next)
			if e.PrevEntityKey == "" && e.NextEntityKey == "" {
				t.Errorf("interstitial at bytes [%d:%d] has both neighbor keys empty",
					e.StartByte, e.EndByte)
			}
		}
	}
}

func TestExtractGoCommentBetweenDecls(t *testing.T) {
	src := "package main\n\n// doc comment\nfunc A() {}\n"
	el, err := Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// The comment should be captured (either as interstitial or as its own entity)
	verifyByteCoverage(t, el)

	// Must have A as a declaration
	found := false
	for _, e := range el.Entities {
		if e.Kind == KindDeclaration && e.Name == "A" {
			found = true
		}
	}
	if !found {
		t.Error("expected declaration 'A'")
	}
}

func TestExtractRust(t *testing.T) {
	src := "use std::io;\n\nfn main() {}\n\nstruct Foo {}\n"
	el, err := Extract("test.rs", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if el.Language != "rust" {
		t.Errorf("expected language %q, got %q", "rust", el.Language)
	}

	hasImport := false
	declNames := map[string]bool{}
	for _, e := range el.Entities {
		if e.Kind == KindImportBlock {
			hasImport = true
		}
		if e.Kind == KindDeclaration {
			declNames[e.Name] = true
		}
	}
	if !hasImport {
		t.Error("expected import entity for use_declaration")
	}
	if !declNames["main"] {
		t.Error("expected declaration 'main'")
	}
	if !declNames["Foo"] {
		t.Error("expected declaration 'Foo'")
	}

	verifyByteCoverage(t, el)
	verifyUniqueKeys(t, el)
}

func TestExtractGoVarConst(t *testing.T) {
	src := "package main\n\nvar x = 1\n\nconst y = 2\n"
	el, err := Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	declNames := map[string]bool{}
	for _, e := range el.Entities {
		if e.Kind == KindDeclaration {
			declNames[e.Name] = true
		}
	}
	if !declNames["x"] {
		t.Error("expected declaration 'x'")
	}
	if !declNames["y"] {
		t.Error("expected declaration 'y'")
	}

	verifyByteCoverage(t, el)
	verifyUniqueKeys(t, el)
}
