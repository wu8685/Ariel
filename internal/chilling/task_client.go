package chilling

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	"github.com/wu8685/Ariel/protocol"
)

type TaskCapability struct {
	Contract     string `json:"contract"`
	Enabled      bool   `json:"enabled"`
	Ready        bool   `json:"ready"`
	TaskStoreID  string `json:"taskStoreId"`
	BuildVersion string `json:"buildVersion"`
}
type TaskClient struct {
	mu         sync.RWMutex
	transport  *Client
	capability TaskCapability
}

func NewTaskClient(ctx context.Context, cfg Config) (*TaskClient, error) {
	if !cfg.Enabled {
		return nil, &Error{Code: "FEATURE_DISABLED"}
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Scheme != "unix" || u.Host != "" || !filepath.IsAbs(u.Path) || u.RawQuery != "" || u.Fragment != "" {
		return nil, &Error{Code: "CHILLING_UNAVAILABLE"}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: timeout}).DialContext(ctx, "unix", u.Path)
	}, MaxIdleConns: 4, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second}
	c := &TaskClient{transport: &Client{http: &http.Client{Transport: taskPrincipalTransport{transport}, Timeout: timeout}}}
	return c, c.Refresh(ctx)
}

type taskPrincipalTransport struct{ base http.RoundTripper }

func (t taskPrincipalTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("X-Chilling-User-ID", protocol.TaskUserID)
	return t.base.RoundTrip(r)
}
func (c *TaskClient) Ready() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.capability.Contract == protocol.TaskContract && c.capability.Enabled && c.capability.Ready
}
func (c *TaskClient) TaskStoreID() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.capability.TaskStoreID
}
func (c *TaskClient) Refresh(ctx context.Context) error {
	var capability TaskCapability
	if err := c.transport.call(ctx, http.MethodGet, "/v1/tasks/capabilities", nil, &capability); err != nil {
		c.mu.Lock()
		c.capability.Ready = false
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.capability = capability
	c.mu.Unlock()
	if capability.Contract != protocol.TaskContract {
		return &Error{Code: "CONTRACT_UNSUPPORTED"}
	}
	if !c.Ready() || capability.TaskStoreID == "" {
		return &Error{Code: "CHILLING_UNAVAILABLE"}
	}
	return nil
}
func (c *TaskClient) Call(ctx context.Context, method string, params any, output any) error {
	if c == nil || !c.Ready() {
		return &Error{Code: "CHILLING_UNAVAILABLE"}
	}
	if !protocol.IsTaskMethod(method) {
		switch method {
		case "task.command.claim", "task.command.tracking", "task.command.validate", "task.command.record", "task.fact.record":
		default:
			return &Error{Code: "INVALID_ARGUMENT"}
		}
	}
	body, err := json.Marshal(params)
	if err != nil || len(body) > protocol.MaxTaskBytes {
		return &Error{Code: "INVALID_ARGUMENT"}
	}
	if protocol.IsTaskMethod(method) {
		var scope struct {
			TaskStoreID string `json:"taskStoreId"`
		}
		if json.Unmarshal(body, &scope) != nil || scope.TaskStoreID != c.TaskStoreID() {
			return errors.New("task store mismatch")
		}
	}
	return c.transport.call(ctx, http.MethodPost, "/v1/tasks/rpc", map[string]any{"method": method, "params": params}, output)
}
