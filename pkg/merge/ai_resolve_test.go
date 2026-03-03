package merge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// MockResolver is a test AIResolver that returns pre-configured results.
type MockResolver struct {
	Result *AIResolveResult
	Err    error
	Calls  []AIResolveRequest
}

func (m *MockResolver) Resolve(_ context.Context, req AIResolveRequest) (*AIResolveResult, error) {
	m.Calls = append(m.Calls, req)
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Result, nil
}

func TestAIResolverInterface(t *testing.T) {
	// Verify MockResolver satisfies the AIResolver interface.
	var _ AIResolver = &MockResolver{}
	var _ AIResolver = &ClaudeResolver{}
}

func TestMockResolver(t *testing.T) {
	mock := &MockResolver{
		Result: &AIResolveResult{
			ResolvedBody: []byte("func Hello() string {\n\treturn \"merged\"\n}"),
			Explanation:  "Combined both changes",
			Confidence:   0.95,
		},
	}

	req := AIResolveRequest{
		FilePath:   "main.go",
		EntityKey:  "func:Hello",
		EntityKind: "function_declaration",
		Language:   "go",
		BaseBody:   []byte("func Hello() string {\n\treturn \"hello\"\n}"),
		OursBody:   []byte("func Hello() string {\n\treturn \"hello world\"\n}"),
		TheirsBody: []byte("func Hello() string {\n\treturn \"hello there\"\n}"),
	}

	result, err := mock.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Confidence != 0.95 {
		t.Errorf("confidence = %f, want 0.95", result.Confidence)
	}
	if string(result.ResolvedBody) != "func Hello() string {\n\treturn \"merged\"\n}" {
		t.Errorf("unexpected resolved body: %s", result.ResolvedBody)
	}
	if result.Explanation != "Combined both changes" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
	if len(mock.Calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(mock.Calls))
	}
	if mock.Calls[0].FilePath != "main.go" {
		t.Errorf("call file path = %q, want %q", mock.Calls[0].FilePath, "main.go")
	}
}

func TestMockResolverError(t *testing.T) {
	mock := &MockResolver{
		Err: fmt.Errorf("simulated API failure"),
	}

	_, err := mock.Resolve(context.Background(), AIResolveRequest{
		FilePath: "main.go",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "simulated API failure") {
		t.Errorf("error = %q, want to contain 'simulated API failure'", err.Error())
	}
}

func TestBuildResolvePrompt(t *testing.T) {
	req := AIResolveRequest{
		FilePath:   "pkg/handler.go",
		EntityKey:  "func:ProcessOrder",
		EntityKind: "function_declaration",
		Language:   "go",
		BaseBody:   []byte("func ProcessOrder(id int) error {\n\treturn nil\n}"),
		OursBody:   []byte("func ProcessOrder(id int) error {\n\tlog.Info(\"processing\")\n\treturn nil\n}"),
		TheirsBody: []byte("func ProcessOrder(id int, ctx context.Context) error {\n\treturn nil\n}"),
	}

	prompt := buildResolvePrompt(req)

	// Check essential parts are present.
	checks := []string{
		"pkg/handler.go",
		"go",
		"function_declaration",
		"func:ProcessOrder",
		"Base version",
		"Ours version",
		"Theirs version",
		"ProcessOrder",
		"```resolved",
		"Confidence:",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}
}

func TestBuildResolvePromptEmptyBase(t *testing.T) {
	req := AIResolveRequest{
		FilePath:   "main.go",
		EntityKey:  "func:NewFunc",
		EntityKind: "function_declaration",
		Language:   "go",
		BaseBody:   nil,
		OursBody:   []byte("func NewFunc() {}"),
		TheirsBody: []byte("func NewFunc() int { return 0 }"),
	}

	prompt := buildResolvePrompt(req)
	if !strings.Contains(prompt, "entity did not exist in base") {
		t.Error("prompt should indicate empty base")
	}
}

func TestBuildResolvePromptDeletedSide(t *testing.T) {
	req := AIResolveRequest{
		FilePath:   "main.go",
		EntityKey:  "func:OldFunc",
		EntityKind: "function_declaration",
		Language:   "go",
		BaseBody:   []byte("func OldFunc() {}"),
		OursBody:   nil,
		TheirsBody: []byte("func OldFunc() { log.Println(\"still here\") }"),
	}

	prompt := buildResolvePrompt(req)
	if !strings.Contains(prompt, "entity was removed in our branch") {
		t.Error("prompt should indicate ours deleted")
	}
}

func TestParseClaudeResponse(t *testing.T) {
	resp := claudeResponse{
		Content: []claudeContentBlock{
			{
				Type: "text",
				Text: `I combined both changes by adding the logging from ours and the context parameter from theirs.

` + "```resolved\nfunc ProcessOrder(id int, ctx context.Context) error {\n\tlog.Info(\"processing\")\n\treturn nil\n}\n```" + `

Confidence: 0.92`,
			},
		},
	}

	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	result, err := parseClaudeResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	expectedCode := "func ProcessOrder(id int, ctx context.Context) error {\n\tlog.Info(\"processing\")\n\treturn nil\n}"
	if string(result.ResolvedBody) != expectedCode {
		t.Errorf("resolved body:\n%s\nwant:\n%s", result.ResolvedBody, expectedCode)
	}

	if result.Confidence != 0.92 {
		t.Errorf("confidence = %f, want 0.92", result.Confidence)
	}

	if !strings.Contains(result.Explanation, "combined both changes") {
		t.Errorf("explanation = %q, expected to contain 'combined both changes'", result.Explanation)
	}
}

func TestParseClaudeResponseNoCodeBlock(t *testing.T) {
	resp := claudeResponse{
		Content: []claudeContentBlock{
			{Type: "text", Text: "I cannot resolve this conflict."},
		},
	}

	body, _ := json.Marshal(resp)
	_, err := parseClaudeResponse(body)
	if err == nil {
		t.Fatal("expected error for missing code block")
	}
	if !strings.Contains(err.Error(), "no code block") {
		t.Errorf("error = %q, want 'no code block'", err.Error())
	}
}

func TestParseClaudeResponseAPIError(t *testing.T) {
	resp := claudeResponse{
		Error: &claudeError{
			Type:    "invalid_request_error",
			Message: "Invalid API key",
		},
	}

	body, _ := json.Marshal(resp)
	_, err := parseClaudeResponse(body)
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("error = %q, want 'Invalid API key'", err.Error())
	}
}

func TestParseClaudeResponseDefaultConfidence(t *testing.T) {
	resp := claudeResponse{
		Content: []claudeContentBlock{
			{
				Type: "text",
				Text: "Here's the resolution.\n\n```resolved\nfunc Foo() {}\n```\n",
			},
		},
	}

	body, _ := json.Marshal(resp)
	result, err := parseClaudeResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Confidence != 0.5 {
		t.Errorf("confidence = %f, want 0.5 (default)", result.Confidence)
	}
}

func TestParseClaudeResponseEmptyContent(t *testing.T) {
	resp := claudeResponse{
		Content: []claudeContentBlock{},
	}

	body, _ := json.Marshal(resp)
	_, err := parseClaudeResponse(body)
	if err == nil {
		t.Fatal("expected error for empty content")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error = %q, want 'empty response'", err.Error())
	}
}

func TestClaudeResolverHTTP(t *testing.T) {
	// Spin up a fake Claude API server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want %q", r.Header.Get("x-api-key"), "test-key")
		}
		if r.Header.Get("anthropic-version") != claudeAPIVersion {
			t.Errorf("anthropic-version = %q, want %q", r.Header.Get("anthropic-version"), claudeAPIVersion)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want %q", r.Header.Get("Content-Type"), "application/json")
		}

		// Verify request body.
		var req claudeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != "claude-sonnet-4-20250514" {
			t.Errorf("model = %q, want %q", req.Model, "claude-sonnet-4-20250514")
		}
		if len(req.Messages) != 1 {
			t.Errorf("messages count = %d, want 1", len(req.Messages))
		}

		resp := claudeResponse{
			Content: []claudeContentBlock{
				{
					Type: "text",
					Text: "Merged both changes.\n\n```resolved\nfunc Hello() string {\n\treturn \"merged\"\n}\n```\n\nConfidence: 0.88",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	resolver := &ClaudeResolver{
		APIKey:     "test-key",
		Model:      defaultClaudeModel,
		APIURL:     server.URL,
		HTTPClient: server.Client(),
	}

	result, err := resolver.Resolve(context.Background(), AIResolveRequest{
		FilePath:   "main.go",
		EntityKey:  "func:Hello",
		EntityKind: "function_declaration",
		Language:   "go",
		BaseBody:   []byte("func Hello() string { return \"hello\" }"),
		OursBody:   []byte("func Hello() string { return \"world\" }"),
		TheirsBody: []byte("func Hello() string { return \"there\" }"),
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Confidence != 0.88 {
		t.Errorf("confidence = %f, want 0.88", result.Confidence)
	}
	expected := "func Hello() string {\n\treturn \"merged\"\n}"
	if string(result.ResolvedBody) != expected {
		t.Errorf("resolved body = %q, want %q", result.ResolvedBody, expected)
	}
}

func TestClaudeResolverHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer server.Close()

	resolver := &ClaudeResolver{
		APIKey:     "bad-key",
		Model:      defaultClaudeModel,
		APIURL:     server.URL,
		HTTPClient: server.Client(),
	}

	_, err := resolver.Resolve(context.Background(), AIResolveRequest{
		FilePath: "main.go",
	})
	if err == nil {
		t.Fatal("expected error for unauthorized")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("error = %q, want to contain 'status 401'", err.Error())
	}
}

func TestClaudeResolverContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response — the context should cancel before we respond.
		select {
		case <-r.Context().Done():
			return
		case <-make(chan struct{}):
			// never reached
		}
	}))
	defer server.Close()

	resolver := &ClaudeResolver{
		APIKey:     "test-key",
		Model:      defaultClaudeModel,
		APIURL:     server.URL,
		HTTPClient: server.Client(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := resolver.Resolve(ctx, AIResolveRequest{FilePath: "main.go"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestNewClaudeResolverNoKey(t *testing.T) {
	// Temporarily unset env var.
	old := getEnvForTest("ANTHROPIC_API_KEY")
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, err := NewClaudeResolver("", "")
	if err == nil {
		t.Fatal("expected error when no API key")
	}
	if !strings.Contains(err.Error(), "no API key") {
		t.Errorf("error = %q, want 'no API key'", err.Error())
	}

	// Restore.
	if old != "" {
		t.Setenv("ANTHROPIC_API_KEY", old)
	}
}

func TestNewClaudeResolverFromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key-123")

	r, err := NewClaudeResolver("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.APIKey != "env-key-123" {
		t.Errorf("APIKey = %q, want %q", r.APIKey, "env-key-123")
	}
	if r.Model != defaultClaudeModel {
		t.Errorf("Model = %q, want %q", r.Model, defaultClaudeModel)
	}
}

func TestNewClaudeResolverCustomModel(t *testing.T) {
	r, err := NewClaudeResolver("test-key", "claude-opus-4-20250514")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Model != "claude-opus-4-20250514" {
		t.Errorf("Model = %q, want %q", r.Model, "claude-opus-4-20250514")
	}
}

func TestTruncateBody(t *testing.T) {
	short := "hello"
	if truncateBody([]byte(short), 10) != "hello" {
		t.Error("short string should not be truncated")
	}

	long := strings.Repeat("x", 100)
	result := truncateBody([]byte(long), 10)
	if len(result) != 13 { // 10 + "..."
		t.Errorf("truncated length = %d, want 13", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Error("truncated string should end with ...")
	}
}

// getEnvForTest reads an environment variable for test setup.
func getEnvForTest(key string) string {
	return os.Getenv(key)
}
