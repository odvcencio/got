package main

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestCoordNoteCreateAndList_JSON(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	createOutput := captureCommandStdout(t, func() error {
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "note", "create", "Working thread", "--kind", "scratch", "--body", "tracking in-flight edge cases"})
		return cmd.Execute()
	})

	var created coord.Note
	if err := json.Unmarshal([]byte(createOutput), &created); err != nil {
		t.Fatalf("json.Unmarshal(create): %v\nraw: %s", err, createOutput)
	}
	if created.ID == "" {
		t.Fatal("expected created note ID")
	}
	if created.Kind != "scratch" || created.Status != "active" {
		t.Fatalf("created note = %#v", created)
	}

	listOutput := captureCommandStdout(t, func() error {
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "note", "list"})
		return cmd.Execute()
	})

	var notes []*coord.Note
	if err := json.Unmarshal([]byte(listOutput), &notes); err != nil {
		t.Fatalf("json.Unmarshal(list): %v\nraw: %s", err, listOutput)
	}
	if len(notes) != 1 || notes[0].ID != created.ID {
		t.Fatalf("listed notes = %#v, want created note", notes)
	}
}

func TestCoordNoteUpdateAndGet_JSON(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	c, _, err := openCoordinator()
	if err != nil {
		t.Fatalf("openCoordinator: %v", err)
	}
	note := &coord.Note{Title: "Scratch", Kind: "scratch", Status: "active"}
	if err := c.CreateNote(note); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	if err := func() error {
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"note", "update", note.ID, "--kind", "handoff", "--status", "paused", "--body", "handing this off"})
		return cmd.Execute()
	}(); err != nil {
		t.Fatalf("coord note update: %v", err)
	}

	getOutput := captureCommandStdout(t, func() error {
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--json", "note", "get", note.ID})
		return cmd.Execute()
	})

	var got coord.Note
	if err := json.Unmarshal([]byte(getOutput), &got); err != nil {
		t.Fatalf("json.Unmarshal(get): %v\nraw: %s", err, getOutput)
	}
	if got.Kind != "handoff" || got.Status != "paused" || got.Body != "handing this off" {
		t.Fatalf("updated note = %#v", got)
	}
}
