package remote

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// Test hashes — valid 64-char lowercase hex strings.
var (
	hashA = object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	hashB = object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	hashC = object.Hash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
)

func TestShallow_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	state := NewShallowState()
	state.Add(hashA)
	state.Add(hashC)
	state.Add(hashB)

	if err := WriteShallowFile(dir, state); err != nil {
		t.Fatalf("WriteShallowFile: %v", err)
	}

	got, err := ReadShallowFile(dir)
	if err != nil {
		t.Fatalf("ReadShallowFile: %v", err)
	}

	if got.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", got.Len())
	}
	for _, h := range []object.Hash{hashA, hashB, hashC} {
		if !got.IsShallow(h) {
			t.Errorf("expected %s to be shallow", h)
		}
	}
}

func TestShallow_IsShallow(t *testing.T) {
	state := NewShallowState()
	state.Add(hashA)
	state.Add(hashB)

	if !state.IsShallow(hashA) {
		t.Error("hashA should be shallow")
	}
	if !state.IsShallow(hashB) {
		t.Error("hashB should be shallow")
	}
	if state.IsShallow(hashC) {
		t.Error("hashC should not be shallow")
	}
}

func TestShallow_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	// Do not create any shallow file — ReadShallowFile should return empty state.
	state, err := ReadShallowFile(dir)
	if err != nil {
		t.Fatalf("ReadShallowFile on missing file: %v", err)
	}
	if state.Len() != 0 {
		t.Fatalf("expected empty state, got %d entries", state.Len())
	}
}

func TestShallow_List(t *testing.T) {
	state := NewShallowState()
	state.Add(hashC)
	state.Add(hashA)
	state.Add(hashB)

	list := state.List()
	if len(list) != 3 {
		t.Fatalf("List() len = %d, want 3", len(list))
	}
	// Verify sorted order: hashA < hashB < hashC
	if list[0] != hashA {
		t.Errorf("list[0] = %s, want %s", list[0], hashA)
	}
	if list[1] != hashB {
		t.Errorf("list[1] = %s, want %s", list[1], hashB)
	}
	if list[2] != hashC {
		t.Errorf("list[2] = %s, want %s", list[2], hashC)
	}
}

func TestShallow_Remove(t *testing.T) {
	state := NewShallowState()
	state.Add(hashA)
	state.Add(hashB)
	state.Remove(hashA)

	if state.IsShallow(hashA) {
		t.Error("hashA should have been removed")
	}
	if !state.IsShallow(hashB) {
		t.Error("hashB should still be present")
	}
	if state.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", state.Len())
	}
}

func TestShallow_WriteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	state := NewShallowState()
	state.Add(hashA)

	if err := WriteShallowFile(dir, state); err != nil {
		t.Fatalf("WriteShallowFile: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "shallow"))
	if err != nil {
		t.Fatalf("reading shallow file: %v", err)
	}
	expected := string(hashA) + "\n"
	if string(data) != expected {
		t.Fatalf("file content = %q, want %q", string(data), expected)
	}
}

func TestShallow_WriteEmptyState(t *testing.T) {
	dir := t.TempDir()
	state := NewShallowState()

	if err := WriteShallowFile(dir, state); err != nil {
		t.Fatalf("WriteShallowFile: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "shallow"))
	if err != nil {
		t.Fatalf("reading shallow file: %v", err)
	}
	if string(data) != "" {
		t.Fatalf("file content = %q, want empty", string(data))
	}
}

func TestProtocol_NewCapabilities(t *testing.T) {
	// Verify the capability constants have the expected values.
	if CapPack != "pack" {
		t.Fatalf("CapPack = %q, want %q", CapPack, "pack")
	}
	if CapZstd != "zstd" {
		t.Fatalf("CapZstd = %q, want %q", CapZstd, "zstd")
	}
	if CapSideband != "sideband" {
		t.Fatalf("CapSideband = %q, want %q", CapSideband, "sideband")
	}
	if CapShallow != "shallow" {
		t.Fatalf("CapShallow = %q, want %q", CapShallow, "shallow")
	}
	if CapFilter != "filter" {
		t.Fatalf("CapFilter = %q, want %q", CapFilter, "filter")
	}
	if CapIncludeTag != "include-tag" {
		t.Fatalf("CapIncludeTag = %q, want %q", CapIncludeTag, "include-tag")
	}

	// Test Add and Len methods.
	caps := ParseCapabilities("pack,zstd")
	if caps.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", caps.Len())
	}

	caps.Add("shallow")
	if caps.Len() != 3 {
		t.Fatalf("Len() after Add = %d, want 3", caps.Len())
	}
	if !caps.Has("shallow") {
		t.Fatal("expected shallow capability after Add")
	}

	// Adding a duplicate should not increase length.
	caps.Add("pack")
	if caps.Len() != 3 {
		t.Fatalf("Len() after duplicate Add = %d, want 3", caps.Len())
	}
}

func TestObjectFilter_Parse(t *testing.T) {
	tests := []struct {
		spec      string
		wantType  string
		wantLimit int64
		wantDepth int
		wantStr   string
		wantErr   bool
	}{
		{
			spec:     "blob:none",
			wantType: "blob:none",
			wantStr:  "blob:none",
		},
		{
			spec:      "blob:limit=1048576",
			wantType:  "blob:limit",
			wantLimit: 1048576,
			wantStr:   "blob:limit=1048576",
		},
		{
			spec:      "blob:limit=0",
			wantType:  "blob:limit",
			wantLimit: 0,
			wantStr:   "blob:limit=0",
		},
		{
			spec:      "tree:0",
			wantType:  "tree",
			wantDepth: 0,
			wantStr:   "tree:0",
		},
		{
			spec:      "tree:3",
			wantType:  "tree",
			wantDepth: 3,
			wantStr:   "tree:3",
		},
		{
			spec:    "",
			wantErr: true,
		},
		{
			spec:    "unknown:foo",
			wantErr: true,
		},
		{
			spec:    "blob:limit=-1",
			wantErr: true,
		},
		{
			spec:    "tree:-5",
			wantErr: true,
		},
		{
			spec:    "blob:limit=abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			f, err := ParseObjectFilter(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for spec %q", tt.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseObjectFilter(%q): %v", tt.spec, err)
			}
			if f.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", f.Type, tt.wantType)
			}
			if f.BlobLimit != tt.wantLimit {
				t.Errorf("BlobLimit = %d, want %d", f.BlobLimit, tt.wantLimit)
			}
			if f.TreeDepth != tt.wantDepth {
				t.Errorf("TreeDepth = %d, want %d", f.TreeDepth, tt.wantDepth)
			}
			if f.String() != tt.wantStr {
				t.Errorf("String() = %q, want %q", f.String(), tt.wantStr)
			}
		})
	}
}

func TestObjectFilter_AllowsBlob(t *testing.T) {
	// blob:none — no blobs pass
	none, _ := ParseObjectFilter("blob:none")
	if none.AllowsBlob(0) {
		t.Error("blob:none should not allow zero-size blob")
	}
	if none.AllowsBlob(100) {
		t.Error("blob:none should not allow any blob")
	}

	// blob:limit=1024 — only blobs strictly under 1024 pass
	limit, _ := ParseObjectFilter("blob:limit=1024")
	if !limit.AllowsBlob(0) {
		t.Error("blob:limit=1024 should allow size 0")
	}
	if !limit.AllowsBlob(1023) {
		t.Error("blob:limit=1024 should allow size 1023")
	}
	if limit.AllowsBlob(1024) {
		t.Error("blob:limit=1024 should not allow size 1024 (at limit)")
	}
	if limit.AllowsBlob(2000) {
		t.Error("blob:limit=1024 should not allow size 2000")
	}

	// tree:0 — all blobs pass
	tree, _ := ParseObjectFilter("tree:0")
	if !tree.AllowsBlob(0) {
		t.Error("tree filter should allow all blobs (size 0)")
	}
	if !tree.AllowsBlob(999999) {
		t.Error("tree filter should allow all blobs (large size)")
	}
}
