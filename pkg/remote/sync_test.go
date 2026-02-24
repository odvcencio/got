package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestFetchIntoStoreBatchThenGetFallback(t *testing.T) {
	remoteRoot := t.TempDir()
	remoteStore := object.NewStore(remoteRoot)

	blobHash, err := remoteStore.WriteBlob(&object.Blob{Data: []byte("hello\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := remoteStore.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "README.md", BlobHash: blobHash}}})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := remoteStore.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "init",
	})
	if err != nil {
		t.Fatal(err)
	}

	commitType, commitData, err := remoteStore.Read(commitHash)
	if err != nil {
		t.Fatal(err)
	}
	treeType, treeData, err := remoteStore.Read(treeHash)
	if err != nil {
		t.Fatal(err)
	}
	blobType, blobData, err := remoteStore.Read(blobHash)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/got/alice/repo/objects/batch":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"objects": []map[string]any{
					{"hash": string(commitHash), "type": string(commitType), "data": commitData},
					{"hash": string(treeHash), "type": string(treeType), "data": treeData},
				},
				"truncated": true,
			})
			return
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/got/alice/repo/objects/"):
			h := object.Hash(path.Base(r.URL.Path))
			if h != blobHash {
				http.Error(w, "object not found", http.StatusNotFound)
				return
			}
			w.Header().Set("X-Object-Type", string(blobType))
			_, _ = w.Write(blobData)
			return
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/got/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	localStore := object.NewStore(t.TempDir())

	written, err := FetchIntoStore(context.Background(), client, localStore, []object.Hash{commitHash}, nil)
	if err != nil {
		t.Fatalf("FetchIntoStore: %v", err)
	}
	if written != 3 {
		t.Fatalf("written = %d, want 3", written)
	}

	for _, h := range []object.Hash{commitHash, treeHash, blobHash} {
		if !localStore.Has(h) {
			t.Fatalf("missing expected object %s", h)
		}
	}
}

func TestFetchIntoStoreUsesMultipleBatchRounds(t *testing.T) {
	remoteRoot := t.TempDir()
	remoteStore := object.NewStore(remoteRoot)

	blobHash, err := remoteStore.WriteBlob(&object.Blob{Data: []byte("hello\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := remoteStore.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "README.md", BlobHash: blobHash}}})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := remoteStore.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "init",
	})
	if err != nil {
		t.Fatal(err)
	}

	commitType, commitData, err := remoteStore.Read(commitHash)
	if err != nil {
		t.Fatal(err)
	}
	treeType, treeData, err := remoteStore.Read(treeHash)
	if err != nil {
		t.Fatal(err)
	}
	blobType, blobData, err := remoteStore.Read(blobHash)
	if err != nil {
		t.Fatal(err)
	}

	batchCalls := 0
	getCalls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/got/alice/repo/objects/batch":
			batchCalls++
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			var req struct {
				Haves []string `json:"haves"`
			}
			_ = json.Unmarshal(body, &req)
			haveSet := make(map[string]struct{}, len(req.Haves))
			for _, h := range req.Haves {
				haveSet[strings.TrimSpace(h)] = struct{}{}
			}

			type obj struct {
				Hash string `json:"hash"`
				Type string `json:"type"`
				Data []byte `json:"data"`
			}
			resp := struct {
				Objects   []obj `json:"objects"`
				Truncated bool  `json:"truncated"`
			}{}

			_, hasCommit := haveSet[string(commitHash)]
			_, hasTree := haveSet[string(treeHash)]
			_, hasBlob := haveSet[string(blobHash)]

			switch {
			case !hasCommit:
				resp.Objects = append(resp.Objects, obj{Hash: string(commitHash), Type: string(commitType), Data: commitData})
				resp.Truncated = true
			case !hasTree:
				resp.Objects = append(resp.Objects, obj{Hash: string(treeHash), Type: string(treeType), Data: treeData})
				resp.Truncated = true
			case !hasBlob:
				resp.Objects = append(resp.Objects, obj{Hash: string(blobHash), Type: string(blobType), Data: blobData})
				resp.Truncated = false
			default:
				resp.Truncated = false
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/got/alice/repo/objects/"):
			getCalls++
			http.Error(w, "unexpected get", http.StatusInternalServerError)
			return
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/got/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	localStore := object.NewStore(t.TempDir())

	written, err := FetchIntoStore(context.Background(), client, localStore, []object.Hash{commitHash}, nil)
	if err != nil {
		t.Fatalf("FetchIntoStore: %v", err)
	}
	if written != 3 {
		t.Fatalf("written = %d, want 3", written)
	}
	if batchCalls < 3 {
		t.Fatalf("expected at least 3 batch rounds, got %d", batchCalls)
	}
	if getCalls != 0 {
		t.Fatalf("expected 0 GET fallback calls, got %d", getCalls)
	}
	for _, h := range []object.Hash{commitHash, treeHash, blobHash} {
		if !localStore.Has(h) {
			t.Fatalf("missing expected object %s", h)
		}
	}
}

func TestFetchIntoStoreRejectsHashMismatch(t *testing.T) {
	obj := &object.Blob{Data: []byte("data")}
	blobData := object.MarshalBlob(obj)
	blobHash := object.HashObject(object.TypeBlob, blobData)
	badHash := object.Hash(strings.Repeat("a", 64))
	if badHash == blobHash {
		t.Fatalf("test setup produced equal hashes")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/got/alice/repo/objects/batch" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"objects": []map[string]any{
					{"hash": string(badHash), "type": string(object.TypeBlob), "data": blobData},
				},
				"truncated": false,
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/got/alice/repo")
	if err != nil {
		t.Fatal(err)
	}

	localStore := object.NewStore(t.TempDir())
	_, err = FetchIntoStore(context.Background(), client, localStore, []object.Hash{blobHash}, nil)
	if err == nil {
		t.Fatalf("expected hash mismatch error")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch error, got %v", err)
	}
}

func TestCollectObjectsForPushStopsAtReachableRoots(t *testing.T) {
	store := object.NewStore(t.TempDir())

	blobA, err := store.WriteBlob(&object.Blob{Data: []byte("v1\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeA, err := store.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "main.txt", BlobHash: blobA}}})
	if err != nil {
		t.Fatal(err)
	}
	commitA, err := store.WriteCommit(&object.CommitObj{
		TreeHash:  treeA,
		Author:    "Alice",
		Timestamp: 1700000000,
		Message:   "A",
	})
	if err != nil {
		t.Fatal(err)
	}

	blobB, err := store.WriteBlob(&object.Blob{Data: []byte("v2\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeB, err := store.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "main.txt", BlobHash: blobB}}})
	if err != nil {
		t.Fatal(err)
	}
	commitB, err := store.WriteCommit(&object.CommitObj{
		TreeHash:  treeB,
		Parents:   []object.Hash{commitA},
		Author:    "Alice",
		Timestamp: 1700000001,
		Message:   "B",
	})
	if err != nil {
		t.Fatal(err)
	}

	objs, err := CollectObjectsForPush(store, []object.Hash{commitB}, []object.Hash{commitA})
	if err != nil {
		t.Fatalf("CollectObjectsForPush: %v", err)
	}

	got := make(map[object.Hash]struct{}, len(objs))
	for _, o := range objs {
		got[o.Hash] = struct{}{}
	}
	for _, h := range []object.Hash{commitB, treeB, blobB} {
		if _, ok := got[h]; !ok {
			t.Fatalf("missing expected object %s", h)
		}
	}
	for _, h := range []object.Hash{commitA, treeA, blobA} {
		if _, ok := got[h]; ok {
			t.Fatalf("unexpected object from stop root history: %s", h)
		}
	}
}

func TestCollectObjectsForPushTraversesTagTargets(t *testing.T) {
	store := object.NewStore(t.TempDir())

	blobHash, err := store.WriteBlob(&object.Blob{Data: []byte("hello\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "README.md", BlobHash: blobHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
	commitHash, err := store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice",
		Timestamp: 1700000000,
		Message:   "init",
	})
	if err != nil {
		t.Fatal(err)
	}
	tagHash, err := store.WriteTag(&object.TagObj{
		TargetHash: commitHash,
		Data: []byte("object " + string(commitHash) + "\n" +
			"type commit\n" +
			"tag v1.0.0\n\nrelease\n"),
	})
	if err != nil {
		t.Fatal(err)
	}

	objs, err := CollectObjectsForPush(store, []object.Hash{tagHash}, nil)
	if err != nil {
		t.Fatalf("CollectObjectsForPush: %v", err)
	}
	got := make(map[object.Hash]struct{}, len(objs))
	for _, obj := range objs {
		got[obj.Hash] = struct{}{}
	}
	for _, want := range []object.Hash{tagHash, commitHash, treeHash, blobHash} {
		if _, ok := got[want]; !ok {
			t.Fatalf("expected object %s in traversal", want)
		}
	}
}

func TestReachableSetIgnoresMissingRoots(t *testing.T) {
	store := object.NewStore(t.TempDir())
	blobHash, err := store.WriteBlob(&object.Blob{Data: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	set, err := ReachableSet(store, []object.Hash{blobHash, object.Hash(strings.Repeat("f", 64))})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := set[blobHash]; !ok {
		t.Fatalf("expected reachable set to include %s", blobHash)
	}
	if len(set) != 1 {
		t.Fatalf("reachable set len = %d, want 1", len(set))
	}
}

func TestUniqueHashes(t *testing.T) {
	in := []object.Hash{"", "a", "b", "a", "  c  ", "b"}
	got := uniqueHashes(in)
	want := []object.Hash{"a", "b", "c"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("uniqueHashes = %v, want %v", got, want)
	}
}
