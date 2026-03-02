package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// Test 1: Parse multi-line file, verify rules.
func TestAttributes_ParseFile(t *testing.T) {
	content := "*.bin filter=lfs diff=lfs merge=lfs\n*.proto merge=union\n# comment line\n\ndocs/** diff=text\n"
	attrs := ParseAttributes(content)

	if len(attrs.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(attrs.Rules))
	}

	// Rule 1: *.bin
	r := attrs.Rules[0]
	if r.Pattern != "*.bin" {
		t.Errorf("rule 0: expected pattern *.bin, got %s", r.Pattern)
	}
	if r.Attrs["filter"] != "lfs" {
		t.Errorf("rule 0: expected filter=lfs, got %s", r.Attrs["filter"])
	}
	if r.Attrs["diff"] != "lfs" {
		t.Errorf("rule 0: expected diff=lfs, got %s", r.Attrs["diff"])
	}
	if r.Attrs["merge"] != "lfs" {
		t.Errorf("rule 0: expected merge=lfs, got %s", r.Attrs["merge"])
	}

	// Rule 2: *.proto
	r = attrs.Rules[1]
	if r.Pattern != "*.proto" {
		t.Errorf("rule 1: expected pattern *.proto, got %s", r.Pattern)
	}
	if r.Attrs["merge"] != "union" {
		t.Errorf("rule 1: expected merge=union, got %s", r.Attrs["merge"])
	}

	// Rule 3: docs/**
	r = attrs.Rules[2]
	if r.Pattern != "docs/**" {
		t.Errorf("rule 2: expected pattern docs/**, got %s", r.Pattern)
	}
	if r.Attrs["diff"] != "text" {
		t.Errorf("rule 2: expected diff=text, got %s", r.Attrs["diff"])
	}
}

// Test 2: *.bin matches foo.bin and dir/foo.bin.
func TestAttributes_MatchPattern(t *testing.T) {
	attrs := ParseAttributes("*.bin filter=lfs\n")

	m := attrs.Match("foo.bin")
	if m["filter"] != "lfs" {
		t.Errorf("expected foo.bin to match *.bin, got filter=%s", m["filter"])
	}

	m = attrs.Match("dir/foo.bin")
	if m["filter"] != "lfs" {
		t.Errorf("expected dir/foo.bin to match *.bin, got filter=%s", m["filter"])
	}

	m = attrs.Match("foo.txt")
	if _, ok := m["filter"]; ok {
		t.Errorf("expected foo.txt to NOT match *.bin")
	}
}

// Test 3: Later rule overrides earlier for same attr.
func TestAttributes_PriorityOrder(t *testing.T) {
	attrs := ParseAttributes("*.txt merge=text\nspecial.txt merge=union\n")

	// special.txt should pick up merge=union from the later rule.
	m := attrs.Match("special.txt")
	if m["merge"] != "union" {
		t.Errorf("expected merge=union for special.txt, got %s", m["merge"])
	}

	// other.txt should still have merge=text.
	m = attrs.Match("other.txt")
	if m["merge"] != "text" {
		t.Errorf("expected merge=text for other.txt, got %s", m["merge"])
	}
}

// Test 4: No attributes file = empty match.
func TestAttributes_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	r := &Repo{RootDir: dir}

	attrs, err := r.ReadAttributes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(attrs.Rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(attrs.Rules))
	}

	m := attrs.Match("anything.bin")
	if len(m) != 0 {
		t.Errorf("expected empty match result, got %v", m)
	}
}

// Test 5: binary sets diff=false and merge=false.
func TestAttributes_BinaryShorthand(t *testing.T) {
	attrs := ParseAttributes("*.jpg binary\n")

	m := attrs.Match("photo.jpg")
	if m["diff"] != "false" {
		t.Errorf("expected diff=false for binary, got %s", m["diff"])
	}
	if m["merge"] != "false" {
		t.Errorf("expected merge=false for binary, got %s", m["merge"])
	}
}

// Test 6: docs/** matches docs/foo.md and docs/sub/bar.md.
func TestAttributes_DoubleStarPattern(t *testing.T) {
	attrs := ParseAttributes("docs/** diff=text\n")

	m := attrs.Match("docs/foo.md")
	if m["diff"] != "text" {
		t.Errorf("expected docs/foo.md to match docs/**, got diff=%s", m["diff"])
	}

	m = attrs.Match("docs/sub/bar.md")
	if m["diff"] != "text" {
		t.Errorf("expected docs/sub/bar.md to match docs/**, got diff=%s", m["diff"])
	}

	m = attrs.Match("src/docs/foo.md")
	if _, ok := m["diff"]; ok {
		t.Errorf("expected src/docs/foo.md to NOT match docs/**")
	}
}

// Test 7: ReadAttributes loads from .graftattributes file on disk.
func TestAttributes_ReadFromFile(t *testing.T) {
	dir := t.TempDir()
	content := "*.bin filter=lfs\n*.txt merge=text\n"
	if err := os.WriteFile(filepath.Join(dir, ".graftattributes"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .graftattributes: %v", err)
	}

	r := &Repo{RootDir: dir}
	attrs, err := r.ReadAttributes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(attrs.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(attrs.Rules))
	}

	m := attrs.Match("image.bin")
	if m["filter"] != "lfs" {
		t.Errorf("expected filter=lfs for image.bin, got %s", m["filter"])
	}
}

// Test 8: Negated attributes with - prefix.
func TestAttributes_NegatedAttribute(t *testing.T) {
	attrs := ParseAttributes("*.dat -diff\n")

	m := attrs.Match("data.dat")
	if m["diff"] != "false" {
		t.Errorf("expected diff=false for negated attribute, got %s", m["diff"])
	}
}
