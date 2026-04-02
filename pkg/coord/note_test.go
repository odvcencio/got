package coord

import "testing"

func TestCreateAndListNotes(t *testing.T) {
	c := newTestCoordinator(t)

	n1 := &Note{Title: "Scratch thread"}
	if err := c.CreateNote(n1); err != nil {
		t.Fatalf("CreateNote 1: %v", err)
	}
	if n1.ID == "" {
		t.Fatal("expected non-empty note ID")
	}
	if n1.Kind != "scratch" {
		t.Fatalf("default kind = %q, want scratch", n1.Kind)
	}
	if n1.Status != "active" {
		t.Fatalf("default status = %q, want active", n1.Status)
	}

	n2 := &Note{Title: "Handoff", Kind: "handoff", Status: "paused"}
	if err := c.CreateNote(n2); err != nil {
		t.Fatalf("CreateNote 2: %v", err)
	}

	notes, err := c.ListNotes()
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
}

func TestGetNote(t *testing.T) {
	c := newTestCoordinator(t)

	note := &Note{
		Title:  "Active status",
		Kind:   "status",
		Status: "active",
		Body:   "working through merge edge cases",
		PlanID: "plan-1",
		TaskID: "task-2",
	}
	if err := c.CreateNote(note); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	got, err := c.GetNote(note.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Title != note.Title {
		t.Fatalf("Title = %q, want %q", got.Title, note.Title)
	}
	if got.PlanID != "plan-1" || got.TaskID != "task-2" {
		t.Fatalf("expected plan/task links, got %#v", got)
	}
}

func TestNoteUpdateAndDelete(t *testing.T) {
	c := newTestCoordinator(t)

	note := &Note{Title: "Scratch"}
	if err := c.CreateNote(note); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	note.Status = "resolved"
	note.Kind = "decision"
	if err := c.UpdateNote(note); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}

	got, err := c.GetNote(note.ID)
	if err != nil {
		t.Fatalf("GetNote(updated): %v", err)
	}
	if got.Status != "resolved" || got.Kind != "decision" {
		t.Fatalf("updated note = %#v", got)
	}

	if err := c.DeleteNote(note.ID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}
	if _, err := c.GetNote(note.ID); err == nil {
		t.Fatal("GetNote after delete should fail")
	}
}

func TestCreateNote_Validation(t *testing.T) {
	c := newTestCoordinator(t)

	if err := c.CreateNote(&Note{Title: "bad", Kind: "spec"}); err == nil {
		t.Fatal("CreateNote with invalid kind should fail")
	}
	if err := c.CreateNote(&Note{Title: "bad", Status: "done"}); err == nil {
		t.Fatal("CreateNote with invalid status should fail")
	}
}
