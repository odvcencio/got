package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestReflogCmd_Basic(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	// Create a file and commit to generate reflog entries.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirReflog(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newReflogCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	output := out.String()
	if output == "" {
		t.Fatal("expected reflog output, got empty")
	}
	// Should contain the ref name and reason.
	if !strings.Contains(output, "refs/heads/main") {
		t.Errorf("output should contain refs/heads/main, got:\n%s", output)
	}
}

func TestReflogCmd_EntityFilter(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	// First commit: Go file with two functions.
	src1 := "package main\n\nfunc LoginHandler() string { return \"login\" }\n\nfunc ProcessOrder() int { return 42 }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src1), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Second commit: modify LoginHandler.
	src2 := "package main\n\nfunc LoginHandler() string { return \"updated\" }\n\nfunc ProcessOrder() int { return 42 }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src2), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("update login", "bob"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirReflog(t, dir)
	defer restore()

	// Filter by entity with wildcard: should show entries with matching entities.
	var out bytes.Buffer
	cmd := newReflogCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--entity", "declaration:*"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute --entity: %v", err)
	}

	output := out.String()
	// Should have entity change lines indented with two spaces.
	if !strings.Contains(output, "  main.go") {
		t.Errorf("expected entity detail lines in output, got:\n%s", output)
	}
}

func TestReflogCmd_EntityFilterNoMatch(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	// Simple commit with a Go file.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc Hello() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "alice"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	restore := chdirReflog(t, dir)
	defer restore()

	// Filter by a non-matching entity pattern.
	var out bytes.Buffer
	cmd := newReflogCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--entity", "type:NoSuchType*"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Should produce empty output since no entities match.
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("expected empty output for non-matching entity filter, got:\n%s", out.String())
	}
}

func TestMatchEntityFilter(t *testing.T) {
	tests := []struct {
		filter    string
		entityKey string
		want      bool
	}{
		{"func:*Handler", "func:LoginHandler", true},
		{"func:*Handler", "func:LogoutHandler", true},
		{"func:*Handler", "type:Config", false},
		{"type:Config*", "type:ConfigManager", true},
		{"type:Config*", "type:Config", true},
		{"type:Config*", "type:Connection", false},
		{"declaration:*", "declaration:Hello", true},
		{"declaration:*", "declaration:World", true},
		{"declaration:Hello", "declaration:Hello", true},
		{"declaration:Hello", "declaration:World", false},
		// No colon: full glob match.
		{"*Hello*", "declaration:Hello", true},
		{"*Hello*", "func:HelloHandler", true},
	}

	for _, tt := range tests {
		t.Run(tt.filter+"_"+tt.entityKey, func(t *testing.T) {
			got := matchEntityFilter(tt.filter, tt.entityKey)
			if got != tt.want {
				t.Errorf("matchEntityFilter(%q, %q) = %v, want %v", tt.filter, tt.entityKey, got, tt.want)
			}
		})
	}
}

func chdirReflog(t *testing.T, dir string) func() {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	return func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd %s: %v", wd, err)
		}
	}
}
