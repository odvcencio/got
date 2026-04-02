package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit_FreshDirCreatesBothGraftAndGit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "myrepo")

	cmd := newInitCmd()
	cmd.SetArgs([]string{target})
	cmd.SetOut(&strings.Builder{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// .graft/ must exist
	if _, err := os.Stat(filepath.Join(target, ".graft")); err != nil {
		t.Error("expected .graft/ to exist after init")
	}

	// .git/ must exist
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Error("expected .git/ to exist after init (dual-repo mode)")
	}
}

func TestInit_NoGitFlagSkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "myrepo")

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--no-git", target})
	cmd.SetOut(&strings.Builder{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --no-git failed: %v", err)
	}

	// .graft/ must exist
	if _, err := os.Stat(filepath.Join(target, ".graft")); err != nil {
		t.Error("expected .graft/ to exist after init --no-git")
	}

	// .git/ must NOT exist
	if _, err := os.Stat(filepath.Join(target, ".git")); err == nil {
		t.Error("expected .git/ to NOT exist after init --no-git")
	}
}

func TestInit_GtsExcludedInGitInfoExclude(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "myrepo")

	cmd := newInitCmd()
	cmd.SetArgs([]string{target})
	cmd.SetOut(&strings.Builder{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	excludePath := filepath.Join(target, ".git", "info", "exclude")
	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("could not read .git/info/exclude: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, ".gts/") {
		t.Errorf("expected .git/info/exclude to contain .gts/, got:\n%s", content)
	}
	if !strings.Contains(content, ".graft/") {
		t.Errorf("expected .git/info/exclude to contain .graft/, got:\n%s", content)
	}
}
