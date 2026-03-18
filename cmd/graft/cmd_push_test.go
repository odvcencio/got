package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestResolvePushRefNames(t *testing.T) {
	r, err := repo.Init(t.TempDir())
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	tests := []struct {
		name       string
		branchArg  string
		wantLabel  string
		wantLocal  string
		wantRemote string
		wantErr    bool
	}{
		{
			name:       "short branch name",
			branchArg:  "main",
			wantLabel:  "branch main",
			wantLocal:  "refs/heads/main",
			wantRemote: "heads/main",
		},
		{
			name:       "full branch ref",
			branchArg:  "refs/heads/feature",
			wantLabel:  "branch feature",
			wantLocal:  "refs/heads/feature",
			wantRemote: "heads/feature",
		},
		{
			name:       "full tag ref",
			branchArg:  "refs/tags/v1.0.0",
			wantLabel:  "tag v1.0.0",
			wantLocal:  "refs/tags/v1.0.0",
			wantRemote: "tags/v1.0.0",
		},
		{
			name:      "unsupported ref namespace",
			branchArg: "refs/notes/release",
			wantErr:   true,
		},
		{
			name:       "infer from HEAD when empty",
			branchArg:  "",
			wantLabel:  "branch main",
			wantLocal:  "refs/heads/main",
			wantRemote: "heads/main",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			label, localRef, remoteRef, err := resolvePushRefNames(r, tc.branchArg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePushRefNames: %v", err)
			}
			if label != tc.wantLabel {
				t.Fatalf("label = %q, want %q", label, tc.wantLabel)
			}
			if localRef != tc.wantLocal {
				t.Fatalf("localRef = %q, want %q", localRef, tc.wantLocal)
			}
			if remoteRef != tc.wantRemote {
				t.Fatalf("remoteRef = %q, want %q", remoteRef, tc.wantRemote)
			}
		})
	}
}

func TestPushObjectsChunkedPrefersPackTransport(t *testing.T) {
	var packRequests, ndjsonRequests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graft/alice/repo/objects" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Header.Get("Content-Type") {
		case "application/x-graft-pack":
			packRequests++
			if r.Header.Get("Content-Encoding") != "zstd" {
				t.Fatalf("Content-Encoding = %q, want zstd", r.Header.Get("Content-Encoding"))
			}
		case "application/x-ndjson":
			ndjsonRequests++
		default:
			t.Fatalf("unexpected Content-Type %q", r.Header.Get("Content-Type"))
		}
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"received":1}`))
	}))
	defer ts.Close()

	client, err := remote.NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	uploaded, err := pushObjectsChunked(context.Background(), client, []remote.ObjectRecord{
		{Hash: object.HashObject(object.TypeBlob, blobData), Type: object.TypeBlob, Data: blobData},
	})
	if err != nil {
		t.Fatalf("pushObjectsChunked: %v", err)
	}
	if uploaded != 1 {
		t.Fatalf("uploaded = %d, want 1", uploaded)
	}
	if packRequests != 1 {
		t.Fatalf("packRequests = %d, want 1", packRequests)
	}
	if ndjsonRequests != 0 {
		t.Fatalf("ndjsonRequests = %d, want 0", ndjsonRequests)
	}
}

func TestPushObjectsChunkedFallsBackWhenPackUnsupported(t *testing.T) {
	var packRequests, ndjsonRequests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graft/alice/repo/objects" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Header.Get("Content-Type") {
		case "application/x-graft-pack":
			packRequests++
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		case "application/x-ndjson":
			ndjsonRequests++
			_, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"received":1}`))
			return
		default:
			t.Fatalf("unexpected Content-Type %q", r.Header.Get("Content-Type"))
		}
	}))
	defer ts.Close()

	client, err := remote.NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	uploaded, err := pushObjectsChunked(context.Background(), client, []remote.ObjectRecord{
		{Hash: object.HashObject(object.TypeBlob, blobData), Type: object.TypeBlob, Data: blobData},
	})
	if err != nil {
		t.Fatalf("pushObjectsChunked: %v", err)
	}
	if uploaded != 1 {
		t.Fatalf("uploaded = %d, want 1", uploaded)
	}
	if packRequests != 1 {
		t.Fatalf("packRequests = %d, want 1", packRequests)
	}
	if ndjsonRequests != 1 {
		t.Fatalf("ndjsonRequests = %d, want 1", ndjsonRequests)
	}
}

func TestPushCmdCheckRejectsOversizedObject(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	large := bytes.Repeat([]byte("a"), pushObjectByteLimit+1)
	if err := os.WriteFile(filepath.Join(dir, "large.bin"), large, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := r.Add([]string{"large.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("large", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/refs") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"refs":{}}`))
	}))
	defer ts.Close()

	if err := r.SetRemote("origin", ts.URL+"/graft/alice/repo"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newPushCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--check"})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected push --check to fail for an oversized object")
	}
	if !strings.Contains(err.Error(), "push limit check failed") {
		t.Fatalf("error = %q, want push limit failure", err.Error())
	}
	if !strings.Contains(err.Error(), "16.0 MiB") {
		t.Fatalf("error = %q, want formatted object limit", err.Error())
	}
}
