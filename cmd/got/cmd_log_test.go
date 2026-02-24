package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/repo"
)

func TestParseLogEntitySelector(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    logEntitySelector
		wantErr bool
	}{
		{
			name:  "plain key",
			input: "decl:function_declaration::Target:func Target() int:0",
			want: logEntitySelector{
				Key: "decl:function_declaration::Target:func Target() int:0",
			},
		},
		{
			name:  "path selector",
			input: "pkg/../a.go::decl:function_declaration::Target:func Target() int:0",
			want: logEntitySelector{
				Path: "a.go",
				Key:  "decl:function_declaration::Target:func Target() int:0",
			},
		},
		{
			name:    "empty selector",
			input:   "  ",
			wantErr: true,
		},
		{
			name:    "missing key in path form",
			input:   "a.go::",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLogEntitySelector(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLogEntitySelector(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("selector = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestLogCmd_EntityFilter(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	aV1 := "package demo\n\nfunc Target() int {\n\treturn 1\n}\n"
	aV2 := "package demo\n\nfunc Target() int {\n\treturn 2\n}\n"
	bV1 := "package demo\n\nfunc Target() int {\n\treturn 10\n}\n"
	bV2 := "package demo\n\nfunc Target() int {\n\treturn 11\n}\n"

	writeRepoFile(t, dir, "a.go", aV1)
	stageAndCommit(t, r, "a.go", "add a target")

	targetKey := declarationKeyByName(t, "a.go", aV1, "Target")

	writeRepoFile(t, dir, "b.go", bV1)
	stageAndCommit(t, r, "b.go", "add b target")

	writeRepoFile(t, dir, "b.go", bV2)
	stageAndCommit(t, r, "b.go", "touch b target")

	writeRepoFile(t, dir, "a.go", aV2)
	stageAndCommit(t, r, "a.go", "touch a target")

	pathOutput := runLogCommand(t, dir, "--oneline", "--entity", "a.go::"+targetKey, "--limit", "10")
	pathLines := nonEmptyLines(pathOutput)
	if len(pathLines) != 2 {
		t.Fatalf("path selector returned %d lines, want 2\noutput:\n%s", len(pathLines), pathOutput)
	}
	assertLineContainsMessage(t, pathLines[0], "touch a target")
	assertLineContainsMessage(t, pathLines[1], "add a target")
	if strings.Contains(pathOutput, "touch b target") {
		t.Fatalf("path selector unexpectedly included b.go change:\n%s", pathOutput)
	}

	globalOutput := runLogCommand(t, dir, "--oneline", "--entity", targetKey, "--limit", "10")
	globalLines := nonEmptyLines(globalOutput)
	if len(globalLines) != 4 {
		t.Fatalf("global selector returned %d lines, want 4\noutput:\n%s", len(globalLines), globalOutput)
	}
	assertLineContainsMessage(t, globalLines[0], "touch a target")
	assertLineContainsMessage(t, globalLines[1], "touch b target")
	assertLineContainsMessage(t, globalLines[2], "add b target")
	assertLineContainsMessage(t, globalLines[3], "add a target")
}

func declarationKeyByName(t *testing.T, path, source, name string) string {
	t.Helper()

	el, err := entity.Extract(path, []byte(source))
	if err != nil {
		t.Fatalf("entity.Extract(%q): %v", path, err)
	}
	for i := range el.Entities {
		e := el.Entities[i]
		if e.Kind == entity.KindDeclaration && e.Name == name {
			return e.IdentityKey()
		}
	}
	t.Fatalf("declaration %q not found in %q", name, path)
	return ""
}

func stageAndCommit(t *testing.T, r *repo.Repo, path, message string) {
	t.Helper()

	if err := r.Add([]string{path}); err != nil {
		t.Fatalf("Add(%q): %v", path, err)
	}
	if _, err := r.Commit(message, "tester"); err != nil {
		t.Fatalf("Commit(%q): %v", message, err)
	}
}

func writeRepoFile(t *testing.T, root, relPath, content string) {
	t.Helper()

	absPath := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", relPath, err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", relPath, err)
	}
}

func runLogCommand(t *testing.T, repoDir string, args ...string) string {
	t.Helper()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Chdir(%q): %v", repoDir, err)
	}
	defer func() {
		if err := os.Chdir(prevWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	cmd := newLogCmd()
	cmd.SetArgs(args)

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("log command failed (%v): %v\noutput:\n%s", args, err, output.String())
	}

	return output.String()
}

func nonEmptyLines(s string) []string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func assertLineContainsMessage(t *testing.T, line, message string) {
	t.Helper()

	if !strings.Contains(line, message) {
		t.Fatalf("line %q does not contain %q", line, message)
	}
}
