package repo

import (
	"testing"
)

func TestTagCreateResolveAndList(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.CreateTag("v1.0.0", head, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	resolved, err := r.ResolveTag("v1.0.0")
	if err != nil {
		t.Fatalf("ResolveTag: %v", err)
	}
	if resolved != head {
		t.Fatalf("resolved tag = %q, want %q", resolved, head)
	}

	tags, err := r.ListTags()
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "v1.0.0" {
		t.Fatalf("ListTags = %v, want [v1.0.0]", tags)
	}
}

func TestTagCreateExistingWithoutForceFails(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.CreateTag("v1.0.0", head, false); err != nil {
		t.Fatalf("CreateTag first: %v", err)
	}
	if err := r.CreateTag("v1.0.0", head, false); err == nil {
		t.Fatalf("CreateTag second without force should fail")
	}
}

func TestTagCreateForceUpdatesTarget(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	h1, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit h1: %v", err)
	}

	if err := r.CreateTag("v1.0.0", h1, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h2, err := r.Commit("second", "test-author")
	if err != nil {
		t.Fatalf("Commit h2: %v", err)
	}

	if err := r.CreateTag("v1.0.0", h2, true); err != nil {
		t.Fatalf("CreateTag force: %v", err)
	}
	resolved, err := r.ResolveTag("v1.0.0")
	if err != nil {
		t.Fatalf("ResolveTag: %v", err)
	}
	if resolved != h2 {
		t.Fatalf("resolved tag = %q, want %q", resolved, h2)
	}
}

func TestTagDelete(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := r.CreateTag("v1.0.0", head, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	if err := r.DeleteTag("v1.0.0"); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	if _, err := r.ResolveTag("v1.0.0"); err == nil {
		t.Fatalf("ResolveTag should fail after delete")
	}
}
