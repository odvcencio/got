package repo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGrepPattern(t *testing.T) {
	tests := []struct {
		input       string
		wantLang    string
		wantPattern string
	}{
		{"go::func $NAME($$$)", "go", "func $NAME($$$)"},
		{"rust::$EXPR.unwrap()", "rust", "$EXPR.unwrap()"},
		{"javascript::console.log($$$)", "javascript", "console.log($$$)"},
		{"func $NAME($$$)", "", "func $NAME($$$)"},
		{"no prefix here", "", "no prefix here"},
		// Edge: pattern contains :: but prefix has spaces (not a lang prefix).
		{"not a lang::pattern", "", "not a lang::pattern"},
	}

	for _, tt := range tests {
		lang, pattern := parseGrepPattern(tt.input)
		if lang != tt.wantLang {
			t.Errorf("parseGrepPattern(%q): lang = %q, want %q", tt.input, lang, tt.wantLang)
		}
		if pattern != tt.wantPattern {
			t.Errorf("parseGrepPattern(%q): pattern = %q, want %q", tt.input, pattern, tt.wantPattern)
		}
	}
}

func TestGrepHook_BlockOnMatch(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a Go file with functions that return string.
	src := `package main

func Hello(name string) string {
	return "hello " + name
}

func Goodbye(name string) string {
	return "goodbye " + name
}
`
	writeFile(t, filepath.Join(dir, "main.go"), []byte(src))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entry := HookEntry{
		Name:    "no-string-returns",
		Point:   "pre-push",
		Grep:    "go::func $NAME($$$PARAMS) string",
		Action:  "block",
		Message: "Functions returning bare string are not allowed",
	}

	err = runGrepHook(context.Background(), r, entry)
	if err == nil {
		t.Fatal("expected grep hook to return error when matches found with action=block")
	}
	if !strings.Contains(err.Error(), "no-string-returns") {
		t.Errorf("error should mention hook name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Functions returning bare string are not allowed") {
		t.Errorf("error should contain custom message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "main.go") {
		t.Errorf("error should mention matched file, got: %v", err)
	}
	if !strings.Contains(err.Error(), "2 match(es) found") {
		t.Errorf("error should report match count, got: %v", err)
	}
}

func TestGrepHook_WarnOnMatch(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	src := `package main

func Hello(name string) string {
	return "hello " + name
}
`
	writeFile(t, filepath.Join(dir, "main.go"), []byte(src))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entry := HookEntry{
		Name:    "string-warning",
		Point:   "pre-push",
		Grep:    "go::func $NAME($$$PARAMS) string",
		Action:  "warn",
		Message: "Consider using a typed return",
	}

	// warn should not return an error.
	err = runGrepHook(context.Background(), r, entry)
	if err != nil {
		t.Fatalf("expected warn action to not return error, got: %v", err)
	}
}

func TestGrepHook_NoMatchPasses(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	src := `package main

func Hello(name string) string {
	return "hello " + name
}
`
	writeFile(t, filepath.Join(dir, "main.go"), []byte(src))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Pattern that does NOT match anything in the file.
	entry := HookEntry{
		Name:    "no-error-returns",
		Point:   "pre-push",
		Grep:    "go::func $NAME() error",
		Action:  "block",
		Message: "Should not match",
	}

	err = runGrepHook(context.Background(), r, entry)
	if err != nil {
		t.Fatalf("expected no error when grep has no matches, got: %v", err)
	}
}

func TestGrepHook_DefaultActionIsBlock(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	src := `package main

func Hello(name string) string {
	return "hello " + name
}
`
	writeFile(t, filepath.Join(dir, "main.go"), []byte(src))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// No action specified — should default to "block".
	entry := HookEntry{
		Name:    "default-block",
		Point:   "pre-commit",
		Grep:    "go::func $NAME($$$PARAMS) string",
		Message: "Matched without explicit action",
	}

	err = runGrepHook(context.Background(), r, entry)
	if err == nil {
		t.Fatal("expected error when action is empty (default block) and matches exist")
	}
}

func TestGrepHook_LanguageFilterRestrictsFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create Go file and JS file, both with matching patterns.
	goSrc := `package main

func Hello(name string) string {
	return "hello " + name
}
`
	jsSrc := `function hello(name) {
	console.log("hello " + name);
}
`
	writeFile(t, filepath.Join(dir, "main.go"), []byte(goSrc))
	writeFile(t, filepath.Join(dir, "app.js"), []byte(jsSrc))
	if err := r.Add([]string{"main.go", "app.js"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Pattern targets JavaScript only via language prefix.
	entry := HookEntry{
		Name:    "no-console",
		Point:   "pre-push",
		Grep:    "javascript::console.log($$$ARGS)",
		Action:  "block",
		Message: "Remove console.log before pushing",
	}

	err = runGrepHook(context.Background(), r, entry)
	if err == nil {
		t.Fatal("expected grep hook to block on console.log match")
	}
	if !strings.Contains(err.Error(), "app.js") {
		t.Errorf("error should mention the JS file, got: %v", err)
	}
}

func TestGrepHook_IntegrationViaRunHookEntry(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	src := `package main

func Hello(name string) string {
	return "hello " + name
}
`
	writeFile(t, filepath.Join(dir, "main.go"), []byte(src))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entry := HookEntry{
		Name:    "integration-grep",
		Point:   "pre-push",
		Grep:    "go::func $NAME($$$PARAMS) string",
		Action:  "block",
		Message: "Blocked via RunHookEntry",
	}

	// RunHookEntry should detect the Grep field and dispatch to runGrepHook.
	err = RunHookEntry(context.Background(), r.RootDir, entry, nil)
	if err == nil {
		t.Fatal("expected RunHookEntry to fail for grep hook with matches")
	}
	if !strings.Contains(err.Error(), "Blocked via RunHookEntry") {
		t.Errorf("error should contain custom message, got: %v", err)
	}
}

func TestGrepHook_LoadFromHooksToml(t *testing.T) {
	dir := t.TempDir()

	tomlContent := `
[pre-push.no-unwrap]
grep = "go::func $NAME($$$PARAMS) string"
action = "block"
message = "Use typed returns instead of string"

[pre-push.warn-todo]
grep = "go::func $NAME($$$PARAMS)"
action = "warn"
message = "Review function signatures"
`
	if err := os.WriteFile(filepath.Join(dir, "hooks.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatalf("write hooks.toml: %v", err)
	}

	cfg, err := LoadHooksConfig(dir, nil)
	if err != nil {
		t.Fatalf("LoadHooksConfig: %v", err)
	}

	prePushHooks := cfg.ForPoint("pre-push")
	if len(prePushHooks) != 2 {
		t.Fatalf("expected 2 pre-push hooks, got %d", len(prePushHooks))
	}

	// Verify grep fields are parsed.
	for _, h := range prePushHooks {
		if h.Grep == "" {
			t.Errorf("hook %s.%s: Grep field is empty", h.Point, h.Name)
		}
		if h.Action == "" {
			t.Errorf("hook %s.%s: Action field is empty", h.Point, h.Name)
		}
		if h.Message == "" {
			t.Errorf("hook %s.%s: Message field is empty", h.Point, h.Name)
		}
	}

	// Verify specific hooks by name.
	var noUnwrap, warnTodo *HookEntry
	for i := range prePushHooks {
		switch prePushHooks[i].Name {
		case "no-unwrap":
			noUnwrap = &prePushHooks[i]
		case "warn-todo":
			warnTodo = &prePushHooks[i]
		}
	}

	if noUnwrap == nil {
		t.Fatal("no-unwrap hook not found")
	}
	if noUnwrap.Grep != "go::func $NAME($$$PARAMS) string" {
		t.Errorf("no-unwrap.Grep = %q, want %q", noUnwrap.Grep, "go::func $NAME($$$PARAMS) string")
	}
	if noUnwrap.Action != "block" {
		t.Errorf("no-unwrap.Action = %q, want %q", noUnwrap.Action, "block")
	}

	if warnTodo == nil {
		t.Fatal("warn-todo hook not found")
	}
	if warnTodo.Action != "warn" {
		t.Errorf("warn-todo.Action = %q, want %q", warnTodo.Action, "warn")
	}
}

func TestGrepHook_EmptyPatternErrors(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	entry := HookEntry{
		Name:   "empty",
		Point:  "pre-push",
		Grep:   "go::",
		Action: "block",
	}

	err = runGrepHook(context.Background(), r, entry)
	if err == nil {
		t.Fatal("expected error for empty grep pattern")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty pattern, got: %v", err)
	}
}

func TestGrepHook_RunHooksForPoint_BlockStopsChain(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	src := `package main

func Hello(name string) string {
	return "hello " + name
}
`
	writeFile(t, filepath.Join(dir, "main.go"), []byte(src))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	marker := filepath.Join(dir, "second-ran")
	scriptPath := writeScript(t, dir, "marker.sh", "#!/bin/sh\ntouch "+marker+"\n")

	hooks := []HookEntry{
		{
			Name:    "grep-block",
			Point:   "pre-push",
			Grep:    "go::func $NAME($$$PARAMS) string",
			Action:  "block",
			Message: "Blocked",
		},
		{
			Name:  "should-not-run",
			Point: "pre-push",
			Run:   scriptPath,
		},
	}

	err = RunHooksForPoint(context.Background(), r.RootDir, hooks, nil, true)
	if err == nil {
		t.Fatal("expected error from grep block hook")
	}

	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("second hook should not have run after grep block (canAbort=true)")
	}
}
