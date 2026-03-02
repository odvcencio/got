package repo

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseGraftModules_Basic(t *testing.T) {
	input := `[module "ui-lib"]
	url = https://orchard.example.com/team/ui-lib
	path = vendor/ui-lib
	track = main

[module "core"]
	url = git@github.com:org/core.git
	path = libs/core
	pin = v1.2.3
`

	modules, err := ParseGraftModules(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseGraftModules: %v", err)
	}
	if len(modules) != 2 {
		t.Fatalf("got %d modules, want 2", len(modules))
	}

	m0 := modules[0]
	if m0.Name != "ui-lib" {
		t.Errorf("modules[0].Name = %q, want %q", m0.Name, "ui-lib")
	}
	if m0.URL != "https://orchard.example.com/team/ui-lib" {
		t.Errorf("modules[0].URL = %q, want %q", m0.URL, "https://orchard.example.com/team/ui-lib")
	}
	if m0.Path != "vendor/ui-lib" {
		t.Errorf("modules[0].Path = %q, want %q", m0.Path, "vendor/ui-lib")
	}
	if m0.Track != "main" {
		t.Errorf("modules[0].Track = %q, want %q", m0.Track, "main")
	}
	if m0.Pin != "" {
		t.Errorf("modules[0].Pin = %q, want empty", m0.Pin)
	}

	m1 := modules[1]
	if m1.Name != "core" {
		t.Errorf("modules[1].Name = %q, want %q", m1.Name, "core")
	}
	if m1.URL != "git@github.com:org/core.git" {
		t.Errorf("modules[1].URL = %q, want %q", m1.URL, "git@github.com:org/core.git")
	}
	if m1.Path != "libs/core" {
		t.Errorf("modules[1].Path = %q, want %q", m1.Path, "libs/core")
	}
	if m1.Track != "" {
		t.Errorf("modules[1].Track = %q, want empty", m1.Track)
	}
	if m1.Pin != "v1.2.3" {
		t.Errorf("modules[1].Pin = %q, want %q", m1.Pin, "v1.2.3")
	}
}

func TestParseGraftModules_Empty(t *testing.T) {
	modules, err := ParseGraftModules(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ParseGraftModules: %v", err)
	}
	if len(modules) != 0 {
		t.Fatalf("got %d modules, want 0", len(modules))
	}
}

func TestParseGraftModules_TrackAndPinConflict(t *testing.T) {
	input := `[module "conflict"]
	url = https://example.com/repo
	path = libs/conflict
	track = main
	pin = v1.0.0
`

	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for track+pin conflict, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got: %v", err)
	}
}

func TestParseGraftModules_MissingURL(t *testing.T) {
	input := `[module "no-url"]
	path = libs/no-url
	track = main
`

	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for missing url, got nil")
	}
	if !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected 'url is required' error, got: %v", err)
	}
}

func TestParseGraftModules_MissingPath(t *testing.T) {
	input := `[module "no-path"]
	url = https://example.com/repo
	track = main
`

	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
	if !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("expected 'path is required' error, got: %v", err)
	}
}

func TestParseGraftModules_DuplicateName(t *testing.T) {
	input := `[module "dup"]
	url = https://example.com/a
	path = libs/a

[module "dup"]
	url = https://example.com/b
	path = libs/b
`

	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate module name") {
		t.Fatalf("expected 'duplicate module name' error, got: %v", err)
	}
}

func TestParseGraftModules_DuplicatePath(t *testing.T) {
	input := `[module "first"]
	url = https://example.com/a
	path = libs/shared

[module "second"]
	url = https://example.com/b
	path = libs/shared
`

	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for duplicate path, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate module path") {
		t.Fatalf("expected 'duplicate module path' error, got: %v", err)
	}
}

func TestWriteGraftModules(t *testing.T) {
	original := []ModuleEntry{
		{
			Name:  "ui-lib",
			URL:   "https://orchard.example.com/team/ui-lib",
			Path:  "vendor/ui-lib",
			Track: "main",
		},
		{
			Name: "core",
			URL:  "git@github.com:org/core.git",
			Path: "libs/core",
			Pin:  "v1.2.3",
		},
	}

	var buf bytes.Buffer
	if err := WriteGraftModules(&buf, original); err != nil {
		t.Fatalf("WriteGraftModules: %v", err)
	}

	// Round-trip: parse what we just wrote.
	parsed, err := ParseGraftModules(&buf)
	if err != nil {
		t.Fatalf("ParseGraftModules round-trip: %v", err)
	}
	if len(parsed) != len(original) {
		t.Fatalf("round-trip: got %d modules, want %d", len(parsed), len(original))
	}

	for i, want := range original {
		got := parsed[i]
		if got.Name != want.Name {
			t.Errorf("modules[%d].Name = %q, want %q", i, got.Name, want.Name)
		}
		if got.URL != want.URL {
			t.Errorf("modules[%d].URL = %q, want %q", i, got.URL, want.URL)
		}
		if got.Path != want.Path {
			t.Errorf("modules[%d].Path = %q, want %q", i, got.Path, want.Path)
		}
		if got.Track != want.Track {
			t.Errorf("modules[%d].Track = %q, want %q", i, got.Track, want.Track)
		}
		if got.Pin != want.Pin {
			t.Errorf("modules[%d].Pin = %q, want %q", i, got.Pin, want.Pin)
		}
	}
}

func TestParseGraftModules_CommentsAndWhitespace(t *testing.T) {
	input := `# This is a top-level comment
; Another comment

[module "commented"]
	# comment inside section
	url = https://example.com/repo   # inline comment
	; semicolon comment
	path = libs/commented   ; inline semicolon comment
	track = develop
`

	modules, err := ParseGraftModules(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseGraftModules: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("got %d modules, want 1", len(modules))
	}

	m := modules[0]
	if m.Name != "commented" {
		t.Errorf("Name = %q, want %q", m.Name, "commented")
	}
	if m.URL != "https://example.com/repo" {
		t.Errorf("URL = %q, want %q", m.URL, "https://example.com/repo")
	}
	if m.Path != "libs/commented" {
		t.Errorf("Path = %q, want %q", m.Path, "libs/commented")
	}
	if m.Track != "develop" {
		t.Errorf("Track = %q, want %q", m.Track, "develop")
	}
}

func TestRepoReadWriteGraftModulesFile(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Reading non-existent file returns nil, nil.
	modules, err := r.ReadGraftModulesFile()
	if err != nil {
		t.Fatalf("ReadGraftModulesFile (missing): %v", err)
	}
	if modules != nil {
		t.Fatalf("expected nil modules for missing file, got %d", len(modules))
	}

	// Write and read back.
	want := []ModuleEntry{
		{
			Name:  "auth",
			URL:   "https://orchard.example.com/team/auth",
			Path:  "vendor/auth",
			Track: "main",
		},
	}
	if err := r.WriteGraftModulesFile(want); err != nil {
		t.Fatalf("WriteGraftModulesFile: %v", err)
	}

	got, err := r.ReadGraftModulesFile()
	if err != nil {
		t.Fatalf("ReadGraftModulesFile: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d modules, want 1", len(got))
	}
	if got[0].Name != want[0].Name || got[0].URL != want[0].URL || got[0].Path != want[0].Path || got[0].Track != want[0].Track {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got[0], want[0])
	}
}

func TestParseGraftModules_UnknownKey(t *testing.T) {
	input := `[module "bad"]
	url = https://example.com/repo
	path = libs/bad
	flavor = chocolate
`

	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("expected 'unknown key' error, got: %v", err)
	}
}
