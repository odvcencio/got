package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/object"
)

// Endpoint identifies a Got protocol repository endpoint.
// BaseURL is normalized to ".../got/{owner}/{repo}" with no trailing slash.
type Endpoint struct {
	Raw     string
	BaseURL string
	Owner   string
	Repo    string
	user    string
	pass    string
}

// ParseEndpoint parses a remote URL into a canonical endpoint.
//
// Supported inputs include:
// - https://host/got/owner/repo
// - https://host/owner/repo (expanded to /got/owner/repo)
// - https://host/api/v1/got/owner/repo
func ParseEndpoint(raw string) (Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Endpoint{}, fmt.Errorf("remote URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Endpoint{}, fmt.Errorf("parse remote URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return Endpoint{}, fmt.Errorf("remote URL must include scheme and host")
	}

	segments := splitPathSegments(u.Path)
	if len(segments) < 2 {
		return Endpoint{}, fmt.Errorf("remote URL must include owner and repository")
	}

	gotIdx := -1
	for i := 0; i+2 < len(segments); i++ {
		if segments[i] == "got" {
			gotIdx = i
		}
	}

	var owner, repo string
	var baseSegments []string
	if gotIdx >= 0 {
		owner = segments[gotIdx+1]
		repo = segments[gotIdx+2]
		baseSegments = append(baseSegments, segments[:gotIdx+3]...)
	} else {
		owner = segments[len(segments)-2]
		repo = segments[len(segments)-1]
		baseSegments = append(baseSegments, segments[:len(segments)-2]...)
		baseSegments = append(baseSegments, "got", owner, repo)
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" {
		return Endpoint{}, fmt.Errorf("remote URL must include non-empty owner and repository")
	}

	endpointURL := *u
	endpointURL.Path = "/" + strings.Join(baseSegments, "/")
	endpointURL.RawPath = ""
	endpointURL.RawQuery = ""
	endpointURL.Fragment = ""
	user := ""
	pass := ""
	if endpointURL.User != nil {
		user = endpointURL.User.Username()
		pass, _ = endpointURL.User.Password()
	}
	endpointURL.User = nil

	return Endpoint{
		Raw:     raw,
		BaseURL: strings.TrimRight(endpointURL.String(), "/"),
		Owner:   owner,
		Repo:    repo,
		user:    user,
		pass:    pass,
	}, nil
}

func splitPathSegments(p string) []string {
	p = strings.TrimSpace(path.Clean(p))
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == "." {
		return nil
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}

// ObjectRecord is an object payload used by push/pull operations.
type ObjectRecord struct {
	Hash object.Hash
	Type object.ObjectType
	Data []byte
}

// RefUpdate is one atomic reference update request.
type RefUpdate struct {
	Name string
	Old  *object.Hash
	New  *object.Hash
}

// Client is a transport client for gothub's Got protocol.
type Client struct {
	endpoint   Endpoint
	httpClient *http.Client
	token      string
	user       string
	pass       string
}

// NewClient creates a remote protocol client.
//
// Auth resolution order:
// 1) GOT_TOKEN (Bearer)
// 2) GOT_USERNAME + GOT_PASSWORD (Basic)
// 3) URL userinfo (Basic)
func NewClient(remoteURL string) (*Client, error) {
	endpoint, err := ParseEndpoint(remoteURL)
	if err != nil {
		return nil, err
	}

	token := strings.TrimSpace(os.Getenv("GOT_TOKEN"))
	user := strings.TrimSpace(os.Getenv("GOT_USERNAME"))
	pass := os.Getenv("GOT_PASSWORD")
	if token == "" && user == "" && endpoint.user != "" {
		user = endpoint.user
		pass = endpoint.pass
	}

	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		token: token,
		user:  user,
		pass:  pass,
	}, nil
}

// Endpoint returns the parsed endpoint metadata.
func (c *Client) Endpoint() Endpoint {
	return c.endpoint
}

// ListRefs returns all remote refs (e.g. heads/main, tags/v1).
func (c *Client) ListRefs(ctx context.Context) (map[string]object.Hash, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint.BaseURL+"/refs", nil)
	if err != nil {
		return nil, err
	}
	body, err := c.do(req, http.StatusOK)
	if err != nil {
		return nil, err
	}
	var raw map[string]string
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode refs response: %w", err)
	}
	refs := make(map[string]object.Hash, len(raw))
	for name, hash := range raw {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		refs[name] = object.Hash(strings.TrimSpace(hash))
	}
	return refs, nil
}

// BatchObjects fetches missing objects reachable from wants and not in haves.
func (c *Client) BatchObjects(ctx context.Context, wants, haves []object.Hash, maxObjects int) ([]ObjectRecord, bool, error) {
	if len(wants) == 0 {
		return nil, false, fmt.Errorf("at least one want hash is required")
	}

	reqBody := struct {
		Wants      []string `json:"wants"`
		Haves      []string `json:"haves,omitempty"`
		MaxObjects int      `json:"max_objects,omitempty"`
	}{
		Wants:      make([]string, 0, len(wants)),
		Haves:      make([]string, 0, len(haves)),
		MaxObjects: maxObjects,
	}
	for _, h := range wants {
		if strings.TrimSpace(string(h)) != "" {
			reqBody.Wants = append(reqBody.Wants, string(h))
		}
	}
	for _, h := range haves {
		if strings.TrimSpace(string(h)) != "" {
			reqBody.Haves = append(reqBody.Haves, string(h))
		}
	}
	if len(reqBody.Wants) == 0 {
		return nil, false, fmt.Errorf("at least one non-empty want hash is required")
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.BaseURL+"/objects/batch", bytes.NewReader(payload))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	body, err := c.do(req, http.StatusOK)
	if err != nil {
		return nil, false, err
	}

	var resp struct {
		Objects []struct {
			Hash string `json:"hash"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		} `json:"objects"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, false, fmt.Errorf("decode batch response: %w", err)
	}

	out := make([]ObjectRecord, 0, len(resp.Objects))
	for _, obj := range resp.Objects {
		objType, err := parseObjectType(obj.Type)
		if err != nil {
			return nil, false, err
		}
		out = append(out, ObjectRecord{
			Hash: object.Hash(strings.TrimSpace(obj.Hash)),
			Type: objType,
			Data: obj.Data,
		})
	}
	return out, resp.Truncated, nil
}

// GetObject fetches one object by hash.
func (c *Client) GetObject(ctx context.Context, hash object.Hash) (ObjectRecord, error) {
	hash = object.Hash(strings.TrimSpace(string(hash)))
	if hash == "" {
		return ObjectRecord{}, fmt.Errorf("object hash is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint.BaseURL+"/objects/"+string(hash), nil)
	if err != nil {
		return ObjectRecord{}, err
	}
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ObjectRecord{}, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if readErr != nil {
		return ObjectRecord{}, readErr
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return ObjectRecord{}, fmt.Errorf("remote request failed (%s %s): %s", req.Method, req.URL.Path, msg)
	}

	objType, err := parseObjectType(strings.TrimSpace(resp.Header.Get("X-Object-Type")))
	if err != nil {
		return ObjectRecord{}, fmt.Errorf("decode object %s: %w", hash, err)
	}
	return ObjectRecord{
		Hash: hash,
		Type: objType,
		Data: body,
	}, nil
}

// PushObjects uploads objects using newline-delimited JSON payload.
func (c *Client) PushObjects(ctx context.Context, objects []ObjectRecord) error {
	if len(objects) == 0 {
		return nil
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i, obj := range objects {
		if _, err := parseObjectType(string(obj.Type)); err != nil {
			return fmt.Errorf("push object %d: %w", i, err)
		}
		computedHash := object.HashObject(obj.Type, obj.Data)
		if provided := object.Hash(strings.TrimSpace(string(obj.Hash))); provided != "" && provided != computedHash {
			return fmt.Errorf("push object %d: hash mismatch (provided %s, computed %s)", i, provided, computedHash)
		}
		payload := struct {
			Hash string `json:"hash"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		}{
			Hash: string(computedHash),
			Type: string(obj.Type),
			Data: obj.Data,
		}
		if err := enc.Encode(payload); err != nil {
			return fmt.Errorf("push object %d: encode: %w", i, err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.BaseURL+"/objects", bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	if _, err := c.do(req, http.StatusOK); err != nil {
		return err
	}
	return nil
}

// UpdateRefs applies atomic CAS updates on the remote refs.
func (c *Client) UpdateRefs(ctx context.Context, updates []RefUpdate) (map[string]object.Hash, error) {
	if len(updates) == 0 {
		return nil, fmt.Errorf("at least one ref update is required")
	}

	type refUpdatePayload struct {
		Name string  `json:"name"`
		Old  *string `json:"old,omitempty"`
		New  *string `json:"new"`
	}
	payload := struct {
		Updates []refUpdatePayload `json:"updates"`
	}{
		Updates: make([]refUpdatePayload, 0, len(updates)),
	}
	for _, u := range updates {
		name := strings.TrimSpace(u.Name)
		if name == "" {
			return nil, fmt.Errorf("ref update name is required")
		}
		var oldStr *string
		if u.Old != nil {
			v := strings.TrimSpace(string(*u.Old))
			oldStr = &v
		}
		var newStr *string
		if u.New != nil {
			v := strings.TrimSpace(string(*u.New))
			newStr = &v
		} else {
			empty := ""
			newStr = &empty
		}
		payload.Updates = append(payload.Updates, refUpdatePayload{
			Name: name,
			Old:  oldStr,
			New:  newStr,
		})
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.BaseURL+"/refs", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	body, err := c.do(req, http.StatusOK)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Updated map[string]string `json:"updated"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode ref update response: %w", err)
	}

	out := make(map[string]object.Hash, len(resp.Updated))
	for name, hash := range resp.Updated {
		out[name] = object.Hash(strings.TrimSpace(hash))
	}
	return out, nil
}

func (c *Client) do(req *http.Request, expectedStatus int) ([]byte, error) {
	c.applyAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
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
	return body, nil
}

func (c *Client) applyAuth(req *http.Request) {
	if strings.TrimSpace(c.token) != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		return
	}
	if strings.TrimSpace(c.user) != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
}

func parseObjectType(raw string) (object.ObjectType, error) {
	switch object.ObjectType(strings.TrimSpace(raw)) {
	case object.TypeBlob, object.TypeTree, object.TypeCommit, object.TypeEntity, object.TypeEntityList:
		return object.ObjectType(strings.TrimSpace(raw)), nil
	default:
		return "", fmt.Errorf("unsupported object type %q", raw)
	}
}
