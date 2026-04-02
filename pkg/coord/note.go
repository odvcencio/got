package coord

import (
	"fmt"
	"sort"
	"time"
)

// Note represents shared in-progress coordination material stored in
// refs/coord/notes/. Notes are distinct from plans: plans are canonical,
// assignable design/program records; notes are shared scratch, handoff, and
// status material that should stay out of tracked source history.
type Note struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	Kind      string    `json:"kind"`   // scratch, handoff, status, decision
	Status    string    `json:"status"` // active, paused, resolved, archived
	Author    string    `json:"author,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	PlanID    string    `json:"plan_id,omitempty"`
	TaskID    string    `json:"task_id,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

var validNoteKinds = map[string]bool{
	"scratch":  true,
	"handoff":  true,
	"status":   true,
	"decision": true,
}

var validNoteStatuses = map[string]bool{
	"active":   true,
	"paused":   true,
	"resolved": true,
	"archived": true,
}

func validateNoteKind(kind string) error {
	if !validNoteKinds[kind] {
		return fmt.Errorf("invalid note kind %q: must be one of scratch, handoff, status, decision", kind)
	}
	return nil
}

func validateNoteStatus(status string) error {
	if !validNoteStatuses[status] {
		return fmt.Errorf("invalid note status %q: must be one of active, paused, resolved, archived", status)
	}
	return nil
}

func generateNoteID() string {
	return generatePlanID()
}

// CreateNote stores a new note under refs/coord/notes/{id}.
func (c *Coordinator) CreateNote(note *Note) error {
	if note.Title == "" {
		return fmt.Errorf("note title is required")
	}
	if note.ID == "" {
		note.ID = generateNoteID()
	}
	now := time.Now().UTC()
	note.CreatedAt = now
	note.UpdatedAt = now
	if note.Kind == "" {
		note.Kind = "scratch"
	}
	if note.Status == "" {
		note.Status = "active"
	}
	if err := validateNoteKind(note.Kind); err != nil {
		return err
	}
	if err := validateNoteStatus(note.Status); err != nil {
		return err
	}

	h, err := c.writeJSONBlob(note)
	if err != nil {
		return fmt.Errorf("write note blob: %w", err)
	}
	return c.Repo.UpdateRef(refPath("notes", note.ID), h)
}

// GetNote reads a note by ID from refs/coord/notes/{id}.
func (c *Coordinator) GetNote(id string) (*Note, error) {
	h, err := c.Repo.ResolveRef(refPath("notes", id))
	if err != nil {
		return nil, fmt.Errorf("note %q not found: %w", id, err)
	}
	var note Note
	if err := c.readJSONBlob(h, &note); err != nil {
		return nil, fmt.Errorf("read note: %w", err)
	}
	return &note, nil
}

// UpdateNote overwrites a note blob and updates the ref.
func (c *Coordinator) UpdateNote(note *Note) error {
	if note.ID == "" {
		return fmt.Errorf("note id is required")
	}
	if note.Title == "" {
		return fmt.Errorf("note title is required")
	}
	if err := validateNoteKind(note.Kind); err != nil {
		return err
	}
	if err := validateNoteStatus(note.Status); err != nil {
		return err
	}
	note.UpdatedAt = time.Now().UTC()
	h, err := c.writeJSONBlob(note)
	if err != nil {
		return fmt.Errorf("write note blob: %w", err)
	}
	return c.Repo.UpdateRef(refPath("notes", note.ID), h)
}

// ListNotes returns all notes stored under refs/coord/notes/.
func (c *Coordinator) ListNotes() ([]*Note, error) {
	refs, err := c.Repo.ListRefs("coord/notes")
	if err != nil {
		return nil, fmt.Errorf("list note refs: %w", err)
	}
	var notes []*Note
	for _, hash := range refs {
		var note Note
		if err := c.readJSONBlob(hash, &note); err != nil {
			continue
		}
		notes = append(notes, &note)
	}
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].UpdatedAt.After(notes[j].UpdatedAt)
	})
	return notes, nil
}

// DeleteNote removes a note from refs/coord/notes/{id}.
func (c *Coordinator) DeleteNote(id string) error {
	ref := refPath("notes", id)
	oldHash, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return fmt.Errorf("note %q not found: %w", id, err)
	}
	return c.Repo.DeleteRefCAS(ref, oldHash)
}
