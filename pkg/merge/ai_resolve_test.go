package merge

import (
	"context"
	"fmt"
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
