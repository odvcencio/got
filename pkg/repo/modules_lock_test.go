package repo

import (
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestModuleLock_ReadWriteRoundTrip(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	lock := &ModuleLock{
		Modules: map[string]ModuleLockEntry{
			"libs/ui": {
				Commit: object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				URL:    "https://example.com/org/ui",
				Track:  "main",
			},
			"vendor/parser": {
				Commit: object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
				URL:    "https://example.com/org/parser",
				Pin:    "v2.1.0",
			},
		},
	}

	if err := r.WriteModuleLock(lock); err != nil {
		t.Fatalf("WriteModuleLock: %v", err)
	}

	got, err := r.ReadModuleLock()
	if err != nil {
		t.Fatalf("ReadModuleLock: %v", err)
	}
	if got == nil {
		t.Fatal("ReadModuleLock returned nil")
	}
	if len(got.Modules) != 2 {
		t.Fatalf("modules count = %d, want 2", len(got.Modules))
	}

	ui := got.Modules["libs/ui"]
	if ui.Commit != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("libs/ui commit = %q, want aaaa...", ui.Commit)
	}
	if ui.URL != "https://example.com/org/ui" {
		t.Errorf("libs/ui url = %q", ui.URL)
	}
	if ui.Track != "main" {
		t.Errorf("libs/ui track = %q, want %q", ui.Track, "main")
	}
	if ui.Pin != "" {
		t.Errorf("libs/ui pin = %q, want empty", ui.Pin)
	}

	parser := got.Modules["vendor/parser"]
	if parser.Commit != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("vendor/parser commit = %q, want bbbb...", parser.Commit)
	}
	if parser.URL != "https://example.com/org/parser" {
		t.Errorf("vendor/parser url = %q", parser.URL)
	}
	if parser.Track != "" {
		t.Errorf("vendor/parser track = %q, want empty", parser.Track)
	}
	if parser.Pin != "v2.1.0" {
		t.Errorf("vendor/parser pin = %q, want %q", parser.Pin, "v2.1.0")
	}
}

func TestModuleLock_NotExist(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.ReadModuleLock()
	if err != nil {
		t.Fatalf("ReadModuleLock: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for nonexistent lock file, got %+v", got)
	}
}
