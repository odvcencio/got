package merge

import (
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
)

// makeRenameEntity creates a declaration entity with explicit name, declKind, and body.
func makeRenameEntity(name, declKind, body string) entity.Entity {
	e := entity.Entity{
		Kind:     entity.KindDeclaration,
		Name:     name,
		DeclKind: declKind,
		Body:     []byte(body),
	}
	e.ComputeHash()
	return e
}

// --- Unit tests for DetectRenames ---

func TestDetectRenames_ExactBodyHash(t *testing.T) {
	// Identical body, different name => definite rename (similarity 1.0)
	body := "func Foo() {\n\treturn 42\n}"
	deleted := map[string]*entity.Entity{
		"old-key": makeEntityPtr(makeRenameEntity("Foo", "function_definition", body)),
	}
	added := map[string]*entity.Entity{
		"new-key": makeEntityPtr(makeRenameEntity("Bar", "function_definition", body)),
	}

	renames := DetectRenames(deleted, added, 0.80)

	if len(renames) != 1 {
		t.Fatalf("expected 1 rename, got %d", len(renames))
	}
	if renames[0].OldKey != "old-key" {
		t.Errorf("expected OldKey %q, got %q", "old-key", renames[0].OldKey)
	}
	if renames[0].NewKey != "new-key" {
		t.Errorf("expected NewKey %q, got %q", "new-key", renames[0].NewKey)
	}
	if renames[0].Similarity != 1.0 {
		t.Errorf("expected Similarity 1.0, got %f", renames[0].Similarity)
	}
}

func TestDetectRenames_HighSimilarity(t *testing.T) {
	// Bodies are very similar but not identical => above threshold
	// 9 shared lines out of 10 total each side => similarity ~0.9
	oldBody := "func Foo() {\n\tx := 1\n\ty := 2\n\tz := 3\n\tw := 4\n\tv := 5\n\tu := 6\n\tq := 7\n\tp := 8\n\treturn x + y + z\n}"
	newBody := "func Bar() {\n\tx := 1\n\ty := 2\n\tz := 3\n\tw := 4\n\tv := 5\n\tu := 6\n\tq := 7\n\tp := 8\n\treturn x + y + z + 1\n}"

	deleted := map[string]*entity.Entity{
		"old-key": makeEntityPtr(makeRenameEntity("Foo", "function_definition", oldBody)),
	}
	added := map[string]*entity.Entity{
		"new-key": makeEntityPtr(makeRenameEntity("Bar", "function_definition", newBody)),
	}

	renames := DetectRenames(deleted, added, 0.80)

	if len(renames) != 1 {
		t.Fatalf("expected 1 rename, got %d", len(renames))
	}
	if renames[0].Similarity <= 0.80 {
		t.Errorf("expected similarity > 0.80, got %f", renames[0].Similarity)
	}
	if renames[0].Similarity >= 1.0 {
		t.Errorf("expected similarity < 1.0, got %f", renames[0].Similarity)
	}
}

func TestDetectRenames_BelowThreshold(t *testing.T) {
	// Bodies are very different => below threshold, no rename detected
	oldBody := "func Foo() {\n\treturn 1\n}"
	newBody := "func Bar() {\n\tx := doSomethingCompletelyDifferent()\n\ty := transform(x)\n\tz := finalize(y)\n\treturn z\n}"

	deleted := map[string]*entity.Entity{
		"old-key": makeEntityPtr(makeRenameEntity("Foo", "function_definition", oldBody)),
	}
	added := map[string]*entity.Entity{
		"new-key": makeEntityPtr(makeRenameEntity("Bar", "function_definition", newBody)),
	}

	renames := DetectRenames(deleted, added, 0.80)

	if len(renames) != 0 {
		t.Fatalf("expected 0 renames, got %d (similarity=%f)", len(renames), renames[0].Similarity)
	}
}

func TestDetectRenames_DifferentDeclKind(t *testing.T) {
	// Same body but different DeclKind => should not match
	body := "something {\n\treturn 42\n}"
	deleted := map[string]*entity.Entity{
		"old-key": makeEntityPtr(makeRenameEntity("Foo", "function_definition", body)),
	}
	added := map[string]*entity.Entity{
		"new-key": makeEntityPtr(makeRenameEntity("Bar", "type_definition", body)),
	}

	renames := DetectRenames(deleted, added, 0.80)

	if len(renames) != 0 {
		t.Fatalf("expected 0 renames for different DeclKind, got %d", len(renames))
	}
}

func TestDetectRenames_MultipleDeletedOneAdded(t *testing.T) {
	// Two deleted, one added — should pick the best match
	body1 := "func Foo() {\n\treturn 42\n}"
	body2 := "func Baz() {\n\treturn 99\n}"

	deleted := map[string]*entity.Entity{
		"del-1": makeEntityPtr(makeRenameEntity("Foo", "function_definition", body1)),
		"del-2": makeEntityPtr(makeRenameEntity("Baz", "function_definition", body2)),
	}
	added := map[string]*entity.Entity{
		"add-1": makeEntityPtr(makeRenameEntity("Bar", "function_definition", body1)),
	}

	renames := DetectRenames(deleted, added, 0.80)

	if len(renames) != 1 {
		t.Fatalf("expected 1 rename, got %d", len(renames))
	}
	if renames[0].OldKey != "del-1" {
		t.Errorf("expected OldKey %q (exact match), got %q", "del-1", renames[0].OldKey)
	}
	if renames[0].Similarity != 1.0 {
		t.Errorf("expected exact match (1.0), got %f", renames[0].Similarity)
	}
}

func TestDetectRenames_NoDeletedEntities(t *testing.T) {
	added := map[string]*entity.Entity{
		"add-1": makeEntityPtr(makeRenameEntity("Bar", "function_definition", "func Bar() {}")),
	}

	renames := DetectRenames(nil, added, 0.80)

	if len(renames) != 0 {
		t.Fatalf("expected 0 renames with no deleted entities, got %d", len(renames))
	}
}

func TestDetectRenames_NoAddedEntities(t *testing.T) {
	deleted := map[string]*entity.Entity{
		"del-1": makeEntityPtr(makeRenameEntity("Foo", "function_definition", "func Foo() {}")),
	}

	renames := DetectRenames(deleted, nil, 0.80)

	if len(renames) != 0 {
		t.Fatalf("expected 0 renames with no added entities, got %d", len(renames))
	}
}

// --- Integration tests with MatchEntities ---

func TestMatchEntities_RenameDetection_OursRenamed(t *testing.T) {
	// Base has "Foo". Ours renames to "Bar" (same body). Theirs keeps "Foo".
	body := "func Foo() {\n\tx := 1\n\ty := 2\n\treturn x + y\n}"
	renamedBody := "func Bar() {\n\tx := 1\n\ty := 2\n\treturn x + y\n}"

	baseFoo := makeRenameEntity("Foo", "function_definition", body)
	oursFoo := makeRenameEntity("Bar", "function_definition", renamedBody)
	theirsFoo := makeRenameEntity("Foo", "function_definition", body)

	base := makeEntityList([]entity.Entity{baseFoo})
	ours := makeEntityList([]entity.Entity{oursFoo})
	theirs := makeEntityList([]entity.Entity{theirsFoo})

	matches := MatchEntities(base, ours, theirs)

	// Should detect rename: Foo -> Bar by ours
	var renamedMatch *MatchedEntity
	for i := range matches {
		if matches[i].Disposition == RenamedOurs {
			renamedMatch = &matches[i]
			break
		}
	}
	if renamedMatch == nil {
		t.Fatal("expected to find a RenamedOurs disposition")
	}
	if renamedMatch.Base == nil {
		t.Error("Base should be non-nil for rename")
	}
	if renamedMatch.Ours == nil {
		t.Error("Ours should be non-nil for rename")
	}
}

func TestMatchEntities_RenameDetection_TheirsRenamed(t *testing.T) {
	// Base has "Foo". Ours keeps "Foo". Theirs renames to "Baz" (same body).
	body := "func Foo() {\n\tx := 1\n\ty := 2\n\treturn x + y\n}"
	renamedBody := "func Baz() {\n\tx := 1\n\ty := 2\n\treturn x + y\n}"

	baseFoo := makeRenameEntity("Foo", "function_definition", body)
	oursFoo := makeRenameEntity("Foo", "function_definition", body)
	theirsFoo := makeRenameEntity("Baz", "function_definition", renamedBody)

	base := makeEntityList([]entity.Entity{baseFoo})
	ours := makeEntityList([]entity.Entity{oursFoo})
	theirs := makeEntityList([]entity.Entity{theirsFoo})

	matches := MatchEntities(base, ours, theirs)

	var renamedMatch *MatchedEntity
	for i := range matches {
		if matches[i].Disposition == RenamedTheirs {
			renamedMatch = &matches[i]
			break
		}
	}
	if renamedMatch == nil {
		t.Fatal("expected to find a RenamedTheirs disposition")
	}
	if renamedMatch.Base == nil {
		t.Error("Base should be non-nil for rename")
	}
	if renamedMatch.Theirs == nil {
		t.Error("Theirs should be non-nil for rename")
	}
}

func TestMatchEntities_RenameConflict_BothRenameDifferently(t *testing.T) {
	// Base has "Foo". Ours renames to "Bar". Theirs renames to "Baz".
	// Both sides delete "Foo" and add a new entity — but rename to different names.
	body := "func Foo() {\n\tx := 1\n\ty := 2\n\treturn x + y\n}"
	oursBody := "func Bar() {\n\tx := 1\n\ty := 2\n\treturn x + y\n}"
	theirsBody := "func Baz() {\n\tx := 1\n\ty := 2\n\treturn x + y\n}"

	baseFoo := makeRenameEntity("Foo", "function_definition", body)
	oursBar := makeRenameEntity("Bar", "function_definition", oursBody)
	theirsBaz := makeRenameEntity("Baz", "function_definition", theirsBody)

	base := makeEntityList([]entity.Entity{baseFoo})
	ours := makeEntityList([]entity.Entity{oursBar})
	theirs := makeEntityList([]entity.Entity{theirsBaz})

	matches := MatchEntities(base, ours, theirs)

	// Should produce a Conflict since both sides renamed to different names
	var conflictMatch *MatchedEntity
	for i := range matches {
		if matches[i].Disposition == Conflict {
			conflictMatch = &matches[i]
			break
		}
	}
	if conflictMatch == nil {
		t.Fatal("expected to find a Conflict disposition for divergent renames")
	}
}

// --- Helper ---

func makeEntityPtr(e entity.Entity) *entity.Entity {
	return &e
}
