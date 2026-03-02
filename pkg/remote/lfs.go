package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// LFSBatchRequest is the payload sent to the LFS batch endpoint.
type LFSBatchRequest struct {
	Operation string           `json:"operation"` // "upload" or "download"
	Objects   []LFSBatchObject `json:"objects"`
}

// LFSBatchObject identifies a single LFS object by OID and size.
type LFSBatchObject struct {
	OID  string `json:"oid"`
	Size int64  `json:"size"`
}

// LFSBatchResponse is the server response from the LFS batch endpoint.
type LFSBatchResponse struct {
	Objects []LFSBatchResponseObject `json:"objects"`
}

// LFSBatchResponseObject describes the server's response for one LFS object,
// including action URLs and optional per-object errors.
type LFSBatchResponseObject struct {
	OID     string                `json:"oid"`
	Size    int64                 `json:"size"`
	Actions map[string]LFSAction  `json:"actions,omitempty"`
	Error   *LFSObjectError       `json:"error,omitempty"`
}

// LFSAction describes an upload, download, or verify action returned by the
// batch endpoint.
type LFSAction struct {
	Href   string            `json:"href"`
	Header map[string]string `json:"header,omitempty"`
}

// LFSObjectError is a per-object error in a batch response.
type LFSObjectError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// LFSClient is a transfer client for the LFS batch API.
type LFSClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

// NewLFSClient creates an LFS client from an existing remote Client. The LFS
// endpoint is derived from the Client's base URL (e.g. .../graft/owner/repo).
func NewLFSClient(c *Client) *LFSClient {
	return &LFSClient{
		BaseURL:    c.endpoint.BaseURL,
		HTTPClient: c.httpClient,
		Token:      c.token,
	}
}

// NewLFSClientFromURL creates an LFS client from a raw base URL and optional
// auth token. This is useful for tests and manual CLI invocations.
func NewLFSClientFromURL(baseURL, token string) *LFSClient {
	return &LFSClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: http.DefaultClient,
		Token:      token,
	}
}

const (
	lfsMediaType      = "application/vnd.graft-lfs+json"
	lfsBatchPath      = "/lfs/objects/batch"
	lfsResponseLimit  = 8 << 20 // 8MB
	lfsDownloadLimit  = 256 << 20 // 256MB per object
)

// BatchRequest sends a batch request to the LFS endpoint and returns the
// server's response describing which objects need action.
func (lc *LFSClient) BatchRequest(ctx context.Context, req LFSBatchRequest) (*LFSBatchResponse, error) {
	if req.Operation != "upload" && req.Operation != "download" {
		return nil, fmt.Errorf("lfs batch: invalid operation %q", req.Operation)
	}
	if len(req.Objects) == 0 {
		return &LFSBatchResponse{}, nil
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("lfs batch: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, lc.BaseURL+lfsBatchPath, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("lfs batch: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", lfsMediaType)
	httpReq.Header.Set("Accept", lfsMediaType)
	lc.applyAuth(httpReq)

	resp, err := lc.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("lfs batch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, lfsResponseLimit))
	if err != nil {
		return nil, fmt.Errorf("lfs batch: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("lfs batch: server returned %d: %s", resp.StatusCode, msg)
	}

	var batchResp LFSBatchResponse
	if err := json.Unmarshal(body, &batchResp); err != nil {
		return nil, fmt.Errorf("lfs batch: decode response: %w", err)
	}

	return &batchResp, nil
}

// Upload sends content to the upload href provided by the batch response.
func (lc *LFSClient) Upload(ctx context.Context, action LFSAction, content io.Reader, size int64) error {
	if strings.TrimSpace(action.Href) == "" {
		return fmt.Errorf("lfs upload: empty href")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, action.Href, content)
	if err != nil {
		return fmt.Errorf("lfs upload: create request: %w", err)
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")

	// Apply action-specific headers (e.g. Authorization from the server).
	for k, v := range action.Header {
		req.Header.Set(k, v)
	}
	// Apply default auth if the action did not provide Authorization.
	if req.Header.Get("Authorization") == "" {
		lc.applyAuth(req)
	}

	resp, err := lc.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("lfs upload: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("lfs upload: server returned %d", resp.StatusCode)
	}

	return nil
}

// Download fetches content from the download href provided by the batch
// response. The caller is responsible for closing the returned ReadCloser.
func (lc *LFSClient) Download(ctx context.Context, action LFSAction) (io.ReadCloser, error) {
	if strings.TrimSpace(action.Href) == "" {
		return nil, fmt.Errorf("lfs download: empty href")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, action.Href, nil)
	if err != nil {
		return nil, fmt.Errorf("lfs download: create request: %w", err)
	}

	// Apply action-specific headers.
	for k, v := range action.Header {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Authorization") == "" {
		lc.applyAuth(req)
	}

	resp, err := lc.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lfs download: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("lfs download: server returned %d", resp.StatusCode)
	}

	return &limitedReadCloser{
		Reader: io.LimitReader(resp.Body, lfsDownloadLimit),
		Closer: resp.Body,
	}, nil
}

// limitedReadCloser wraps a Reader and Closer so that reads are bounded
// while the underlying body can still be closed.
type limitedReadCloser struct {
	io.Reader
	io.Closer
}

func (lc *LFSClient) applyAuth(req *http.Request) {
	if strings.TrimSpace(lc.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+lc.Token)
	}
}
