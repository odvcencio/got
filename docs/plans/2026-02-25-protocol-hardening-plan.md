# Graft Protocol Hardening Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make graft's wire protocol production-grade with bulletproof transport, pack-compressed transfers with zstd and delta encoding, sideband framing for progress/errors, and smart negotiation with server-advertised limits.

**Architecture:** Four layers applied bottom-up. Layer 1 hardens the HTTP transport (retry, timeouts, buffer limits, content-type checks). Layer 2 adds protocol validation (hash format, structured errors, capability headers). Layer 3 replaces JSON object payloads with pack-on-wire format, zstd compression, and sideband framing. Layer 4 adds server-advertised limits, bisect-style negotiation, and ref pagination. Changes span both graft (client at `pkg/remote/`) and orchard (server at `internal/gotprotocol/`).

**Tech Stack:** Go 1.25, `github.com/klauspost/compress/zstd`, existing `pkg/object` pack reader/writer

---

## Layer 1: Transport Robustness

### Task 1: Add retry with exponential backoff

**Files:**
- Create: `/home/draco/work/graft/pkg/remote/retry.go`
- Create: `/home/draco/work/graft/pkg/remote/retry_test.go`

**Step 1: Write the tests**

```go
package remote

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRetryDoSucceedsFirstAttempt(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	resp, err := retryDo(client, req, 3)
	if err != nil {
		t.Fatalf("retryDo: %v", err)
	}
	defer resp.Body.Close()
	if calls != 1 {
		t.Fatalf("expected 1 call, graft %d", calls)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRetryDoRetriesOn500(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	resp, err := retryDo(client, req, 3)
	if err != nil {
		t.Fatalf("retryDo: %v", err)
	}
	defer resp.Body.Close()
	if calls != 3 {
		t.Fatalf("expected 3 calls, graft %d", calls)
	}
}

func TestRetryDoRetriesOn429(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	resp, err := retryDo(client, req, 3)
	if err != nil {
		t.Fatalf("retryDo: %v", err)
	}
	defer resp.Body.Close()
	if calls != 2 {
		t.Fatalf("expected 2 calls, graft %d", calls)
	}
}

func TestRetryDoDoesNotRetry4xx(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	resp, err := retryDo(client, req, 3)
	if err != nil {
		t.Fatalf("retryDo: %v", err)
	}
	defer resp.Body.Close()
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry on 4xx), graft %d", calls)
	}
}

func TestRetryDoReplaysPostBody(t *testing.T) {
	calls := 0
	var bodies []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if calls < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader("payload"))
	resp, err := retryDo(client, req, 3)
	if err != nil {
		t.Fatalf("retryDo: %v", err)
	}
	defer resp.Body.Close()
	if calls != 2 {
		t.Fatalf("expected 2 calls, graft %d", calls)
	}
	for i, b := range bodies {
		if b != "payload" {
			t.Fatalf("call %d body = %q, want %q", i, b, "payload")
		}
	}
}

func TestRetryDoExhaustsRetries(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	resp, err := retryDo(client, req, 3)
	if err != nil {
		t.Fatalf("retryDo: %v", err)
	}
	defer resp.Body.Close()
	if calls != 3 {
		t.Fatalf("expected 3 calls (exhausted retries), graft %d", calls)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("final status = %d, want 500", resp.StatusCode)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run TestRetryDo -v`
Expected: FAIL — `retryDo` undefined

**Step 3: Write the implementation**

```go
package remote

import (
	"bytes"
	"io"
	"net/http"
	"time"
)

// retryDo executes an HTTP request with exponential backoff retry.
// Retries on network errors, HTTP 429, and HTTP 5xx responses.
// Does not retry 4xx client errors.
// For requests with a body, the body is buffered and replayed on retry.
func retryDo(client *http.Client, req *http.Request, maxAttempts int) (*http.Response, error) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	// Buffer body for replay on retry.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
	}

	var lastResp *http.Response
	var lastErr error
	backoff := time.Second

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}

		// Reset body for each attempt.
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			req.ContentLength = int64(len(bodyBytes))
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			lastResp = nil
			continue
		}

		// Don't retry client errors (4xx) except 429.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		// Don't retry success.
		if resp.StatusCode < 400 {
			return resp, nil
		}

		// Retryable: 429 or 5xx. Drain and close body before retry.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		lastResp = resp
		lastErr = nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return lastResp, nil
}

// isRetryableStatus returns true for HTTP status codes that should be retried.
func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run TestRetryDo -v`
Expected: PASS (all 6 tests)

**Step 5: Commit**

Run: `cd /home/draco/work/graft && git add pkg/remote/retry.go pkg/remote/retry_test.go && buckley commit --yes --minimal-output`

---

### Task 2: Configurable client options and integrate retry

**Files:**
- Modify: `/home/draco/work/graft/pkg/remote/client.go`
- Modify: `/home/draco/work/graft/pkg/remote/client_test.go`

**Step 1: Write the test**

Add to `client_test.go`:

```go
func TestNewClientWithOptionsTimeout(t *testing.T) {
	client, err := NewClientWithOptions("https://example.com/graft/alice/repo", ClientOptions{
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
	client, err := NewClientWithOptions("https://example.com/graft/alice/repo", ClientOptions{})
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
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run TestNewClientWithOptions -v`
Expected: FAIL — `NewClientWithOptions` undefined

**Step 3: Implement**

Modify `client.go`:

1. Add `ClientOptions` struct and `maxAttempts` field to `Client`:

```go
// ClientOptions configures the remote protocol client.
type ClientOptions struct {
	Timeout     time.Duration // HTTP client timeout (default 60s)
	MaxAttempts int           // retry attempts (default 3)
}

type Client struct {
	endpoint    Endpoint
	httpClient  *http.Client
	token       string
	user        string
	pass        string
	maxAttempts int
}
```

2. Add `NewClientWithOptions`:

```go
func NewClientWithOptions(remoteURL string, opts ClientOptions) (*Client, error) {
	endpoint, err := ParseEndpoint(remoteURL)
	if err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	token := strings.TrimSpace(os.Getenv("GRAFT_TOKEN"))
	user := strings.TrimSpace(os.Getenv("GRAFT_USERNAME"))
	pass := os.Getenv("GRAFT_PASSWORD")
	if token == "" && user == "" && endpoint.user != "" {
		user = endpoint.user
		pass = endpoint.pass
	}

	return &Client{
		endpoint:    endpoint,
		httpClient:  &http.Client{Timeout: timeout},
		token:       token,
		user:        user,
		pass:        pass,
		maxAttempts: maxAttempts,
	}, nil
}
```

3. Update `NewClient` to call `NewClientWithOptions`:

```go
func NewClient(remoteURL string) (*Client, error) {
	return NewClientWithOptions(remoteURL, ClientOptions{})
}
```

4. Update `do()` method to use `retryDo`:

```go
func (c *Client) do(req *http.Request, expectedStatus int) ([]byte, error) {
	c.applyAuth(req)
	resp, err := retryDo(c.httpClient, req, c.maxAttempts)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// ... rest unchanged
}
```

5. Update `GetObject()` to also use `retryDo` instead of `c.httpClient.Do(req)`:

```go
// In GetObject, replace:
//   resp, err := c.httpClient.Do(req)
// With:
	resp, err := retryDo(c.httpClient, req, c.maxAttempts)
```

**Step 4: Run all remote tests**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -v`
Expected: PASS (all existing + new tests)

**Step 5: Commit**

Run: `cd /home/draco/work/graft && git add pkg/remote/client.go pkg/remote/client_test.go && buckley commit --yes --minimal-output`

---

### Task 3: Fix response buffer limits and add content-type validation

**Files:**
- Modify: `/home/draco/work/graft/pkg/remote/client.go`
- Modify: `/home/draco/work/graft/pkg/remote/client_test.go`

**Step 1: Write the tests**

Add to `client_test.go`:

```go
func TestDoRejectsWrongContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>error</html>"))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListRefs(t.Context())
	if err == nil {
		t.Fatal("expected content-type error")
	}
	if !strings.Contains(err.Error(), "text/html") {
		t.Fatalf("expected content-type in error, graft: %v", err)
	}
}

func TestListRefsHandlesLargeResponse(t *testing.T) {
	// Build a response with many refs (up to 8MB limit).
	refs := make(map[string]string)
	for i := 0; i < 1000; i++ {
		refs[fmt.Sprintf("heads/branch-%04d", i)] = strings.Repeat("a", 64)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(refs)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	graft, err := client.ListRefs(t.Context())
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(graft) != 1000 {
		t.Fatalf("graft %d refs, want 1000", len(graft))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run "TestDoRejects|TestListRefsHandles" -v`
Expected: FAIL

**Step 3: Implement**

In `client.go`, update the `do()` method:

1. Add per-endpoint response limit by adding a `responseLimit` parameter:

```go
// Response limits per endpoint type.
const (
	responseLimitDefault = 2 << 20  // 2MB
	responseLimitRefs    = 8 << 20  // 8MB
	responseLimitBatch   = 64 << 20 // 64MB
	responseLimitObject  = 32 << 20 // 32MB
)

func (c *Client) doWithLimit(req *http.Request, expectedStatus int, maxBytes int64, expectedContentType string) ([]byte, error) {
	c.applyAuth(req)
	resp, err := retryDo(c.httpClient, req, c.maxAttempts)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode != expectedStatus {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("remote request failed (%s %s): %s", req.Method, req.URL.Path, msg)
	}

	// Validate content type if expected.
	if expectedContentType != "" {
		ct := resp.Header.Get("Content-Type")
		if ct != "" && !strings.HasPrefix(ct, expectedContentType) {
			return nil, fmt.Errorf("unexpected content type %q (expected %s) from %s %s (status %d)",
				ct, expectedContentType, req.Method, req.URL.Path, resp.StatusCode)
		}
	}

	return body, nil
}

// Keep do() as backward-compatible wrapper.
func (c *Client) do(req *http.Request, expectedStatus int) ([]byte, error) {
	return c.doWithLimit(req, expectedStatus, responseLimitDefault, "")
}
```

2. Update `ListRefs` to use `doWithLimit` with 8MB limit and JSON content-type:

```go
body, err := c.doWithLimit(req, http.StatusOK, responseLimitRefs, "application/json")
```

3. Update `BatchObjects` to use 64MB limit:

```go
body, err := c.doWithLimit(req, http.StatusOK, responseLimitBatch, "application/json")
```

4. Update `PushObjects` to use 1MB limit for ack response:

```go
if _, err := c.doWithLimit(req, http.StatusOK, 1<<20, "application/json"); err != nil {
```

5. Update `UpdateRefs` to use 1MB limit:

```go
body, err := c.doWithLimit(req, http.StatusOK, 1<<20, "application/json")
```

6. Update `GetObject` to use content-type validation too (check for `application/octet-stream` but don't reject if missing — some servers omit it).

**Step 4: Run all tests**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -v`
Expected: PASS

**Step 5: Commit**

Run: `cd /home/draco/work/graft && git add pkg/remote/client.go pkg/remote/client_test.go && buckley commit --yes --minimal-output`

---

## Layer 2: Protocol Validation & Capabilities

### Task 4: Hash format validation

**Files:**
- Create: `/home/draco/work/graft/pkg/remote/protocol.go`
- Create: `/home/draco/work/graft/pkg/remote/protocol_test.go`

**Step 1: Write the tests**

```go
package remote

import (
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestValidateHashValid(t *testing.T) {
	// 64 hex chars (SHA-256).
	valid := object.Hash("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if err := ValidateHash(valid); err != nil {
		t.Fatalf("valid hash rejected: %v", err)
	}
}

func TestValidateHashEmpty(t *testing.T) {
	if err := ValidateHash(""); err == nil {
		t.Fatal("empty hash accepted")
	}
}

func TestValidateHashWrongLength(t *testing.T) {
	if err := ValidateHash("abc123"); err == nil {
		t.Fatal("short hash accepted")
	}
}

func TestValidateHashNonHex(t *testing.T) {
	bad := object.Hash("g1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if err := ValidateHash(bad); err == nil {
		t.Fatal("non-hex hash accepted")
	}
}

func TestValidateHashWhitespace(t *testing.T) {
	if err := ValidateHash("  "); err == nil {
		t.Fatal("whitespace-only hash accepted")
	}
}

func TestParseCapabilities(t *testing.T) {
	caps := ParseCapabilities("pack,zstd,sideband")
	if !caps.Has("pack") {
		t.Fatal("missing pack capability")
	}
	if !caps.Has("zstd") {
		t.Fatal("missing zstd capability")
	}
	if !caps.Has("sideband") {
		t.Fatal("missing sideband capability")
	}
	if caps.Has("nonexistent") {
		t.Fatal("unexpected capability")
	}
}

func TestCapabilitiesIntersect(t *testing.T) {
	a := ParseCapabilities("pack,zstd,sideband")
	b := ParseCapabilities("pack,zstd")
	common := a.Intersect(b)
	if !common.Has("pack") || !common.Has("zstd") {
		t.Fatal("missing intersected capability")
	}
	if common.Has("sideband") {
		t.Fatal("sideband should not be in intersection")
	}
}

func TestCapabilitiesString(t *testing.T) {
	caps := ParseCapabilities("zstd,pack,sideband")
	s := caps.String()
	// Should be sorted.
	if s != "pack,sideband,zstd" {
		t.Fatalf("String() = %q, want %q", s, "pack,sideband,zstd")
	}
}

func TestRemoteErrorFormat(t *testing.T) {
	re := &RemoteError{Code: "ref_not_found", Message: "ref not found", Detail: "heads/main"}
	if re.Error() != "ref not found (ref_not_found): heads/main" {
		t.Fatalf("Error() = %q", re.Error())
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run "TestValidateHash|TestParseCapabilities|TestCapabilities|TestRemoteError" -v`
Expected: FAIL

**Step 3: Implement `protocol.go`**

```go
package remote

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

const (
	// ProtocolVersion is the current Graft protocol version.
	ProtocolVersion = "1"

	// ClientCapabilities lists all capabilities this client supports.
	ClientCapabilities = "pack,zstd,sideband"

	headerProtocol     = "Graft-Protocol"
	headerCapabilities = "Graft-Capabilities"
	headerLimits       = "Graft-Limits"
)

// ValidateHash checks that a hash is a valid 64-character lowercase hex string (SHA-256).
func ValidateHash(h object.Hash) error {
	s := strings.TrimSpace(string(h))
	if s == "" {
		return fmt.Errorf("hash is empty")
	}
	if len(s) != 64 {
		return fmt.Errorf("hash length %d, expected 64", len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		return fmt.Errorf("hash contains non-hex characters: %w", err)
	}
	return nil
}

// Capabilities represents a set of protocol capabilities.
type Capabilities struct {
	set map[string]struct{}
}

// ParseCapabilities parses a comma-separated capability string.
func ParseCapabilities(raw string) Capabilities {
	caps := Capabilities{set: make(map[string]struct{})}
	for _, cap := range strings.Split(raw, ",") {
		cap = strings.TrimSpace(cap)
		if cap != "" {
			caps.set[cap] = struct{}{}
		}
	}
	return caps
}

// Has returns true if the capability is present.
func (c Capabilities) Has(name string) bool {
	_, ok := c.set[name]
	return ok
}

// Intersect returns capabilities present in both sets.
func (c Capabilities) Intersect(other Capabilities) Capabilities {
	result := Capabilities{set: make(map[string]struct{})}
	for k := range c.set {
		if _, ok := other.set[k]; ok {
			result.set[k] = struct{}{}
		}
	}
	return result
}

// String returns a sorted comma-separated capability string.
func (c Capabilities) String() string {
	names := make([]string, 0, len(c.set))
	for k := range c.set {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// RemoteError is a structured error from the remote server.
type RemoteError struct {
	Code    string `json:"code"`
	Message string `json:"error"`
	Detail  string `json:"detail,omitempty"`
}

func (e *RemoteError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s (%s): %s", e.Message, e.Code, e.Detail)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

// tryParseRemoteError attempts to parse a JSON error response body.
// Returns nil if the body is not a valid structured error.
func tryParseRemoteError(body []byte) *RemoteError {
	var re RemoteError
	if err := json.Unmarshal(body, &re); err != nil {
		return nil
	}
	if re.Message == "" && re.Code == "" {
		return nil
	}
	return &re
}
```

**Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run "TestValidateHash|TestParseCapabilities|TestCapabilities|TestRemoteError" -v`
Expected: PASS

**Step 5: Commit**

Run: `cd /home/draco/work/graft && git add pkg/remote/protocol.go pkg/remote/protocol_test.go && buckley commit --yes --minimal-output`

---

### Task 5: Integrate hash validation and capability headers into client

**Files:**
- Modify: `/home/draco/work/graft/pkg/remote/client.go`
- Modify: `/home/draco/work/graft/pkg/remote/client_test.go`

**Step 1: Write the tests**

Add to `client_test.go`:

```go
func TestListRefsRejectsInvalidHash(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"heads/main": "not-a-hash",
		})
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
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
		gotProtocol = r.Header.Get("Graft-Protocol")
		gotCaps = r.Header.Get("Graft-Capabilities")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL + "/graft/alice/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.ListRefs(t.Context())

	if gotProtocol != ProtocolVersion {
		t.Fatalf("Graft-Protocol = %q, want %q", gotProtocol, ProtocolVersion)
	}
	if gotCaps == "" {
		t.Fatal("Graft-Capabilities header missing")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run "TestListRefsRejects|TestClientSends" -v`
Expected: FAIL

**Step 3: Implement**

1. Add protocol headers to `applyAuth`:

```go
func (c *Client) applyAuth(req *http.Request) {
	req.Header.Set(headerProtocol, ProtocolVersion)
	req.Header.Set(headerCapabilities, ClientCapabilities)

	if strings.TrimSpace(c.token) != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		return
	}
	if strings.TrimSpace(c.user) != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
}
```

2. Add hash validation to `ListRefs` after parsing:

```go
for name, hash := range raw {
	name = strings.TrimSpace(name)
	if name == "" {
		continue
	}
	h := object.Hash(strings.TrimSpace(hash))
	if err := ValidateHash(h); err != nil {
		return nil, fmt.Errorf("invalid hash for ref %q: %w", name, err)
	}
	refs[name] = h
}
```

3. Add hash validation to `BatchObjects` after parsing each object hash.

4. Update `doWithLimit` to parse structured errors:

```go
if resp.StatusCode != expectedStatus {
	msg := strings.TrimSpace(string(body))
	if re := tryParseRemoteError(body); re != nil {
		return nil, re
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return nil, fmt.Errorf("remote request failed (%s %s): %s", req.Method, req.URL.Path, msg)
}
```

**Step 4: Run all tests**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -v`
Expected: PASS

**Step 5: Commit**

Run: `cd /home/draco/work/graft && git add pkg/remote/client.go pkg/remote/client_test.go && buckley commit --yes --minimal-output`

---

## Layer 3: Pack Wire Format + Zstd + Sideband

### Task 6: Add zstd dependency and compression wrapper

**Files:**
- Modify: `/home/draco/work/graft/go.mod`
- Create: `/home/draco/work/graft/pkg/remote/compress.go`
- Create: `/home/draco/work/graft/pkg/remote/compress_test.go`

**Step 1: Add dependency**

Run: `cd /home/draco/work/graft && go get github.com/klauspost/compress/zstd && go mod tidy`

**Step 2: Write tests**

```go
package remote

import (
	"bytes"
	"testing"
)

func TestZstdRoundTrip(t *testing.T) {
	original := []byte("hello world, this is a test of zstd compression in the graft protocol")
	compressed, err := compressZstd(original)
	if err != nil {
		t.Fatalf("compressZstd: %v", err)
	}
	if len(compressed) >= len(original) {
		// Compression should help on repeated content.
		t.Logf("warning: compressed %d >= original %d", len(compressed), len(original))
	}

	decompressed, err := decompressZstd(compressed)
	if err != nil {
		t.Fatalf("decompressZstd: %v", err)
	}
	if !bytes.Equal(decompressed, original) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestZstdStreamRoundTrip(t *testing.T) {
	original := bytes.Repeat([]byte("graft protocol compression test data\n"), 100)
	var compressed bytes.Buffer
	if err := compressZstdStream(&compressed, bytes.NewReader(original)); err != nil {
		t.Fatalf("compressZstdStream: %v", err)
	}

	var decompressed bytes.Buffer
	if err := decompressZstdStream(&decompressed, &compressed); err != nil {
		t.Fatalf("decompressZstdStream: %v", err)
	}
	if !bytes.Equal(decompressed.Bytes(), original) {
		t.Fatalf("stream round-trip mismatch: graft %d bytes, want %d", decompressed.Len(), len(original))
	}
}

func TestZstdEmptyInput(t *testing.T) {
	compressed, err := compressZstd(nil)
	if err != nil {
		t.Fatalf("compressZstd(nil): %v", err)
	}
	decompressed, err := decompressZstd(compressed)
	if err != nil {
		t.Fatalf("decompressZstd: %v", err)
	}
	if len(decompressed) != 0 {
		t.Fatalf("expected empty, graft %d bytes", len(decompressed))
	}
}
```

**Step 3: Implement `compress.go`**

```go
package remote

import (
	"bytes"
	"io"

	"github.com/klauspost/compress/zstd"
)

// compressZstd compresses data using zstd.
func compressZstd(data []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	return enc.EncodeAll(data, nil), nil
}

// decompressZstd decompresses zstd-compressed data.
func decompressZstd(data []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return dec.DecodeAll(data, nil)
}

// compressZstdStream compresses from src to dst using streaming zstd.
func compressZstdStream(dst io.Writer, src io.Reader) error {
	enc, err := zstd.NewWriter(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(enc, src); err != nil {
		enc.Close()
		return err
	}
	return enc.Close()
}

// decompressZstdStream decompresses from src to dst using streaming zstd.
func decompressZstdStream(dst io.Writer, src io.Reader) error {
	dec, err := zstd.NewReader(src)
	if err != nil {
		return err
	}
	defer dec.Close()
	_, err = io.Copy(dst, dec)
	return err
}

// newZstdReader wraps an io.Reader with zstd decompression.
func newZstdReader(r io.Reader) (io.ReadCloser, error) {
	dec, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &zstdReadCloser{dec: dec}, nil
}

type zstdReadCloser struct {
	dec *zstd.Decoder
}

func (z *zstdReadCloser) Read(p []byte) (int, error) {
	return z.dec.Read(p)
}

func (z *zstdReadCloser) Close() error {
	z.dec.Close()
	return nil
}

// isZstdEncoded checks if the response has zstd content encoding.
func isZstdEncoded(contentEncoding string) bool {
	return bytes.Contains([]byte(contentEncoding), []byte("zstd"))
}
```

**Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run TestZstd -v`
Expected: PASS

**Step 5: Commit**

Run: `cd /home/draco/work/graft && git add go.mod go.sum pkg/remote/compress.go pkg/remote/compress_test.go && buckley commit --yes --minimal-output`

---

### Task 7: Sideband framing reader/writer

**Files:**
- Create: `/home/draco/work/graft/pkg/remote/sideband.go`
- Create: `/home/draco/work/graft/pkg/remote/sideband_test.go`

**Step 1: Write the tests**

```go
package remote

import (
	"bytes"
	"io"
	"testing"
)

func TestSidebandRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSidebandWriter(&buf)

	if err := sw.WriteData([]byte("pack-data-1")); err != nil {
		t.Fatalf("WriteData: %v", err)
	}
	if err := sw.WriteProgress("50%"); err != nil {
		t.Fatalf("WriteProgress: %v", err)
	}
	if err := sw.WriteData([]byte("pack-data-2")); err != nil {
		t.Fatalf("WriteData: %v", err)
	}

	sr := NewSidebandReader(&buf)
	var dataFrames [][]byte
	var progressFrames []string

	for {
		channel, payload, err := sr.ReadFrame()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		switch channel {
		case SidebandData:
			dataFrames = append(dataFrames, append([]byte{}, payload...))
		case SidebandProgress:
			progressFrames = append(progressFrames, string(payload))
		}
	}

	if len(dataFrames) != 2 {
		t.Fatalf("data frames: %d, want 2", len(dataFrames))
	}
	if string(dataFrames[0]) != "pack-data-1" {
		t.Fatalf("data[0] = %q", dataFrames[0])
	}
	if string(dataFrames[1]) != "pack-data-2" {
		t.Fatalf("data[1] = %q", dataFrames[1])
	}
	if len(progressFrames) != 1 || progressFrames[0] != "50%" {
		t.Fatalf("progress = %v", progressFrames)
	}
}

func TestSidebandErrorFrame(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSidebandWriter(&buf)
	if err := sw.WriteError("disk full"); err != nil {
		t.Fatalf("WriteError: %v", err)
	}

	sr := NewSidebandReader(&buf)
	channel, payload, err := sr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if channel != SidebandError {
		t.Fatalf("channel = %d, want SidebandError", channel)
	}
	if string(payload) != "disk full" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestSidebandEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSidebandWriter(&buf)
	if err := sw.WriteData(nil); err != nil {
		t.Fatalf("WriteData(nil): %v", err)
	}
	sr := NewSidebandReader(&buf)
	ch, payload, err := sr.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if ch != SidebandData || len(payload) != 0 {
		t.Fatalf("unexpected frame: channel=%d, len=%d", ch, len(payload))
	}
}

func TestSidebandDataReader(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSidebandWriter(&buf)
	_ = sw.WriteData([]byte("hello"))
	_ = sw.WriteProgress("working...")
	_ = sw.WriteData([]byte(" world"))

	dr := NewSidebandDataReader(&buf, nil)
	all, err := io.ReadAll(dr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(all) != "hello world" {
		t.Fatalf("data = %q, want %q", all, "hello world")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run TestSideband -v`
Expected: FAIL

**Step 3: Implement `sideband.go`**

```go
package remote

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Sideband channel identifiers.
const (
	SidebandData     byte = 0x01
	SidebandProgress byte = 0x02
	SidebandError    byte = 0x03
)

// SidebandWriter writes length-prefixed sideband frames.
// Frame format: [4 bytes big-endian length][1 byte channel][payload]
type SidebandWriter struct {
	w io.Writer
}

func NewSidebandWriter(w io.Writer) *SidebandWriter {
	return &SidebandWriter{w: w}
}

func (sw *SidebandWriter) writeFrame(channel byte, data []byte) error {
	frameLen := uint32(1 + len(data)) // channel + payload
	if err := binary.Write(sw.w, binary.BigEndian, frameLen); err != nil {
		return fmt.Errorf("write frame length: %w", err)
	}
	if _, err := sw.w.Write([]byte{channel}); err != nil {
		return fmt.Errorf("write channel: %w", err)
	}
	if len(data) > 0 {
		if _, err := sw.w.Write(data); err != nil {
			return fmt.Errorf("write payload: %w", err)
		}
	}
	return nil
}

func (sw *SidebandWriter) WriteData(data []byte) error {
	return sw.writeFrame(SidebandData, data)
}

func (sw *SidebandWriter) WriteProgress(msg string) error {
	return sw.writeFrame(SidebandProgress, []byte(msg))
}

func (sw *SidebandWriter) WriteError(msg string) error {
	return sw.writeFrame(SidebandError, []byte(msg))
}

// SidebandReader reads length-prefixed sideband frames.
type SidebandReader struct {
	r io.Reader
}

func NewSidebandReader(r io.Reader) *SidebandReader {
	return &SidebandReader{r: r}
}

// ReadFrame reads one sideband frame, returning channel and payload.
// Returns io.EOF when no more frames are available.
func (sr *SidebandReader) ReadFrame() (byte, []byte, error) {
	var frameLen uint32
	if err := binary.Read(sr.r, binary.BigEndian, &frameLen); err != nil {
		return 0, nil, err
	}
	if frameLen < 1 {
		return 0, nil, fmt.Errorf("sideband frame too short: %d", frameLen)
	}

	frame := make([]byte, frameLen)
	if _, err := io.ReadFull(sr.r, frame); err != nil {
		return 0, nil, fmt.Errorf("read frame: %w", err)
	}

	channel := frame[0]
	payload := frame[1:]
	return channel, payload, nil
}

// SidebandDataReader presents sideband data frames as a sequential io.Reader,
// discarding progress frames (or forwarding them to a callback).
type SidebandDataReader struct {
	sr       *SidebandReader
	onProgress func(string)
	buf      []byte
	done     bool
}

func NewSidebandDataReader(r io.Reader, onProgress func(string)) *SidebandDataReader {
	return &SidebandDataReader{
		sr:         NewSidebandReader(r),
		onProgress: onProgress,
	}
}

func (dr *SidebandDataReader) Read(p []byte) (int, error) {
	for len(dr.buf) == 0 {
		if dr.done {
			return 0, io.EOF
		}
		channel, payload, err := dr.sr.ReadFrame()
		if err == io.EOF {
			dr.done = true
			return 0, io.EOF
		}
		if err != nil {
			return 0, err
		}
		switch channel {
		case SidebandData:
			dr.buf = payload
		case SidebandProgress:
			if dr.onProgress != nil {
				dr.onProgress(string(payload))
			}
		case SidebandError:
			return 0, fmt.Errorf("remote error: %s", string(payload))
		}
	}

	n := copy(p, dr.buf)
	dr.buf = dr.buf[n:]
	return n, nil
}
```

**Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run TestSideband -v`
Expected: PASS

**Step 5: Commit**

Run: `cd /home/draco/work/graft && git add pkg/remote/sideband.go pkg/remote/sideband_test.go && buckley commit --yes --minimal-output`

---

### Task 8: Pack transport encoder/decoder for wire

**Files:**
- Create: `/home/draco/work/graft/pkg/remote/pack_transport.go`
- Create: `/home/draco/work/graft/pkg/remote/pack_transport_test.go`

**Step 1: Write the tests**

```go
package remote

import (
	"bytes"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestPackTransportRoundTrip(t *testing.T) {
	blob1 := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	blob2 := object.MarshalBlob(&object.Blob{Data: []byte("world\n")})
	hash1 := object.HashObject(object.TypeBlob, blob1)
	hash2 := object.HashObject(object.TypeBlob, blob2)

	records := []ObjectRecord{
		{Hash: hash1, Type: object.TypeBlob, Data: blob1},
		{Hash: hash2, Type: object.TypeBlob, Data: blob2},
	}

	var buf bytes.Buffer
	if err := EncodePackTransport(&buf, records); err != nil {
		t.Fatalf("EncodePackTransport: %v", err)
	}

	decoded, err := DecodePackTransport(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodePackTransport: %v", err)
	}

	if len(decoded) != 2 {
		t.Fatalf("decoded %d records, want 2", len(decoded))
	}
	for i, rec := range decoded {
		if rec.Type != object.TypeBlob {
			t.Fatalf("record %d type = %s, want blob", i, rec.Type)
		}
	}
}

func TestPackTransportEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePackTransport(&buf, nil); err != nil {
		t.Fatalf("EncodePackTransport(nil): %v", err)
	}
	decoded, err := DecodePackTransport(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodePackTransport: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded %d records, want 0", len(decoded))
	}
}

func TestPackTransportCommitAndTree(t *testing.T) {
	blobData := object.MarshalBlob(&object.Blob{Data: []byte("data")})
	blobHash := object.HashObject(object.TypeBlob, blobData)

	treeData := object.MarshalTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "f.txt", BlobHash: blobHash}},
	})
	treeHash := object.HashObject(object.TypeTree, treeData)

	commitData := object.MarshalCommit(&object.CommitObj{
		TreeHash: treeHash, Author: "A", Timestamp: 1, Message: "init",
	})
	commitHash := object.HashObject(object.TypeCommit, commitData)

	records := []ObjectRecord{
		{Hash: commitHash, Type: object.TypeCommit, Data: commitData},
		{Hash: treeHash, Type: object.TypeTree, Data: treeData},
		{Hash: blobHash, Type: object.TypeBlob, Data: blobData},
	}

	var buf bytes.Buffer
	if err := EncodePackTransport(&buf, records); err != nil {
		t.Fatalf("EncodePackTransport: %v", err)
	}

	decoded, err := DecodePackTransport(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodePackTransport: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("decoded %d, want 3", len(decoded))
	}

	types := map[object.ObjectType]int{}
	for _, r := range decoded {
		types[r.Type]++
	}
	if types[object.TypeCommit] != 1 || types[object.TypeTree] != 1 || types[object.TypeBlob] != 1 {
		t.Fatalf("type distribution: %v", types)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run TestPackTransport -v`
Expected: FAIL

**Step 3: Implement `pack_transport.go`**

```go
package remote

import (
	"bytes"
	"fmt"
	"io"

	"github.com/odvcencio/graft/pkg/object"
)

// objectTypeToPackType maps Graft object types to pack object types.
// For entity and entitylist (which don't have Git pack counterparts),
// we use the blob pack type with a type annotation in a separate mechanism.
func objectTypeToPackType(t object.ObjectType) (object.PackObjectType, bool) {
	switch t {
	case object.TypeCommit:
		return object.PackCommit, true
	case object.TypeTree:
		return object.PackTree, true
	case object.TypeBlob, object.TypeEntity, object.TypeEntityList:
		return object.PackBlob, true
	case object.TypeTag:
		return object.PackTag, true
	default:
		return 0, false
	}
}

// packTypeToObjectType maps pack types back, using entity trailer for disambiguation.
func packTypeToObjectType(t object.PackObjectType) (object.ObjectType, bool) {
	switch t {
	case object.PackCommit:
		return object.TypeCommit, true
	case object.PackTree:
		return object.TypeTree, true
	case object.PackBlob:
		return object.TypeBlob, true
	case object.PackTag:
		return object.TypeTag, true
	default:
		return "", false
	}
}

// EncodePackTransport encodes ObjectRecords into a pack stream.
func EncodePackTransport(w io.Writer, records []ObjectRecord) error {
	pw, err := object.NewPackWriter(w, uint32(len(records)))
	if err != nil {
		return fmt.Errorf("create pack writer: %w", err)
	}

	// Build entity trailer for entity/entitylist types that use blob pack type.
	var entityEntries []object.PackEntityTrailerEntry

	for _, rec := range records {
		packType, ok := objectTypeToPackType(rec.Type)
		if !ok {
			return fmt.Errorf("unsupported object type %q", rec.Type)
		}

		// Track entity/entitylist types that need disambiguation.
		if rec.Type == object.TypeEntity || rec.Type == object.TypeEntityList {
			entityEntries = append(entityEntries, object.PackEntityTrailerEntry{
				ObjectHash: rec.Hash,
				StableID:   "type:" + string(rec.Type),
			})
		}

		if err := pw.WriteEntry(packType, rec.Data); err != nil {
			return fmt.Errorf("write pack entry for %s: %w", rec.Hash, err)
		}
	}

	if len(entityEntries) > 0 {
		if _, err := pw.FinishWithEntityTrailer(entityEntries); err != nil {
			return fmt.Errorf("finish pack with entity trailer: %w", err)
		}
	} else {
		if _, err := pw.Finish(); err != nil {
			return fmt.Errorf("finish pack: %w", err)
		}
	}

	return nil
}

// DecodePackTransport decodes a pack stream into ObjectRecords.
func DecodePackTransport(data []byte) ([]ObjectRecord, error) {
	if len(data) == 0 {
		return nil, nil
	}

	pf, err := object.ReadPack(data)
	if err != nil {
		return nil, fmt.Errorf("read pack: %w", err)
	}

	// Resolve any delta entries.
	resolved, err := object.ResolvePackEntries(pf.Entries)
	if err != nil {
		return nil, fmt.Errorf("resolve deltas: %w", err)
	}

	// Build entity type overrides from trailer.
	typeOverrides := map[object.Hash]object.ObjectType{}
	if pf.EntityTrailer != nil {
		for _, entry := range pf.EntityTrailer.Entries {
			if len(entry.StableID) > 5 && entry.StableID[:5] == "type:" {
				typeOverrides[entry.ObjectHash] = object.ObjectType(entry.StableID[5:])
			}
		}
	}

	records := make([]ObjectRecord, 0, len(resolved))
	for _, entry := range resolved {
		objType, ok := packTypeToObjectType(entry.Type)
		if !ok {
			return nil, fmt.Errorf("unsupported pack type %d", entry.Type)
		}

		hash := object.HashObject(objType, entry.Data)

		// Check for entity type overrides.
		if override, ok := typeOverrides[hash]; ok {
			objType = override
			hash = object.HashObject(objType, entry.Data)
		}

		records = append(records, ObjectRecord{
			Hash: hash,
			Type: objType,
			Data: entry.Data,
		})
	}

	return records, nil
}

// EncodePackTransportToBytes is a convenience wrapper.
func EncodePackTransportToBytes(records []ObjectRecord) ([]byte, error) {
	var buf bytes.Buffer
	if err := EncodePackTransport(&buf, records); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
```

**Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -run TestPackTransport -v`
Expected: PASS

**Step 5: Commit**

Run: `cd /home/draco/work/graft && git add pkg/remote/pack_transport.go pkg/remote/pack_transport_test.go && buckley commit --yes --minimal-output`

---

### Task 9: Wire pack+zstd+sideband into client fetch path

**Files:**
- Modify: `/home/draco/work/graft/pkg/remote/client.go`
- Modify: `/home/draco/work/graft/pkg/remote/sync_test.go`

This task adds a new `BatchObjectsPack` method that uses the pack wire format when server capabilities allow it, and updates `FetchIntoStoreWithConfig` to prefer it.

**Step 1: Write the test**

Add to `sync_test.go`:

```go
func TestFetchIntoStoreWithPackTransport(t *testing.T) {
	remoteRoot := t.TempDir()
	remoteStore := object.NewStore(remoteRoot)

	blobHash, _ := remoteStore.WriteBlob(&object.Blob{Data: []byte("hello\n")})
	treeHash, _ := remoteStore.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "README.md", BlobHash: blobHash}}})
	commitHash, _ := remoteStore.WriteCommit(&object.CommitObj{
		TreeHash: treeHash, Author: "Alice", Timestamp: 1700000000, Message: "init",
	})

	// Build a pack response from the objects.
	commitType, commitData, _ := remoteStore.Read(commitHash)
	treeType, treeData, _ := remoteStore.Read(treeHash)
	blobType, blobData, _ := remoteStore.Read(blobHash)

	records := []ObjectRecord{
		{Hash: commitHash, Type: commitType, Data: commitData},
		{Hash: treeHash, Type: treeType, Data: treeData},
		{Hash: blobHash, Type: blobType, Data: blobData},
	}
	packBytes, err := EncodePackTransportToBytes(records)
	if err != nil {
		t.Fatalf("EncodePackTransport: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/graft/alice/repo/objects/batch" {
			accept := r.Header.Get("Accept")
			if strings.Contains(accept, "application/x-graft-pack") {
				w.Header().Set("Content-Type", "application/x-graft-pack")
				_, _ = w.Write(packBytes)
				return
			}
			// Fallback to JSON.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"objects": []map[string]any{
					{"hash": string(commitHash), "type": string(commitType), "data": commitData},
					{"hash": string(treeHash), "type": string(treeType), "data": treeData},
					{"hash": string(blobHash), "type": string(blobType), "data": blobData},
				},
				"truncated": false,
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	client, _ := NewClient(ts.URL + "/graft/alice/repo")
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
			t.Fatalf("missing object %s", h)
		}
	}
}
```

**Step 2: Implement**

Add `BatchObjectsPack` to `client.go` — a variant of `BatchObjects` that sends `Accept: application/x-graft-pack` and decodes the response as a pack stream:

```go
func (c *Client) BatchObjectsPack(ctx context.Context, wants, haves []object.Hash, maxObjects int) ([]ObjectRecord, bool, error) {
	// Same request body as BatchObjects.
	reqBody := struct {
		Wants      []string `json:"wants"`
		Haves      []string `json:"haves,omitempty"`
		MaxObjects int      `json:"max_objects,omitempty"`
	}{...} // Same construction as BatchObjects

	payload, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.BaseURL+"/objects/batch", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-graft-pack")
	req.Header.Set("Accept-Encoding", "zstd")
	c.applyAuth(req)

	resp, err := retryDo(c.httpClient, req, c.maxAttempts)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, false, fmt.Errorf("batch objects: %s", string(body))
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/x-graft-pack") {
		// Server didn't support pack — fall back to JSON parse.
		return c.decodeBatchJSON(resp)
	}

	var reader io.Reader = resp.Body
	if isZstdEncoded(resp.Header.Get("Content-Encoding")) {
		zr, err := newZstdReader(reader)
		if err != nil {
			return nil, false, err
		}
		defer zr.Close()
		reader = zr
	}

	body, err := io.ReadAll(io.LimitReader(reader, responseLimitBatch))
	if err != nil {
		return nil, false, err
	}

	records, err := DecodePackTransport(body)
	if err != nil {
		return nil, false, fmt.Errorf("decode pack: %w", err)
	}

	truncated := resp.Header.Get("X-Truncated") == "true"
	return records, truncated, nil
}
```

Update `FetchIntoStoreWithConfig` to try pack transport first, falling back to JSON:

Replace the `BatchObjects` call with a try-pack-then-fallback approach, or simply always try `BatchObjectsPack` which handles the fallback internally when server returns JSON.

**Step 3: Run all tests**

Run: `cd /home/draco/work/graft && go test ./pkg/remote/ -v`
Expected: PASS

**Step 4: Commit**

Run: `cd /home/draco/work/graft && git add pkg/remote/client.go pkg/remote/sync.go pkg/remote/sync_test.go && buckley commit --yes --minimal-output`

---

### Task 10: Wire pack+zstd into client push path

**Files:**
- Modify: `/home/draco/work/graft/pkg/remote/client.go`
- Modify: `/home/draco/work/graft/pkg/remote/client_test.go`

Add `PushObjectsPack` that sends objects as a zstd-compressed pack stream with `Content-Type: application/x-graft-pack` and `Content-Encoding: zstd`. Update `PushObjects` to use it when server capabilities match.

Follow the same test pattern as Task 9: create an httptest server that accepts pack-encoded pushes, verify objects decode correctly.

**Step 1: Write test, Step 2: Verify fail, Step 3: Implement, Step 4: Verify pass, Step 5: Commit**

---

## Layer 4: Smart Negotiation

### Task 11: Server-advertised limits parsing

**Files:**
- Modify: `/home/draco/work/graft/pkg/remote/protocol.go`
- Modify: `/home/draco/work/graft/pkg/remote/protocol_test.go`

Add `ServerLimits` struct and `ParseLimits(header string) ServerLimits` that parses `Graft-Limits: max_batch=50000,max_payload=67108864,max_object=33554432`.

Update `FetchConfig` resolution to use server-advertised limits when available. The client reads `Graft-Limits` from the first response and caches it on the `Client` struct.

**Step 1: Write test, Step 2: Verify fail, Step 3: Implement, Step 4: Verify pass, Step 5: Commit**

---

### Task 12: Ref pagination

**Files:**
- Modify: `/home/draco/work/graft/pkg/remote/client.go`
- Modify: `/home/draco/work/graft/pkg/remote/client_test.go`

Add paginated `ListRefs` that sends `?cursor=&limit=1000`, loops until cursor is empty. Backward compatible — if server returns no cursor, treats as single page (existing behavior).

**Step 1: Write test, Step 2: Verify fail, Step 3: Implement, Step 4: Verify pass, Step 5: Commit**

---

## Orchard Server Side

### Task 13: Add structured errors, capability headers, and limits to orchard handlers

**Files:**
- Modify: `/home/draco/work/orchard/internal/gotprotocol/transport.go`

Add to all handlers:
- Read `Graft-Protocol` and `Graft-Capabilities` headers from request
- Set `Graft-Capabilities` and `Graft-Limits` response headers
- Return structured JSON error bodies: `{"error": "...", "code": "...", "detail": "..."}`

---

### Task 14: Add pack transport support to orchard batch endpoint

**Files:**
- Modify: `/home/draco/work/orchard/internal/gotprotocol/transport.go`

In `handleBatchObjects`: check `Accept: application/x-graft-pack` header. If present, encode response as pack stream (using the same `EncodePackTransport` function — extract to a shared package or duplicate). Add `Content-Encoding: zstd` if client sent `Accept-Encoding: zstd`. Wrap in sideband framing if client advertises `sideband` capability.

---

### Task 15: Add pack transport support to orchard push endpoint

**Files:**
- Modify: `/home/draco/work/orchard/internal/gotprotocol/transport.go`

In `handlePushObjects`: check `Content-Type: application/x-graft-pack`. If present, decode request body as pack stream (with zstd decompression if `Content-Encoding: zstd`). Otherwise fall back to existing NDJSON parsing.

---

### Task 16: Add ref pagination to orchard

**Files:**
- Modify: `/home/draco/work/orchard/internal/gotprotocol/transport.go`

In `handleListRefs`: read `cursor` and `limit` query params. If present, paginate the ref listing. Return `{"refs": {...}, "cursor": "..."}` format. If no params, return existing flat format for backward compatibility.

---

### Task 17: Final verification and tag

**Step 1:** Run all graft tests: `cd /home/draco/work/graft && go test ./... -v`
**Step 2:** Run all orchard tests: `cd /home/draco/work/orchard && go test ./... -v`
**Step 3:** E2E smoke test: clone, push, pull against local orchard with pack transport
**Step 4:** Tag and push: `cd /home/draco/work/graft && git tag v0.X.0 && git push origin main --tags`
