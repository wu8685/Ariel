package chilling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"
)

const contract = "file-context/v1"
const maxResponseBytes = (1 << 20) + (256 << 10)

type Config struct {
	Enabled  bool
	Endpoint string
	Timeout  time.Duration
}
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code }
func CodeOf(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return "INTERNAL"
}

type Limits struct {
	MaxTurnBatch      int `json:"maxTurnBatch"`
	MaxFileBytes      int `json:"maxFileBytes"`
	MaxSelections     int `json:"maxSelections"`
	MaxSelectionBytes int `json:"maxSelectionBytes"`
	MaxTotalBytes     int `json:"maxTotalBytes"`
}
type Capability struct {
	Contract          string `json:"contract"`
	Enabled           bool   `json:"enabled"`
	Ready             bool   `json:"ready"`
	MessageReferences bool   `json:"messageReferences"`
	WorkspaceBrowser  bool   `json:"workspaceBrowser"`
	Limits            Limits `json:"limits"`
}
type Client struct {
	http       *http.Client
	capability Capability
}

func New(ctx context.Context, cfg Config) (*Client, error) {
	if !cfg.Enabled {
		return nil, &Error{Code: "FEATURE_DISABLED", Message: "FEATURE_DISABLED"}
	}
	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil || parsed.Scheme != "unix" || parsed.Host != "" || !filepath.IsAbs(parsed.Path) || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, &Error{Code: "CHILLING_UNAVAILABLE", Message: "invalid Chilling endpoint"}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	socket := parsed.Path
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: timeout}).DialContext(ctx, "unix", socket)
	}, MaxIdleConns: 2, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}
	client := &Client{http: &http.Client{Transport: transport, Timeout: timeout}}
	if err := client.call(ctx, http.MethodGet, "/v1/file-context/capabilities", nil, &client.capability); err != nil {
		return nil, err
	}
	if client.capability.Contract != contract {
		return nil, &Error{Code: "CONTRACT_UNSUPPORTED", Message: "unsupported Chilling contract"}
	}
	if !client.capability.Enabled || !client.capability.Ready {
		return nil, &Error{Code: "CHILLING_UNAVAILABLE", Message: "Chilling file context not ready"}
	}
	if client.capability.Limits.MaxTurnBatch < 1 || client.capability.Limits.MaxFileBytes < 1 || client.capability.Limits.MaxSelections < 1 {
		return nil, &Error{Code: "CONTRACT_UNSUPPORTED", Message: "invalid Chilling limits"}
	}
	return client, nil
}
func (c *Client) Ready() bool { return c != nil && c.capability.Ready }
func (c *Client) MessageReferences() bool {
	return c != nil && c.capability.Ready && c.capability.MessageReferences
}
func (c *Client) WorkspaceBrowser() bool {
	return c != nil && c.capability.Ready && c.capability.WorkspaceBrowser
}

type ChangesRequest struct {
	ThreadID string   `json:"threadId"`
	TurnIDs  []string `json:"turnIds"`
}
type FileChange struct {
	ItemID        string `json:"itemId"`
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	Diff          string `json:"diff,omitempty"`
	DiffTruncated bool   `json:"diffTruncated,omitempty"`
}
type TurnChanges struct {
	TurnID      string       `json:"turnId"`
	FileChanges []FileChange `json:"fileChanges"`
}
type ChangesResponse struct {
	Turns []TurnChanges `json:"turns"`
}
type ReadRequest struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Path     string `json:"path"`
}
type ReadResponse struct {
	Path          string `json:"path"`
	Content       string `json:"content"`
	SHA256        string `json:"sha256"`
	ModifiedAtMS  int64  `json:"modifiedAtMs"`
	LineCount     int    `json:"lineCount"`
	Language      string `json:"language"`
	Diff          string `json:"diff,omitempty"`
	DiffTruncated bool   `json:"diffTruncated,omitempty"`
	Version       string `json:"version"`
}
type ReferenceReadRequest struct {
	ThreadID      string `json:"threadId"`
	TurnID        string `json:"turnId"`
	ItemID        string `json:"itemId"`
	ReferenceKind string `json:"referenceKind"`
	RawReference  string `json:"rawReference"`
	StartLine     int    `json:"startLine,omitempty"`
	EndLine       int    `json:"endLine,omitempty"`
}
type Selection struct {
	Source        string `json:"source,omitempty"`
	ThreadID      string `json:"threadId"`
	TurnID        string `json:"turnId"`
	ItemID        string `json:"itemId"`
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	ModifiedAtMS  int64  `json:"modifiedAtMs"`
	StartLine     int    `json:"startLine"`
	EndLine       int    `json:"endLine"`
	Text          string `json:"text"`
	ReferenceKind string `json:"referenceKind,omitempty"`
	RawReference  string `json:"rawReference,omitempty"`
}
type WorkspaceListRequest struct {
	ThreadID string `json:"threadId"`
	Path     string `json:"path"`
	Cursor   string `json:"cursor,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}
type WorkspaceEntry struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Kind         string `json:"kind"`
	SizeBytes    int64  `json:"sizeBytes"`
	ModifiedAtMS int64  `json:"modifiedAtMs"`
	Readable     bool   `json:"readable"`
}
type WorkspaceListResponse struct {
	Path       string           `json:"path"`
	Entries    []WorkspaceEntry `json:"entries"`
	NextCursor string           `json:"nextCursor,omitempty"`
	Revision   string           `json:"revision"`
}
type WorkspaceReadRequest struct {
	ThreadID string `json:"threadId"`
	Path     string `json:"path"`
}
type PrepareRequest struct {
	Selections []Selection `json:"selections"`
}
type Quote struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	SHA256    string `json:"sha256"`
	Text      string `json:"text"`
}
type PrepareResponse struct {
	Envelope string  `json:"envelope"`
	Quotes   []Quote `json:"quotes"`
}

func (c *Client) Changes(ctx context.Context, request ChangesRequest) (ChangesResponse, error) {
	var out ChangesResponse
	err := c.call(ctx, http.MethodPost, "/v1/file-context/changes", request, &out)
	return out, err
}
func (c *Client) Read(ctx context.Context, request ReadRequest) (ReadResponse, error) {
	var out ReadResponse
	err := c.call(ctx, http.MethodPost, "/v1/file-context/read", request, &out)
	return out, err
}
func (c *Client) ReferenceRead(ctx context.Context, request ReferenceReadRequest) (ReadResponse, error) {
	var out ReadResponse
	err := c.call(ctx, http.MethodPost, "/v1/file-context/reference/read", request, &out)
	return out, err
}
func (c *Client) WorkspaceList(ctx context.Context, request WorkspaceListRequest) (WorkspaceListResponse, error) {
	var out WorkspaceListResponse
	err := c.call(ctx, http.MethodPost, "/v1/file-context/workspace/files/list", request, &out)
	return out, err
}
func (c *Client) WorkspaceRead(ctx context.Context, request WorkspaceReadRequest) (ReadResponse, error) {
	var out ReadResponse
	err := c.call(ctx, http.MethodPost, "/v1/file-context/workspace/file/read", request, &out)
	return out, err
}
func (c *Client) Prepare(ctx context.Context, request PrepareRequest) (PrepareResponse, error) {
	var out PrepareResponse
	err := c.call(ctx, http.MethodPost, "/v1/file-context/prepare", request, &out)
	return out, err
}

func (c *Client) call(ctx context.Context, method, path string, input, output any) error {
	if c == nil || c.http == nil {
		return &Error{Code: "CHILLING_UNAVAILABLE", Message: "Chilling client unavailable"}
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return &Error{Code: "INVALID_ARGUMENT", Message: "invalid request"}
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://chilling"+path, body)
	if err != nil {
		return &Error{Code: "INVALID_ARGUMENT", Message: "invalid request"}
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return &Error{Code: "CHILLING_UNAVAILABLE", Message: "Chilling unavailable"}
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil || len(data) > maxResponseBytes {
		return &Error{Code: "CHILLING_UNAVAILABLE", Message: "Chilling response too large"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var remote Error
		if json.Unmarshal(data, &remote) == nil && remote.Code != "" {
			return &remote
		}
		return &Error{Code: "CHILLING_UNAVAILABLE", Message: fmt.Sprintf("Chilling HTTP %d", response.StatusCode)}
	}
	if output != nil && json.Unmarshal(data, output) != nil {
		return &Error{Code: "CHILLING_UNAVAILABLE", Message: "invalid Chilling response"}
	}
	return nil
}
