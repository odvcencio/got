package entity

import (
	"bytes"
	"testing"
)

func TestReconstructGoFile(t *testing.T) {
	// Extract then Reconstruct should reproduce the original source byte-for-byte.
	// Go file with package, import, 2 functions, whitespace between them.
	src := "package main\n\nimport \"fmt\"\n\nfunc Hello() {\n\tfmt.Println(\"hello\")\n}\n\nfunc World() {\n\tfmt.Println(\"world\")\n}\n"

	el, err := Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	got := Reconstruct(el)
	if !bytes.Equal(got, []byte(src)) {
		t.Errorf("Reconstruct did not reproduce original source\nwant: %q\ngot:  %q", src, string(got))
	}
}

func TestReconstructPythonFile(t *testing.T) {
	// Extract then Reconstruct should reproduce the original source byte-for-byte.
	// Python file with import, function, class.
	src := "import os\n\ndef hello():\n    pass\n"

	el, err := Extract("test.py", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	got := Reconstruct(el)
	if !bytes.Equal(got, []byte(src)) {
		t.Errorf("Reconstruct did not reproduce original source\nwant: %q\ngot:  %q", src, string(got))
	}
}

func TestReconstructModifiedBody(t *testing.T) {
	// Modified entity body should produce valid output with the modification present.
	src := "package main\n\nfunc A() {}\n\nfunc B() {}\n"

	el, err := Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Find a declaration entity and modify its body.
	modified := false
	replacement := []byte("func A() { /* modified */ }")
	for i := range el.Entities {
		if el.Entities[i].Kind == KindDeclaration && el.Entities[i].Name == "A" {
			el.Entities[i].Body = replacement
			modified = true
			break
		}
	}
	if !modified {
		t.Fatal("could not find declaration 'A' to modify")
	}

	got := Reconstruct(el)

	// The modification must be present in the output.
	if !bytes.Contains(got, replacement) {
		t.Errorf("Reconstruct output does not contain modified body\nwant substring: %q\ngot: %q", replacement, got)
	}

	// The output should NOT equal the original source (since we changed it).
	if bytes.Equal(got, []byte(src)) {
		t.Error("Reconstruct output should differ from original source after modification")
	}
}

func TestReconstructEmptyList(t *testing.T) {
	// Empty entity list should produce empty bytes.
	el := &EntityList{
		Entities: []Entity{},
	}

	got := Reconstruct(el)
	if len(got) != 0 {
		t.Errorf("expected empty bytes for empty entity list, got %d bytes: %q", len(got), got)
	}
}

func TestReconstructNilEntityList(t *testing.T) {
	// Nil EntityList should produce empty bytes (handle gracefully).
	got := Reconstruct(nil)
	if len(got) != 0 {
		t.Errorf("expected empty bytes for nil EntityList, got %d bytes: %q", len(got), got)
	}
}
