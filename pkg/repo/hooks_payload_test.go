package repo

import (
	"encoding/json"
	"testing"
)

func TestPreCommitPayloadRoundTrip(t *testing.T) {
	original := PreCommitPayload{
		Hook:   "pre-commit",
		Repo:   "/tmp/repo",
		Branch: "main",
		Author: "alice",
		StagedFiles: []StagedFile{
			{Path: "main.go", Status: "modified"},
			{Path: "new.go", Status: "added"},
		},
		EntityDiff: &EntityDiff{
			Added:    []EntityChange{{Path: "new.go", Kind: "function", Name: "New", DeclKind: "func", SignatureChanged: false}},
			Modified: []EntityChange{{Path: "main.go", Kind: "function", Name: "main", DeclKind: "func", SignatureChanged: true}},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded PreCommitPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Hook != original.Hook {
		t.Errorf("Hook = %q, want %q", decoded.Hook, original.Hook)
	}
	if decoded.Author != original.Author {
		t.Errorf("Author = %q, want %q", decoded.Author, original.Author)
	}
	if len(decoded.StagedFiles) != 2 {
		t.Fatalf("StagedFiles len = %d, want 2", len(decoded.StagedFiles))
	}
	if decoded.StagedFiles[0].Path != "main.go" {
		t.Errorf("StagedFiles[0].Path = %q, want %q", decoded.StagedFiles[0].Path, "main.go")
	}
	if decoded.EntityDiff == nil {
		t.Fatal("EntityDiff is nil after round-trip")
	}
	if len(decoded.EntityDiff.Added) != 1 {
		t.Errorf("EntityDiff.Added len = %d, want 1", len(decoded.EntityDiff.Added))
	}
	if len(decoded.EntityDiff.Modified) != 1 {
		t.Errorf("EntityDiff.Modified len = %d, want 1", len(decoded.EntityDiff.Modified))
	}
	if !decoded.EntityDiff.Modified[0].SignatureChanged {
		t.Error("EntityDiff.Modified[0].SignatureChanged = false, want true")
	}
}

func TestPostCommitPayloadRoundTrip(t *testing.T) {
	original := PostCommitPayload{
		Hook:    "post-commit",
		Repo:    "/tmp/repo",
		Branch:  "main",
		Commit:  "abc123",
		Message: "initial commit",
		Author:  "bob",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded PostCommitPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Commit != original.Commit {
		t.Errorf("Commit = %q, want %q", decoded.Commit, original.Commit)
	}
	if decoded.Message != original.Message {
		t.Errorf("Message = %q, want %q", decoded.Message, original.Message)
	}
}

func TestPrePushPayloadRoundTrip(t *testing.T) {
	original := PrePushPayload{
		Hook:      "pre-push",
		Repo:      "/tmp/repo",
		Remote:    "origin",
		RemoteURL: "https://example.com/repo.git",
		Refs: []HookRefUpdate{
			{
				Name:       "refs/heads/main",
				Old:        "aaa",
				New:        "bbb",
				LocalRef:   "refs/heads/main",
				RemoteRef:  "refs/heads/main",
				LocalHash:  "bbb",
				RemoteHash: "aaa",
			},
		},
		Commits: []string{"abc", "def"},
		EntityDiff: &EntityDiff{
			Removed: []EntityChange{{Path: "old.go", Kind: "function", Name: "Old", DeclKind: "func"}},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded PrePushPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(decoded.Refs) != 1 {
		t.Fatalf("Refs len = %d, want 1", len(decoded.Refs))
	}
	if decoded.Refs[0].LocalHash != "bbb" {
		t.Errorf("Refs[0].LocalHash = %q, want %q", decoded.Refs[0].LocalHash, "bbb")
	}
	if len(decoded.Commits) != 2 {
		t.Fatalf("Commits len = %d, want 2", len(decoded.Commits))
	}
	if decoded.EntityDiff == nil || len(decoded.EntityDiff.Removed) != 1 {
		t.Error("EntityDiff.Removed not preserved")
	}
}

func TestPostPushPayloadRoundTrip(t *testing.T) {
	original := PostPushPayload{
		Hook:      "post-push",
		Remote:    "origin",
		RemoteURL: "https://example.com/repo.git",
		Refs: []HookRefUpdate{
			{Name: "refs/heads/main", Old: "aaa", New: "bbb"},
		},
		ObjectsPushed: 42,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded PostPushPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.ObjectsPushed != 42 {
		t.Errorf("ObjectsPushed = %d, want 42", decoded.ObjectsPushed)
	}
	if decoded.Remote != "origin" {
		t.Errorf("Remote = %q, want %q", decoded.Remote, "origin")
	}
}

func TestNilEntityDiffOmitted(t *testing.T) {
	p := PreCommitPayload{Hook: "pre-commit", Repo: "/tmp"}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// entity_diff should be omitted when nil.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, ok := raw["entity_diff"]; ok {
		t.Error("entity_diff should be omitted when nil")
	}
}
