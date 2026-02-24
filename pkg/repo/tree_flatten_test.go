package repo

import (
	"fmt"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestFlattenTree_PathJoinSemantics(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	dotTreeHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "child.txt",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(1),
			},
		},
	})
	if err != nil {
		t.Fatalf("write dot tree: %v", err)
	}

	uncleanTreeHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "child.txt",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(2),
			},
		},
	})
	if err != nil {
		t.Fatalf("write unclean tree: %v", err)
	}

	normalTreeHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "..",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(3),
			},
			{
				Name:     "leaf.txt",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(4),
			},
		},
	})
	if err != nil {
		t.Fatalf("write normal tree: %v", err)
	}

	rootHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "./root.txt",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(5),
			},
			{
				Name:        ".",
				IsDir:       true,
				Mode:        object.TreeModeDir,
				SubtreeHash: dotTreeHash,
			},
			{
				Name:        "a//b",
				IsDir:       true,
				Mode:        object.TreeModeDir,
				SubtreeHash: uncleanTreeHash,
			},
			{
				Name:        "normal",
				IsDir:       true,
				Mode:        object.TreeModeDir,
				SubtreeHash: normalTreeHash,
			},
		},
	})
	if err != nil {
		t.Fatalf("write root tree: %v", err)
	}

	entries, err := r.FlattenTree(rootHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	want := map[string]object.Hash{
		"./root.txt":      testTreeHash(5),
		"child.txt":       testTreeHash(1),
		"a/b/child.txt":   testTreeHash(2),
		".":               testTreeHash(3),
		"normal/leaf.txt": testTreeHash(4),
	}
	if len(entries) != len(want) {
		t.Fatalf("FlattenTree returned %d entries, want %d", len(entries), len(want))
	}

	for _, e := range entries {
		wantHash, ok := want[e.Path]
		if !ok {
			t.Fatalf("unexpected path %q", e.Path)
		}
		if e.BlobHash != wantHash {
			t.Fatalf("BlobHash at %q = %q, want %q", e.Path, e.BlobHash, wantHash)
		}
	}
}

func testTreeHash(seed int) object.Hash {
	return object.Hash(fmt.Sprintf("%064x", seed))
}
