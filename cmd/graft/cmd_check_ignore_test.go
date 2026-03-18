package main

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestCheckIgnoreCmdJSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	writeVerifyCmdFile(t, filepath.Join(dir, ".graftignore"), []byte("orchard\n"))
	if err := r.Add([]string{".graftignore"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newCheckIgnoreCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json", "cmd/orchard/main.go"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result struct {
		Results []checkIgnoreResult `json:"results"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if len(result.Results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(result.Results))
	}
	if result.Results[0].Path != "cmd/orchard/main.go" {
		t.Fatalf("path = %q, want %q", result.Results[0].Path, "cmd/orchard/main.go")
	}
	if result.Results[0].Graft == nil || !result.Results[0].Graft.Ignored {
		t.Fatal("expected graft ignore diagnostics to report the path as ignored")
	}
	if result.Results[0].Graft.Final == nil || result.Results[0].Graft.Final.Pattern != "orchard" {
		t.Fatalf("final graft pattern = %#v, want %q", result.Results[0].Graft.Final, "orchard")
	}
	if result.Results[0].Graft.MatchedPath != "cmd/orchard" {
		t.Fatalf("matchedPath = %q, want %q", result.Results[0].Graft.MatchedPath, "cmd/orchard")
	}
}
