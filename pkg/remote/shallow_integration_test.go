package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestFetchConfigWithDepthSendsCorrectJSON(t *testing.T) {
	remoteStore := object.NewStore(t.TempDir())

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

	commitType, commitData, _ := remoteStore.Read(commitHash)
	treeType, treeData, _ := remoteStore.Read(treeHash)
	blobType, blobData, _ := remoteStore.Read(blobHash)

	var capturedDepth int
	var capturedShallow []string
	var capturedDeepen int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/graft/alice/repo/objects/batch":
			var req struct {
				Wants   []string `json:"wants"`
				Haves   []string `json:"haves"`
				Depth   int      `json:"depth"`
				Deepen  int      `json:"deepen"`
				Shallow []string `json:"shallow"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			capturedDepth = req.Depth
			capturedShallow = req.Shallow
			capturedDeepen = req.Deepen

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"objects": []map[string]any{
					{"hash": string(commitHash), "type": string(commitType), "data": commitData},
					{"hash": string(treeHash), "type": string(treeType), "data": treeData},
					{"hash": string(blobHash), "type": string(blobType), "data": blobData},
				},
				"truncated": false,
				"shallow":   []string{string(commitHash)},
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	localStore := object.NewStore(t.TempDir())

	existingShallow := NewShallowState()
	existingShallow.Add(hashA)

	cfg := FetchConfig{
		Depth:        3,
		Deepen:       2,
		ShallowState: existingShallow,
	}

	result, err := FetchIntoStoreShallow(context.Background(), client, localStore, []object.Hash{commitHash}, nil, cfg)
	if err != nil {
		t.Fatalf("FetchIntoStoreShallow: %v", err)
	}

	if capturedDepth != 3 {
		t.Errorf("expected depth=3 in request, got %d", capturedDepth)
	}
	if capturedDeepen != 2 {
		t.Errorf("expected deepen=2 in request, got %d", capturedDeepen)
	}
	if len(capturedShallow) != 1 || capturedShallow[0] != string(hashA) {
		t.Errorf("expected shallow=[%s] in request, got %v", hashA, capturedShallow)
	}

	// Verify shallow boundaries from response are captured.
	if result.ShallowState == nil {
		t.Fatal("expected non-nil ShallowState in result")
	}
	if !result.ShallowState.IsShallow(commitHash) {
		t.Errorf("expected commitHash %s in result shallow state", commitHash)
	}
	// Existing shallow boundary should also be preserved.
	if !result.ShallowState.IsShallow(hashA) {
		t.Errorf("expected existing boundary %s in result shallow state", hashA)
	}
}

func TestShallowBoundaryParsingFromJSONResponse(t *testing.T) {
	remoteStore := object.NewStore(t.TempDir())

	blobHash, err := remoteStore.WriteBlob(&object.Blob{Data: []byte("data\n")})
	if err != nil {
		t.Fatal(err)
	}
	blobType, blobData, _ := remoteStore.Read(blobHash)

	boundaryHash := object.Hash(strings.Repeat("d", 64))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/graft/alice/repo/objects/batch":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"objects": []map[string]any{
					{"hash": string(blobHash), "type": string(blobType), "data": blobData},
				},
				"truncated": false,
				"shallow":   []string{string(boundaryHash)},
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.BatchObjectsPackShallow(
		context.Background(),
		[]object.Hash{blobHash},
		nil,
		100,
		&ShallowFetchOpts{Depth: 1},
	)
	if err != nil {
		t.Fatalf("BatchObjectsPackShallow: %v", err)
	}

	if len(result.Objects) != 1 {
		t.Errorf("expected 1 object, got %d", len(result.Objects))
	}
	if len(result.Shallow) != 1 || result.Shallow[0] != boundaryHash {
		t.Errorf("expected shallow=[%s], got %v", boundaryHash, result.Shallow)
	}
}

func TestShallowBoundaryParsingFromHeader(t *testing.T) {
	remoteStore := object.NewStore(t.TempDir())

	blobHash, err := remoteStore.WriteBlob(&object.Blob{Data: []byte("data\n")})
	if err != nil {
		t.Fatal(err)
	}
	blobType, blobData, _ := remoteStore.Read(blobHash)

	boundaryHash := object.Hash(strings.Repeat("e", 64))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/graft/alice/repo/objects/batch":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Shallow", string(boundaryHash))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"objects": []map[string]any{
					{"hash": string(blobHash), "type": string(blobType), "data": blobData},
				},
				"truncated": false,
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.BatchObjectsPackShallow(
		context.Background(),
		[]object.Hash{blobHash},
		nil,
		100,
		&ShallowFetchOpts{Depth: 1},
	)
	if err != nil {
		t.Fatalf("BatchObjectsPackShallow: %v", err)
	}

	if len(result.Shallow) != 1 || result.Shallow[0] != boundaryHash {
		t.Errorf("expected shallow=[%s] from header, got %v", boundaryHash, result.Shallow)
	}
}

func TestFetchShallowClosureStopsAtBoundary(t *testing.T) {
	remoteStore := object.NewStore(t.TempDir())

	// Create a chain: parentCommit -> childCommit
	blobHash, err := remoteStore.WriteBlob(&object.Blob{Data: []byte("file\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := remoteStore.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "file.txt", BlobHash: blobHash}}})
	if err != nil {
		t.Fatal(err)
	}
	parentCommitHash, err := remoteStore.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice",
		Timestamp: 1700000000,
		Message:   "parent",
	})
	if err != nil {
		t.Fatal(err)
	}
	childCommitHash, err := remoteStore.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{parentCommitHash},
		Author:    "Alice",
		Timestamp: 1700000001,
		Message:   "child",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Only serve the child commit, tree, and blob — not the parent commit.
	childType, childData, _ := remoteStore.Read(childCommitHash)
	treeType, treeData, _ := remoteStore.Read(treeHash)
	blobType, blobData, _ := remoteStore.Read(blobHash)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/graft/alice/repo/objects/batch":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"objects": []map[string]any{
					{"hash": string(childCommitHash), "type": string(childType), "data": childData},
					{"hash": string(treeHash), "type": string(treeType), "data": treeData},
					{"hash": string(blobHash), "type": string(blobType), "data": blobData},
				},
				"truncated": false,
				"shallow":   []string{string(parentCommitHash)},
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	localStore := object.NewStore(t.TempDir())

	cfg := FetchConfig{Depth: 1}
	result, err := FetchIntoStoreShallow(context.Background(), client, localStore, []object.Hash{childCommitHash}, nil, cfg)
	if err != nil {
		t.Fatalf("FetchIntoStoreShallow: %v", err)
	}

	// Should have child commit, tree, and blob — but NOT the parent commit.
	if !localStore.Has(childCommitHash) {
		t.Error("expected child commit in local store")
	}
	if !localStore.Has(treeHash) {
		t.Error("expected tree in local store")
	}
	if !localStore.Has(blobHash) {
		t.Error("expected blob in local store")
	}
	if localStore.Has(parentCommitHash) {
		t.Error("parent commit should not be in local store (shallow boundary)")
	}

	// Result shallow state should include the parent.
	if result.ShallowState == nil || !result.ShallowState.IsShallow(parentCommitHash) {
		t.Errorf("expected parent %s in shallow state", parentCommitHash)
	}
}
