package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestStatus_ShowsShadowDesyncWarning(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	// Write a fake shadow-failures.log into .graft/
	logPath := filepath.Join(dir, ".graft", "shadow-failures.log")
	if err := os.WriteFile(logPath, []byte("shadow op failed\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(out.String(), "git shadow out of sync") {
		t.Errorf("expected shadow desync warning in output, got:\n%s", out.String())
	}
}

func TestStatus_NoShadowDesyncWarningWhenClean(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.Contains(out.String(), "git shadow out of sync") {
		t.Errorf("unexpected shadow desync warning in output:\n%s", out.String())
	}
}

func TestStatus_ShadowDesyncJSON(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	// Write a fake shadow-failures.log into .graft/
	logPath := filepath.Join(dir, ".graft", "shadow-failures.log")
	if err := os.WriteFile(logPath, []byte("shadow op failed\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONStatusOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if !result.ShadowDesync {
		t.Error("shadow_desync = false, want true")
	}
}

func TestStatus_ShadowDesyncJSON_Clean(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result JSONStatusOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}

	if result.ShadowDesync {
		t.Error("shadow_desync = true, want false (no failures log)")
	}

	// Also verify the field is omitted from the JSON when false
	if strings.Contains(out.String(), "shadow_desync") {
		t.Errorf("shadow_desync should be omitted when false, got:\n%s", out.String())
	}
}
