package merge

import (
	"bytes"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
)

func TestReconstructAllClean(t *testing.T) {
	entities := []ResolvedEntity{
		{
			Entity: entity.Entity{
				Kind: entity.KindPreamble,
				Body: []byte("package main\n"),
			},
		},
		{
			Entity: entity.Entity{
				Kind: entity.KindDeclaration,
				Name: "Foo",
				Body: []byte("func Foo() {}\n"),
			},
		},
		{
			Entity: entity.Entity{
				Kind: entity.KindDeclaration,
				Name: "Bar",
				Body: []byte("func Bar() {}\n"),
			},
		},
	}

	got := Reconstruct(entities)
	want := []byte("package main\nfunc Foo() {}\nfunc Bar() {}\n")

	if !bytes.Equal(got, want) {
		t.Errorf("Reconstruct all clean:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestReconstructOneConflict(t *testing.T) {
	entities := []ResolvedEntity{
		{
			Entity: entity.Entity{
				Kind: entity.KindDeclaration,
				Name: "Foo",
			},
			Conflict:   true,
			OursBody:   []byte("func Foo() { return 1 }"),
			TheirsBody: []byte("func Foo() { return 2 }"),
		},
	}

	got := string(Reconstruct(entities))

	if !strings.Contains(got, "<<<<<<< ours") {
		t.Error("expected conflict marker <<<<<<< ours")
	}
	if !strings.Contains(got, "=======") {
		t.Error("expected conflict separator =======")
	}
	if !strings.Contains(got, ">>>>>>> theirs") {
		t.Error("expected conflict marker >>>>>>> theirs")
	}
	if !strings.Contains(got, "func Foo() { return 1 }") {
		t.Error("expected ours body in conflict output")
	}
	if !strings.Contains(got, "func Foo() { return 2 }") {
		t.Error("expected theirs body in conflict output")
	}

	// Verify the exact conflict block structure
	want := "<<<<<<< ours\nfunc Foo() { return 1 }\n=======\nfunc Foo() { return 2 }\n>>>>>>> theirs\n"
	if got != want {
		t.Errorf("conflict block structure:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestReconstructMixedCleanAndConflict(t *testing.T) {
	entities := []ResolvedEntity{
		{
			Entity: entity.Entity{
				Kind: entity.KindPreamble,
				Body: []byte("package main\n"),
			},
		},
		{
			Entity: entity.Entity{
				Kind: entity.KindDeclaration,
				Name: "Foo",
			},
			Conflict:   true,
			OursBody:   []byte("func Foo() { return 1 }"),
			TheirsBody: []byte("func Foo() { return 2 }"),
		},
		{
			Entity: entity.Entity{
				Kind: entity.KindDeclaration,
				Name: "Bar",
				Body: []byte("func Bar() {}\n"),
			},
		},
	}

	got := string(Reconstruct(entities))

	// Check that the preamble comes first
	preambleIdx := strings.Index(got, "package main\n")
	conflictIdx := strings.Index(got, "<<<<<<< ours")
	barIdx := strings.Index(got, "func Bar() {}\n")

	if preambleIdx == -1 {
		t.Fatal("missing preamble in output")
	}
	if conflictIdx == -1 {
		t.Fatal("missing conflict block in output")
	}
	if barIdx == -1 {
		t.Fatal("missing Bar function in output")
	}

	if preambleIdx >= conflictIdx {
		t.Error("preamble should come before conflict block")
	}
	if conflictIdx >= barIdx {
		t.Error("conflict block should come before Bar function")
	}

	// Verify the full output
	want := "package main\n<<<<<<< ours\nfunc Foo() { return 1 }\n=======\nfunc Foo() { return 2 }\n>>>>>>> theirs\nfunc Bar() {}\n"
	if got != want {
		t.Errorf("mixed output:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestReconstructEmptyEntities(t *testing.T) {
	got := Reconstruct(nil)
	if len(got) != 0 {
		t.Errorf("expected empty output for nil entities, got %q", got)
	}

	got = Reconstruct([]ResolvedEntity{})
	if len(got) != 0 {
		t.Errorf("expected empty output for empty entities, got %q", got)
	}
}
