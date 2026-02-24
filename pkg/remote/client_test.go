package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantBase   string
		wantOwner  string
		wantRepo   string
		shouldFail bool
	}{
		{
			name:      "canonical got path",
			in:        "https://example.com/got/alice/proj",
			wantBase:  "https://example.com/got/alice/proj",
			wantOwner: "alice",
			wantRepo:  "proj",
		},
		{
			name:      "plain owner repo path",
			in:        "https://example.com/alice/proj",
			wantBase:  "https://example.com/got/alice/proj",
			wantOwner: "alice",
			wantRepo:  "proj",
		},
		{
			name:      "api prefix with got path",
			in:        "https://example.com/api/v1/got/alice/proj",
			wantBase:  "https://example.com/api/v1/got/alice/proj",
			wantOwner: "alice",
			wantRepo:  "proj",
		},
		{
			name:       "invalid",
			in:         "alice/proj",
			shouldFail: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := ParseEndpoint(tc.in)
			if tc.shouldFail {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEndpoint: %v", err)
			}
			if ep.BaseURL != tc.wantBase {
				t.Fatalf("BaseURL = %q, want %q", ep.BaseURL, tc.wantBase)
			}
			if ep.Owner != tc.wantOwner {
				t.Fatalf("Owner = %q, want %q", ep.Owner, tc.wantOwner)
			}
			if ep.Repo != tc.wantRepo {
				t.Fatalf("Repo = %q, want %q", ep.Repo, tc.wantRepo)
			}
		})
	}
}

func TestPushObjectsIncludesComputedHash(t *testing.T) {
	var received int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/got/alice/repo/objects" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		type pushedObject struct {
			Hash string `json:"hash"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		}
		dec := json.NewDecoder(r.Body)
		for {
			var obj pushedObject
			if err := dec.Decode(&obj); err != nil {
				break
			}
			received++
			objType := object.ObjectType(obj.Type)
			computed := object.HashObject(objType, obj.Data)
			if obj.Hash != string(computed) {
				t.Fatalf("expected pushed hash %s, got %s", computed, obj.Hash)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"received":1}`))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/got/alice/repo")
	if err != nil {
		t.Fatal(err)
	}

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	err = client.PushObjects(t.Context(), []ObjectRecord{
		{Type: object.TypeBlob, Data: blobData},
	})
	if err != nil {
		t.Fatalf("PushObjects: %v", err)
	}
	if received != 1 {
		t.Fatalf("expected 1 pushed object, got %d", received)
	}
}

func TestPushObjectsRejectsProvidedHashMismatch(t *testing.T) {
	var requests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/got/alice/repo")
	if err != nil {
		t.Fatal(err)
	}

	blobData := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	err = client.PushObjects(t.Context(), []ObjectRecord{
		{
			Hash: object.Hash(strings.Repeat("a", 64)),
			Type: object.TypeBlob,
			Data: blobData,
		},
	})
	if err == nil {
		t.Fatal("expected hash mismatch error")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch error, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("expected no HTTP requests on local hash mismatch, got %d", requests)
	}
}
