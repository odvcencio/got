package merge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// AIResolver resolves entity-level merge conflicts using an AI model.
type AIResolver interface {
	Resolve(ctx context.Context, req AIResolveRequest) (*AIResolveResult, error)
}

// AIResolveRequest contains the entity-level context for an AI merge resolution.
type AIResolveRequest struct {
	FilePath   string
	EntityKey  string
	EntityKind string
	Language   string
	BaseBody   []byte
	OursBody   []byte
	TheirsBody []byte
}

// AIResolveResult holds the AI-generated resolution for a merge conflict.
type AIResolveResult struct {
	ResolvedBody []byte
	Explanation  string
	Confidence   float64
}

// ClaudeResolver implements AIResolver using the Anthropic Claude API.
type ClaudeResolver struct {
	APIKey     string
	Model      string
	APIURL     string
	HTTPClient *http.Client
}

const (
	defaultClaudeModel  = "claude-sonnet-4-20250514"
	defaultClaudeAPIURL = "https://api.anthropic.com/v1/messages"
	claudeAPIVersion    = "2023-06-01"
	claudeMaxTokens     = 4096
	claudeHTTPTimeout   = 120 * time.Second
	// maxEntityBodySize is the maximum size of a single entity body that will
	// be sent to the AI resolver. Bodies larger than this are likely too big for
	// the model's context window and would produce poor results.
	maxEntityBodySize = 100 * 1024 // 100 KB
)

// NewClaudeResolver creates a ClaudeResolver from config values and environment.
// apiKey may be empty; the resolver will fall back to the ANTHROPIC_API_KEY
// environment variable.
func NewClaudeResolver(apiKey, model string) (*ClaudeResolver, error) {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("AI resolve: no API key configured (set ai_api_key in ~/.graftconfig or ANTHROPIC_API_KEY env var)")
	}
	if model == "" {
		model = defaultClaudeModel
	}
	return &ClaudeResolver{
		APIKey: apiKey,
		Model:  model,
		APIURL: defaultClaudeAPIURL,
		HTTPClient: &http.Client{
			Timeout: claudeHTTPTimeout,
		},
	}, nil
}

// Resolve sends the entity conflict to Claude and parses the resolved code
// from the response.
func (c *ClaudeResolver) Resolve(ctx context.Context, req AIResolveRequest) (*AIResolveResult, error) {
	// Check entity body sizes to avoid sending requests that will exceed the
	// model's context window or produce poor results.
	for _, body := range []struct {
		name string
		data []byte
	}{
		{"base", req.BaseBody},
		{"ours", req.OursBody},
		{"theirs", req.TheirsBody},
	} {
		if len(body.data) > maxEntityBodySize {
			return nil, fmt.Errorf("AI resolve: %s body too large (%d bytes, max %d) — resolve this conflict manually",
				body.name, len(body.data), maxEntityBodySize)
		}
	}

	prompt := buildResolvePrompt(req)

	body := claudeRequest{
		Model:     c.Model,
		MaxTokens: claudeMaxTokens,
		Messages: []claudeMessage{
			{Role: "user", Content: prompt},
		},
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("AI resolve: marshal request: %w", err)
	}

	apiURL := c.APIURL
	if apiURL == "" {
		apiURL = defaultClaudeAPIURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("AI resolve: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", claudeAPIVersion)

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: claudeHTTPTimeout}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("AI resolve: API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Limit response size to 1MB to prevent unbounded memory allocation.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("AI resolve: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AI resolve: API returned status %d: %s", resp.StatusCode, truncateBody(respBody, 500))
	}

	return parseClaudeResponse(respBody)
}

// claudeRequest is the request body for the Claude messages API.
type claudeRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []claudeMessage  `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// claudeResponse is the response body from the Claude messages API.
type claudeResponse struct {
	Content []claudeContentBlock `json:"content"`
	Error   *claudeError         `json:"error,omitempty"`
}

type claudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type claudeError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// buildResolvePrompt constructs the prompt for AI merge resolution.
func buildResolvePrompt(req AIResolveRequest) string {
	var sb strings.Builder

	sb.WriteString("You are resolving a merge conflict in a version control system.\n\n")

	sb.WriteString("## Context\n")
	fmt.Fprintf(&sb, "- **File:** %s\n", req.FilePath)
	if req.Language != "" {
		fmt.Fprintf(&sb, "- **Language:** %s\n", req.Language)
	}
	if req.EntityKind != "" {
		fmt.Fprintf(&sb, "- **Entity kind:** %s\n", req.EntityKind)
	}
	if req.EntityKey != "" {
		fmt.Fprintf(&sb, "- **Entity:** %s\n", req.EntityKey)
	}
	sb.WriteString("\n")

	sb.WriteString("## Base version (common ancestor)\n```\n")
	if len(req.BaseBody) > 0 {
		sb.Write(req.BaseBody)
	} else {
		sb.WriteString("(empty — entity did not exist in base)")
	}
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Ours version (current branch)\n```\n")
	if len(req.OursBody) > 0 {
		sb.Write(req.OursBody)
	} else {
		sb.WriteString("(deleted — entity was removed in our branch)")
	}
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Theirs version (incoming branch)\n```\n")
	if len(req.TheirsBody) > 0 {
		sb.Write(req.TheirsBody)
	} else {
		sb.WriteString("(deleted — entity was removed in their branch)")
	}
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Instructions\n")
	sb.WriteString("Resolve this merge conflict by producing the correct merged version of this code entity. ")
	sb.WriteString("Consider the intent of both changes relative to the base version. ")
	sb.WriteString("Preserve the semantics of both modifications where possible. ")
	sb.WriteString("If one side deleted the entity and the other modified it, determine whether the deletion or the modification should take precedence based on context.\n\n")

	sb.WriteString("## Output format\n")
	sb.WriteString("First, briefly explain your resolution (1-3 sentences).\n")
	sb.WriteString("Then provide the resolved code in a fenced code block:\n")
	sb.WriteString("```resolved\n<your resolved code here>\n```\n\n")
	sb.WriteString("After the code block, state your confidence as a decimal between 0.0 and 1.0:\n")
	sb.WriteString("Confidence: 0.95\n")

	return sb.String()
}

// codeBlockPattern matches fenced code blocks with a "resolved" or language tag.
var codeBlockPattern = regexp.MustCompile("(?s)```(?:resolved|\\w*)\\s*\\n(.*?)\\n?```")

// confidencePattern matches the confidence line.
var confidencePattern = regexp.MustCompile(`(?i)confidence:\s*([\d.]+)`)

// parseClaudeResponse extracts the resolved code, explanation, and confidence
// from a Claude API response.
func parseClaudeResponse(body []byte) (*AIResolveResult, error) {
	var resp claudeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("AI resolve: parse response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("AI resolve: API error: %s: %s", resp.Error.Type, resp.Error.Message)
	}

	// Extract text content.
	var text string
	for _, block := range resp.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("AI resolve: empty response from API")
	}

	// Extract code block.
	matches := codeBlockPattern.FindStringSubmatch(text)
	if len(matches) < 2 {
		return nil, fmt.Errorf("AI resolve: no code block found in response")
	}
	resolvedCode := matches[1]

	// Extract confidence.
	confidence := 0.5 // default if not found
	confMatches := confidencePattern.FindStringSubmatch(text)
	if len(confMatches) >= 2 {
		var parsed float64
		if _, err := fmt.Sscanf(confMatches[1], "%f", &parsed); err == nil {
			if parsed >= 0 && parsed <= 1.0 {
				confidence = parsed
			}
		}
	}

	// Extract explanation: everything before the code block.
	codeBlockIdx := codeBlockPattern.FindStringIndex(text)
	explanation := ""
	if codeBlockIdx != nil {
		explanation = strings.TrimSpace(text[:codeBlockIdx[0]])
	}

	return &AIResolveResult{
		ResolvedBody: []byte(resolvedCode),
		Explanation:  explanation,
		Confidence:   confidence,
	}, nil
}

// truncateBody truncates a response body for error messages.
func truncateBody(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
