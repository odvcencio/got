package remote

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLFSBatchRequest_Upload(t *testing.T) {
	var received LFSBatchRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/lfs/objects/batch") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		ct := r.Header.Get("Content-Type")
		if ct != lfsMediaType {
			t.Fatalf("Content-Type = %q, want %q", ct, lfsMediaType)
		}

		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}

		resp := LFSBatchResponse{
			Objects: []LFSBatchResponseObject{
				{
					OID:  received.Objects[0].OID,
					Size: received.Objects[0].Size,
					Actions: map[string]LFSAction{
						"upload": {
							Href:   "https://storage.example.com/upload/abc123",
							Header: map[string]string{"X-Custom": "yes"},
						},
						"verify": {
							Href: "https://storage.example.com/verify/abc123",
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", lfsMediaType)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	lc := NewLFSClientFromURL(ts.URL+"/graft/alice/repo", "test-token")
	batchResp, err := lc.BatchRequest(t.Context(), LFSBatchRequest{
		Operation: "upload",
		Objects: []LFSBatchObject{
			{OID: strings.Repeat("a", 64), Size: 1024},
		},
	})
	if err != nil {
		t.Fatalf("BatchRequest: %v", err)
	}

	if received.Operation != "upload" {
		t.Fatalf("server received operation = %q, want %q", received.Operation, "upload")
	}
	if len(received.Objects) != 1 {
		t.Fatalf("server received %d objects, want 1", len(received.Objects))
	}
	if received.Objects[0].OID != strings.Repeat("a", 64) {
		t.Fatalf("OID mismatch")
	}

	if len(batchResp.Objects) != 1 {
		t.Fatalf("response has %d objects, want 1", len(batchResp.Objects))
	}
	obj := batchResp.Objects[0]
	upload, ok := obj.Actions["upload"]
	if !ok {
		t.Fatal("missing upload action")
	}
	if upload.Href != "https://storage.example.com/upload/abc123" {
		t.Fatalf("upload href = %q", upload.Href)
	}
	if upload.Header["X-Custom"] != "yes" {
		t.Fatalf("upload header X-Custom = %q", upload.Header["X-Custom"])
	}
}

func TestLFSBatchRequest_Download(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := LFSBatchResponse{
			Objects: []LFSBatchResponseObject{
				{
					OID:  strings.Repeat("b", 64),
					Size: 2048,
					Actions: map[string]LFSAction{
						"download": {
							Href: "https://storage.example.com/download/bbb",
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", lfsMediaType)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	lc := NewLFSClientFromURL(ts.URL+"/graft/alice/repo", "")
	batchResp, err := lc.BatchRequest(t.Context(), LFSBatchRequest{
		Operation: "download",
		Objects: []LFSBatchObject{
			{OID: strings.Repeat("b", 64), Size: 2048},
		},
	})
	if err != nil {
		t.Fatalf("BatchRequest: %v", err)
	}
	if len(batchResp.Objects) != 1 {
		t.Fatalf("response has %d objects, want 1", len(batchResp.Objects))
	}
	dl, ok := batchResp.Objects[0].Actions["download"]
	if !ok {
		t.Fatal("missing download action")
	}
	if dl.Href != "https://storage.example.com/download/bbb" {
		t.Fatalf("download href = %q", dl.Href)
	}
}

func TestLFSBatchRequest_EmptyObjects(t *testing.T) {
	lc := NewLFSClientFromURL("https://example.com/graft/alice/repo", "")
	resp, err := lc.BatchRequest(t.Context(), LFSBatchRequest{
		Operation: "upload",
		Objects:   nil,
	})
	if err != nil {
		t.Fatalf("BatchRequest: %v", err)
	}
	if len(resp.Objects) != 0 {
		t.Fatalf("expected 0 objects, got %d", len(resp.Objects))
	}
}

func TestLFSBatchRequest_InvalidOperation(t *testing.T) {
	lc := NewLFSClientFromURL("https://example.com/graft/alice/repo", "")
	_, err := lc.BatchRequest(t.Context(), LFSBatchRequest{
		Operation: "invalid",
		Objects:   []LFSBatchObject{{OID: "abc", Size: 1}},
	})
	if err == nil {
		t.Fatal("expected error for invalid operation")
	}
	if !strings.Contains(err.Error(), "invalid operation") {
		t.Fatalf("error = %v, expected invalid operation", err)
	}
}

func TestLFSBatchRequest_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal failure", http.StatusInternalServerError)
	}))
	defer ts.Close()

	lc := NewLFSClientFromURL(ts.URL+"/graft/alice/repo", "")
	_, err := lc.BatchRequest(t.Context(), LFSBatchRequest{
		Operation: "upload",
		Objects:   []LFSBatchObject{{OID: strings.Repeat("a", 64), Size: 100}},
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v, expected 500", err)
	}
}

func TestLFSBatchRequest_ObjectError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := LFSBatchResponse{
			Objects: []LFSBatchResponseObject{
				{
					OID:  strings.Repeat("c", 64),
					Size: 500,
					Error: &LFSObjectError{
						Code:    404,
						Message: "not found",
					},
				},
			},
		}
		w.Header().Set("Content-Type", lfsMediaType)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	lc := NewLFSClientFromURL(ts.URL+"/graft/alice/repo", "")
	batchResp, err := lc.BatchRequest(t.Context(), LFSBatchRequest{
		Operation: "download",
		Objects:   []LFSBatchObject{{OID: strings.Repeat("c", 64), Size: 500}},
	})
	if err != nil {
		t.Fatalf("BatchRequest: %v", err)
	}
	if len(batchResp.Objects) != 1 {
		t.Fatalf("response has %d objects, want 1", len(batchResp.Objects))
	}
	objErr := batchResp.Objects[0].Error
	if objErr == nil {
		t.Fatal("expected per-object error")
	}
	if objErr.Code != 404 {
		t.Fatalf("error code = %d, want 404", objErr.Code)
	}
}

func TestLFSUpload(t *testing.T) {
	content := []byte("binary content for upload test")
	var receivedBody []byte
	var receivedHeaders http.Header

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("expected PUT, got %s", r.Method)
		}
		receivedHeaders = r.Header
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	lc := NewLFSClientFromURL("https://example.com/graft/alice/repo", "my-token")
	action := LFSAction{
		Href:   ts.URL + "/upload/abc123",
		Header: map[string]string{"X-Upload-Token": "server-signed"},
	}

	err := lc.Upload(t.Context(), action, bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	if !bytes.Equal(receivedBody, content) {
		t.Fatalf("uploaded content mismatch: got %q, want %q", receivedBody, content)
	}
	if receivedHeaders.Get("X-Upload-Token") != "server-signed" {
		t.Fatalf("X-Upload-Token = %q, want %q", receivedHeaders.Get("X-Upload-Token"), "server-signed")
	}
	if receivedHeaders.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want %q", receivedHeaders.Get("Content-Type"), "application/octet-stream")
	}
}

func TestLFSUpload_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	lc := NewLFSClientFromURL("https://example.com/graft/alice/repo", "")
	err := lc.Upload(t.Context(), LFSAction{Href: ts.URL + "/upload"}, strings.NewReader("x"), 1)
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("error = %v, expected 403", err)
	}
}

func TestLFSUpload_EmptyHref(t *testing.T) {
	lc := NewLFSClientFromURL("https://example.com/graft/alice/repo", "")
	err := lc.Upload(t.Context(), LFSAction{Href: ""}, strings.NewReader("x"), 1)
	if err == nil {
		t.Fatal("expected error for empty href")
	}
}

func TestLFSDownload(t *testing.T) {
	content := []byte("downloaded binary content")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer ts.Close()

	lc := NewLFSClientFromURL("https://example.com/graft/alice/repo", "my-token")
	rc, err := lc.Download(t.Context(), LFSAction{Href: ts.URL + "/download/abc123"})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded content mismatch: got %q, want %q", got, content)
	}
}

func TestLFSDownload_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	lc := NewLFSClientFromURL("https://example.com/graft/alice/repo", "")
	_, err := lc.Download(t.Context(), LFSAction{Href: ts.URL + "/download"})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("error = %v, expected 404", err)
	}
}

func TestLFSDownload_EmptyHref(t *testing.T) {
	lc := NewLFSClientFromURL("https://example.com/graft/alice/repo", "")
	_, err := lc.Download(t.Context(), LFSAction{Href: ""})
	if err == nil {
		t.Fatal("expected error for empty href")
	}
}

func TestLFSBatchRequest_AuthHeader(t *testing.T) {
	var authHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", lfsMediaType)
		_ = json.NewEncoder(w).Encode(LFSBatchResponse{})
	}))
	defer ts.Close()

	lc := NewLFSClientFromURL(ts.URL+"/graft/alice/repo", "test-bearer-token")
	_, err := lc.BatchRequest(t.Context(), LFSBatchRequest{
		Operation: "upload",
		Objects:   []LFSBatchObject{{OID: strings.Repeat("a", 64), Size: 1}},
	})
	if err != nil {
		t.Fatalf("BatchRequest: %v", err)
	}
	if authHeader != "Bearer test-bearer-token" {
		t.Fatalf("Authorization = %q, want %q", authHeader, "Bearer test-bearer-token")
	}
}

func TestLFSUpload_ActionHeaderOverridesAuth(t *testing.T) {
	var authHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	lc := NewLFSClientFromURL("https://example.com/graft/alice/repo", "default-token")
	action := LFSAction{
		Href:   ts.URL + "/upload",
		Header: map[string]string{"Authorization": "Bearer server-specific-token"},
	}
	err := lc.Upload(t.Context(), action, strings.NewReader("data"), 4)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if authHeader != "Bearer server-specific-token" {
		t.Fatalf("Authorization = %q, want %q", authHeader, "Bearer server-specific-token")
	}
}
