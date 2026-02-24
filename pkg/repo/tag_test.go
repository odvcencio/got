package repo

import (
	"strings"
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

func TestCreateAnnotatedTagStoresTagObjectAndRef(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	tagHash, err := r.CreateAnnotatedTag("v1.0.0", head, "Alice <alice@example.com>", "release 1.0.0", false)
	if err != nil {
		t.Fatalf("CreateAnnotatedTag: %v", err)
	}
	if tagHash == "" {
		t.Fatalf("CreateAnnotatedTag returned empty hash")
	}
	if tagHash == head {
		t.Fatalf("annotated tag hash should differ from target commit hash")
	}

	resolvedRef, err := r.ResolveTag("v1.0.0")
	if err != nil {
		t.Fatalf("ResolveTag: %v", err)
	}
	if resolvedRef != tagHash {
		t.Fatalf("resolved tag ref = %q, want %q", resolvedRef, tagHash)
	}

	tag, err := r.Store.ReadTag(tagHash)
	if err != nil {
		t.Fatalf("ReadTag(%s): %v", tagHash, err)
	}
	if tag.TargetHash != head {
		t.Fatalf("tag target = %q, want %q", tag.TargetHash, head)
	}
	data := string(tag.Data)
	if !strings.Contains(data, "object "+string(head)+"\n") {
		t.Fatalf("tag payload missing object header: %q", data)
	}
	if !strings.Contains(data, "type commit\n") {
		t.Fatalf("tag payload missing commit type: %q", data)
	}
	if !strings.Contains(data, "tag v1.0.0\n") {
		t.Fatalf("tag payload missing name: %q", data)
	}
	if !strings.Contains(data, "tagger Alice <alice@example.com> ") {
		t.Fatalf("tag payload missing tagger: %q", data)
	}
	if !strings.Contains(data, "\n\nrelease 1.0.0\n") {
		t.Fatalf("tag payload missing message: %q", data)
	}
}

func TestCreateAnnotatedTagRequiresMessage(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	head, err := r.Commit("initial", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, err := r.CreateAnnotatedTag("v1.0.0", head, "Alice <alice@example.com>", "   ", false); err == nil {
		t.Fatalf("expected CreateAnnotatedTag to fail without message")
	}
}
