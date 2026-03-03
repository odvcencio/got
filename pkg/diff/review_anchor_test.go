package diff

import (
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
)

// TestResolveCommentPosition_Found verifies that a comment anchored to an
// entity key resolves to the correct line range.
func TestResolveCommentPosition_Found(t *testing.T) {
	el, err := entity.Extract("main.go", []byte(goBase))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Find the Hello function's identity key.
	var helloKey string
	for i := range el.Entities {
		e := &el.Entities[i]
		if e.Kind == entity.KindDeclaration && e.Name == "Hello" {
			helloKey = e.IdentityKey()
			break
		}
	}
	if helloKey == "" {
		t.Fatal("could not find Hello entity")
	}

	start, end, ok := ResolveCommentPosition(el, helloKey)
	if !ok {
		t.Fatal("expected entity to be found")
	}
	if start <= 0 || end <= 0 {
		t.Errorf("expected positive line numbers, got start=%d end=%d", start, end)
	}
	if end < start {
		t.Errorf("end (%d) should be >= start (%d)", end, start)
	}
}

// TestResolveCommentPosition_NotFound verifies that resolving a nonexistent
// entity key returns ok=false.
func TestResolveCommentPosition_NotFound(t *testing.T) {
	el, err := entity.Extract("main.go", []byte(goBase))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	start, end, ok := ResolveCommentPosition(el, "decl:function_definition:::nonexistent:0")
	if ok {
		t.Errorf("expected ok=false for nonexistent key, got start=%d end=%d", start, end)
	}
}

// TestResolveCommentPosition_SurvivesRebase verifies that a comment anchored
// to an entity key resolves correctly even when lines shift (simulating what
// happens after a rebase adds code above the target entity).
func TestResolveCommentPosition_SurvivesRebase(t *testing.T) {
	// Extract entities from the base version.
	baselist, err := entity.Extract("main.go", []byte(goBase))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Find Goodbye's identity key in the base.
	var goodbyeKey string
	for i := range baselist.Entities {
		e := &baselist.Entities[i]
		if e.Kind == entity.KindDeclaration && e.Name == "Goodbye" {
			goodbyeKey = e.IdentityKey()
			break
		}
	}
	if goodbyeKey == "" {
		t.Fatal("could not find Goodbye entity in base")
	}

	baseStart, _, baseOK := ResolveCommentPosition(baselist, goodbyeKey)
	if !baseOK {
		t.Fatal("expected Goodbye to resolve in base")
	}

	// Now resolve the same key in a version where lines shifted (added func above).
	afterList, err := entity.Extract("main.go", []byte(goAddedFunc))
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	afterStart, afterEnd, afterOK := ResolveCommentPosition(afterList, goodbyeKey)
	if !afterOK {
		t.Fatalf("expected Goodbye to resolve in after (key survived rebase)")
	}
	if afterEnd < afterStart {
		t.Errorf("end (%d) should be >= start (%d)", afterEnd, afterStart)
	}

	// Lines should have shifted down due to the added function.
	if afterStart <= baseStart {
		t.Errorf("expected Goodbye to move down after adding function above: base start=%d, after start=%d",
			baseStart, afterStart)
	}
}

// TestReviewComment_Struct verifies the ReviewComment struct fields.
func TestReviewComment_Struct(t *testing.T) {
	c := ReviewComment{
		EntityKey: "decl:function_definition:::Hello:0",
		Body:      "This function needs error handling.",
	}
	if c.EntityKey == "" {
		t.Error("expected non-empty EntityKey")
	}
	if c.Body == "" {
		t.Error("expected non-empty Body")
	}
}
