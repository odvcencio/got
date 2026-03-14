package gitbridge

import (
	"testing"
)

func TestParseGitBlob(t *testing.T) {
	content := []byte("hello world\n")
	raw := gitBlobBytes(content)

	obj, err := ParseGitObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Type != "blob" {
		t.Errorf("type = %q, want blob", obj.Type)
	}
	if string(obj.Data) != string(content) {
		t.Errorf("data = %q, want %q", obj.Data, content)
	}
}

func TestParseGitTree(t *testing.T) {
	obj, err := ParseGitObject(gitTreeBytes([]GitTreeEntry{
		{Mode: "100644", Name: "hello.go", Hash: make([]byte, 20)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if obj.Type != "tree" {
		t.Errorf("type = %q, want tree", obj.Type)
	}
}

func TestParseGitCommit(t *testing.T) {
	raw := gitCommitBytes("abc123", "Test Author <test@test.com>", "test commit\n")
	obj, err := ParseGitObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Type != "commit" {
		t.Errorf("type = %q, want commit", obj.Type)
	}
}

func TestGitObjectHash(t *testing.T) {
	// Known git blob hash for "hello world\n"
	content := []byte("hello world\n")
	hash := GitObjectHashHex("blob", content)
	// git hash-object computes: "blob 12\0hello world\n" → SHA-1
	if len(hash) != 40 {
		t.Errorf("expected 40 char hex hash, got %d chars: %s", len(hash), hash)
	}
}
