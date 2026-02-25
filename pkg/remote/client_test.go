package remote

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestParseObjectTypeAcceptsTag(t *testing.T) {
	got, err := parseObjectType("tag")
	if err != nil {
		t.Fatalf("parseObjectType(tag): %v", err)
	}
	if got != object.TypeTag {
		t.Fatalf("parseObjectType(tag) = %q, want %q", got, object.TypeTag)
	}
}

func TestNewClientWithOptionsTimeout(t *testing.T) {
	client, err := NewClientWithOptions("https://example.com/got/alice/repo", ClientOptions{
		Timeout:     120 * time.Second,
		MaxAttempts: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.Timeout != 120*time.Second {
		t.Fatalf("timeout = %v, want 120s", client.httpClient.Timeout)
	}
	if client.maxAttempts != 5 {
		t.Fatalf("maxAttempts = %d, want 5", client.maxAttempts)
	}
}

func TestNewClientWithOptionsDefaults(t *testing.T) {
	client, err := NewClientWithOptions("https://example.com/got/alice/repo", ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.Timeout != 60*time.Second {
		t.Fatalf("timeout = %v, want 60s", client.httpClient.Timeout)
	}
	if client.maxAttempts != 3 {
		t.Fatalf("maxAttempts = %d, want 3", client.maxAttempts)
	}
}

func TestDoRejectsWrongContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>error</html>"))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/got/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListRefs(t.Context())
	if err == nil {
		t.Fatal("expected content-type error")
	}
	if !strings.Contains(err.Error(), "text/html") {
		t.Fatalf("expected content-type in error, got: %v", err)
	}
}

func TestListRefsRejectsInvalidHash(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"heads/main": "not-a-hash",
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/got/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListRefs(t.Context())
	if err == nil {
		t.Fatal("expected hash validation error")
	}
}

func TestClientSendsCapabilityHeaders(t *testing.T) {
	var gotProtocol, gotCaps string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProtocol = r.Header.Get("Got-Protocol")
		gotCaps = r.Header.Get("Got-Capabilities")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/got/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.ListRefs(t.Context())

	if gotProtocol != ProtocolVersion {
		t.Fatalf("Got-Protocol = %q, want %q", gotProtocol, ProtocolVersion)
	}
	if gotCaps == "" {
		t.Fatal("Got-Capabilities header missing")
	}
}

func TestListRefsHandlesLargeResponse(t *testing.T) {
	refs := make(map[string]string)
	for i := 0; i < 1000; i++ {
		refs[fmt.Sprintf("heads/branch-%04d", i)] = strings.Repeat("a", 64)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(refs)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/got/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.ListRefs(t.Context())
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(got) != 1000 {
		t.Fatalf("got %d refs, want 1000", len(got))
	}
}
