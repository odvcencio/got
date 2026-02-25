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
		t.Fatalf("expected 1 call, got %d", calls)
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
		t.Fatalf("expected 3 calls, got %d", calls)
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
		t.Fatalf("expected 2 calls, got %d", calls)
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
		t.Fatalf("expected 1 call (no retry on 4xx), got %d", calls)
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
		t.Fatalf("expected 2 calls, got %d", calls)
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
		t.Fatalf("expected 3 calls (exhausted retries), got %d", calls)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("final status = %d, want 500", resp.StatusCode)
	}
}
