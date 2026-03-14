package gitbridge

import (
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestHashMapPutAndLookup(t *testing.T) {
	dir := t.TempDir()
	hm, err := OpenHashMap(filepath.Join(dir, "hashmap"))
	if err != nil {
		t.Fatal(err)
	}
	defer hm.Close()

	graftHash := object.HashBytes([]byte("hello"))
	gitHash := GitHash{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a,
		0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}

	if err := hm.Put(graftHash, gitHash); err != nil {
		t.Fatal(err)
	}

	got, ok := hm.GraftToGit(graftHash)
	if !ok {
		t.Fatal("expected to find graft→git mapping")
	}
	if string(got) != string(gitHash) {
		t.Errorf("got %x, want %x", got, gitHash)
	}

	got2, ok := hm.GitToGraft(gitHash)
	if !ok {
		t.Fatal("expected to find git→graft mapping")
	}
	if got2 != graftHash {
		t.Errorf("got %s, want %s", got2, graftHash)
	}
}

func TestHashMapPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hashmap")

	graftHash := object.HashBytes([]byte("persist"))
	gitHash := GitHash{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00, 0x11, 0x22, 0x33,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd}

	hm, err := OpenHashMap(path)
	if err != nil {
		t.Fatal(err)
	}
	hm.Put(graftHash, gitHash)
	hm.Close()

	hm2, err := OpenHashMap(path)
	if err != nil {
		t.Fatal(err)
	}
	defer hm2.Close()

	got, ok := hm2.GraftToGit(graftHash)
	if !ok {
		t.Fatal("expected mapping to persist across close/open")
	}
	if string(got) != string(gitHash) {
		t.Errorf("got %x, want %x", got, gitHash)
	}
}

func TestHashMapNotFound(t *testing.T) {
	dir := t.TempDir()
	hm, err := OpenHashMap(filepath.Join(dir, "hashmap"))
	if err != nil {
		t.Fatal(err)
	}
	defer hm.Close()

	_, ok := hm.GraftToGit("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent hash")
	}
}
