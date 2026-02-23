package merge

import (
	"testing"

	"github.com/odvcencio/got/pkg/entity"
)

// makeEntity creates an Entity with the given kind, name, and body, then computes its hash.
func makeEntity(kind entity.EntityKind, name string, body string) entity.Entity {
	e := entity.Entity{
		Kind: kind,
		Name: name,
		Body: []byte(body),
	}
	if kind == entity.KindDeclaration {
		e.DeclKind = "function_definition"
	}
	e.ComputeHash()
	return e
}

// makeEntityList creates an EntityList from a slice of entities.
func makeEntityList(entities []entity.Entity) *entity.EntityList {
	return &entity.EntityList{
		Language: "go",
		Path:     "test.go",
		Entities: entities,
	}
}

func TestMatchAllUnchanged(t *testing.T) {
	preamble := makeEntity(entity.KindPreamble, "", "package main")
	fn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")

	base := makeEntityList([]entity.Entity{preamble, fn})
	ours := makeEntityList([]entity.Entity{preamble, fn})
	theirs := makeEntityList([]entity.Entity{preamble, fn})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	for _, m := range matches {
		if m.Disposition != Unchanged {
			t.Errorf("key %q: expected Unchanged, got %v", m.Key, m.Disposition)
		}
	}
}

func TestMatchOursModified(t *testing.T) {
	baseFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	oursFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() { return 1 }")
	theirsFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")

	base := makeEntityList([]entity.Entity{baseFn})
	ours := makeEntityList([]entity.Entity{oursFn})
	theirs := makeEntityList([]entity.Entity{theirsFn})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Disposition != OursOnly {
		t.Errorf("expected OursOnly, got %v", matches[0].Disposition)
	}
	if matches[0].Base == nil || matches[0].Ours == nil || matches[0].Theirs == nil {
		t.Error("all three pointers should be non-nil")
	}
}

func TestMatchTheirsModified(t *testing.T) {
	baseFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	oursFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	theirsFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() { return 2 }")

	base := makeEntityList([]entity.Entity{baseFn})
	ours := makeEntityList([]entity.Entity{oursFn})
	theirs := makeEntityList([]entity.Entity{theirsFn})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Disposition != TheirsOnly {
		t.Errorf("expected TheirsOnly, got %v", matches[0].Disposition)
	}
}

func TestMatchConflict(t *testing.T) {
	baseFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	oursFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() { return 1 }")
	theirsFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() { return 2 }")

	base := makeEntityList([]entity.Entity{baseFn})
	ours := makeEntityList([]entity.Entity{oursFn})
	theirs := makeEntityList([]entity.Entity{theirsFn})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Disposition != Conflict {
		t.Errorf("expected Conflict, got %v", matches[0].Disposition)
	}
}

func TestMatchBothSame(t *testing.T) {
	baseFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	modifiedFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() { return 42 }")

	base := makeEntityList([]entity.Entity{baseFn})
	ours := makeEntityList([]entity.Entity{modifiedFn})
	theirs := makeEntityList([]entity.Entity{modifiedFn})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Disposition != BothSame {
		t.Errorf("expected BothSame, got %v", matches[0].Disposition)
	}
}

func TestMatchAddedOurs(t *testing.T) {
	baseFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	newFn := makeEntity(entity.KindDeclaration, "Bar", "func Bar() {}")

	base := makeEntityList([]entity.Entity{baseFn})
	ours := makeEntityList([]entity.Entity{baseFn, newFn})
	theirs := makeEntityList([]entity.Entity{baseFn})

	matches := MatchEntities(base, ours, theirs)

	// Should have two matches: Foo (Unchanged) and Bar (AddedOurs)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	var barMatch *MatchedEntity
	for i := range matches {
		if matches[i].Key == newFn.IdentityKey() {
			barMatch = &matches[i]
			break
		}
	}
	if barMatch == nil {
		t.Fatal("did not find match for Bar")
	}
	if barMatch.Disposition != AddedOurs {
		t.Errorf("expected AddedOurs, got %v", barMatch.Disposition)
	}
	if barMatch.Base != nil {
		t.Error("Base should be nil for AddedOurs")
	}
	if barMatch.Ours == nil {
		t.Error("Ours should be non-nil for AddedOurs")
	}
	if barMatch.Theirs != nil {
		t.Error("Theirs should be nil for AddedOurs")
	}
}

func TestMatchDeletedTheirs(t *testing.T) {
	baseFoo := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	baseBar := makeEntity(entity.KindDeclaration, "Bar", "func Bar() {}")

	base := makeEntityList([]entity.Entity{baseFoo, baseBar})
	ours := makeEntityList([]entity.Entity{baseFoo, baseBar})
	theirs := makeEntityList([]entity.Entity{baseFoo}) // Bar deleted by theirs

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	var barMatch *MatchedEntity
	for i := range matches {
		if matches[i].Key == baseBar.IdentityKey() {
			barMatch = &matches[i]
			break
		}
	}
	if barMatch == nil {
		t.Fatal("did not find match for Bar")
	}
	if barMatch.Disposition != DeletedTheirs {
		t.Errorf("expected DeletedTheirs, got %v", barMatch.Disposition)
	}
	if barMatch.Base == nil {
		t.Error("Base should be non-nil for DeletedTheirs")
	}
	if barMatch.Ours == nil {
		t.Error("Ours should be non-nil for DeletedTheirs")
	}
	if barMatch.Theirs != nil {
		t.Error("Theirs should be nil for DeletedTheirs")
	}
}

func TestMatchDeleteVsModify(t *testing.T) {
	baseFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	oursFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() { return 99 }") // modified

	base := makeEntityList([]entity.Entity{baseFn})
	ours := makeEntityList([]entity.Entity{oursFn})
	theirs := makeEntityList([]entity.Entity{}) // deleted by theirs

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Disposition != DeleteVsModify {
		t.Errorf("expected DeleteVsModify, got %v", matches[0].Disposition)
	}
	if matches[0].Theirs != nil {
		t.Error("Theirs should be nil for DeleteVsModify (theirs deleted)")
	}
}

func TestMatchOrderPreservation(t *testing.T) {
	// Base has A, B; Ours adds C after B; Theirs adds D after B
	a := makeEntity(entity.KindDeclaration, "A", "func A() {}")
	b := makeEntity(entity.KindDeclaration, "B", "func B() {}")
	c := makeEntity(entity.KindDeclaration, "C", "func C() {}")
	d := makeEntity(entity.KindDeclaration, "D", "func D() {}")

	base := makeEntityList([]entity.Entity{a, b})
	ours := makeEntityList([]entity.Entity{a, b, c})
	theirs := makeEntityList([]entity.Entity{a, b, d})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 4 {
		t.Fatalf("expected 4 matches, got %d", len(matches))
	}

	// Verify order: base entities first (A, B), then new in ours (C), then new in theirs (D)
	expectedNames := []string{
		a.IdentityKey(),
		b.IdentityKey(),
		c.IdentityKey(),
		d.IdentityKey(),
	}
	for i, m := range matches {
		if m.Key != expectedNames[i] {
			t.Errorf("position %d: expected key %q, got %q", i, expectedNames[i], m.Key)
		}
	}
}

func TestMatchDeletedOurs(t *testing.T) {
	baseFoo := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	baseBar := makeEntity(entity.KindDeclaration, "Bar", "func Bar() {}")

	base := makeEntityList([]entity.Entity{baseFoo, baseBar})
	ours := makeEntityList([]entity.Entity{baseFoo}) // Bar deleted by ours
	theirs := makeEntityList([]entity.Entity{baseFoo, baseBar})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	var barMatch *MatchedEntity
	for i := range matches {
		if matches[i].Key == baseBar.IdentityKey() {
			barMatch = &matches[i]
			break
		}
	}
	if barMatch == nil {
		t.Fatal("did not find match for Bar")
	}
	if barMatch.Disposition != DeletedOurs {
		t.Errorf("expected DeletedOurs, got %v", barMatch.Disposition)
	}
	if barMatch.Base == nil {
		t.Error("Base should be non-nil for DeletedOurs")
	}
	if barMatch.Ours != nil {
		t.Error("Ours should be nil for DeletedOurs")
	}
	if barMatch.Theirs == nil {
		t.Error("Theirs should be non-nil for DeletedOurs")
	}
}

func TestMatchAddedTheirs(t *testing.T) {
	baseFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	newFn := makeEntity(entity.KindDeclaration, "Baz", "func Baz() {}")

	base := makeEntityList([]entity.Entity{baseFn})
	ours := makeEntityList([]entity.Entity{baseFn})
	theirs := makeEntityList([]entity.Entity{baseFn, newFn})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	var bazMatch *MatchedEntity
	for i := range matches {
		if matches[i].Key == newFn.IdentityKey() {
			bazMatch = &matches[i]
			break
		}
	}
	if bazMatch == nil {
		t.Fatal("did not find match for Baz")
	}
	if bazMatch.Disposition != AddedTheirs {
		t.Errorf("expected AddedTheirs, got %v", bazMatch.Disposition)
	}
	if bazMatch.Base != nil {
		t.Error("Base should be nil for AddedTheirs")
	}
	if bazMatch.Ours != nil {
		t.Error("Ours should be nil for AddedTheirs")
	}
	if bazMatch.Theirs == nil {
		t.Error("Theirs should be non-nil for AddedTheirs")
	}
}

func TestMatchDeleteVsModifyOursDeleted(t *testing.T) {
	// Ours deletes, theirs modifies — also DeleteVsModify
	baseFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() {}")
	theirsFn := makeEntity(entity.KindDeclaration, "Foo", "func Foo() { return 99 }")

	base := makeEntityList([]entity.Entity{baseFn})
	ours := makeEntityList([]entity.Entity{}) // deleted by ours
	theirs := makeEntityList([]entity.Entity{theirsFn})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Disposition != DeleteVsModify {
		t.Errorf("expected DeleteVsModify, got %v", matches[0].Disposition)
	}
	if matches[0].Ours != nil {
		t.Error("Ours should be nil for DeleteVsModify (ours deleted)")
	}
}

func TestMatchAddedBothSides(t *testing.T) {
	// Both sides add the same new entity — not in base
	newFn := makeEntity(entity.KindDeclaration, "New", "func New() {}")

	base := makeEntityList([]entity.Entity{})
	ours := makeEntityList([]entity.Entity{newFn})
	theirs := makeEntityList([]entity.Entity{newFn})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	// Both added identically, not in base → BothSame
	if matches[0].Disposition != BothSame {
		t.Errorf("expected BothSame for identical additions, got %v", matches[0].Disposition)
	}
}

func TestMatchAddedBothSidesDifferently(t *testing.T) {
	// Both sides add the same key but different bodies
	oursFn := makeEntity(entity.KindDeclaration, "New", "func New() { return 1 }")
	theirsFn := makeEntity(entity.KindDeclaration, "New", "func New() { return 2 }")

	base := makeEntityList([]entity.Entity{})
	ours := makeEntityList([]entity.Entity{oursFn})
	theirs := makeEntityList([]entity.Entity{theirsFn})

	matches := MatchEntities(base, ours, theirs)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	// Both added differently → Conflict
	if matches[0].Disposition != Conflict {
		t.Errorf("expected Conflict for different additions, got %v", matches[0].Disposition)
	}
}
