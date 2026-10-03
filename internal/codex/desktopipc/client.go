package desktopipc

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

var (
	ErrClosed         = errors.New("IPC connection closed")
	ErrOverloaded     = errors.New("IPC bounded capacity exceeded; resync required")
	ErrRemoteRejected = errors.New("IPC owner rejected request")
	ErrProtocol       = errors.New("IPC protocol incompatible")
)

type Options struct {
	ClientType     string
	MaxFrameBytes  uint32
	MaxPending     int
	MaxEvents      int
	MaxEventBytes  int
	ThreadID       string
	RequestTimeout time.Duration
}

type Request struct {
	Method         string
	Version        int
	Params         any
	TargetClientID string
	Mutating       bool
}

type Reply struct {
	Type              string          `json:"type"`
	RequestID         string          `json:"requestId"`
	ResultType        string          `json:"resultType"`
	HandledByClientID string          `json:"handledByClientId"`
	Result            json.RawMessage `json:"result"`
}

// CallError intentionally excludes remote error text, which may contain user data.
type CallError struct {
	Cause   error
	Outcome string
}

func (e *CallError) Error() string {
	return fmt.Sprintf("IPC call: %v (outcome=%s)", e.Cause, e.Outcome)
}
func (e *CallError) Unwrap() error { return e.Cause }

type Client struct {
	conn        net.Conn
	opts        Options
	mu          sync.Mutex
	clientID    string
	pending     map[string]chan Reply
	err         error
	done        chan struct{}
	events      []json.RawMessage
	eventBytes  int
	eventsReady chan struct{}
	writer      chan struct{}
}

func NewClient(conn net.Conn, opts Options) *Client {
	if opts.MaxFrameBytes == 0 {
		opts.MaxFrameBytes = DefaultMaxFrameBytes
	}
	if opts.MaxPending <= 0 {
		opts.MaxPending = 32
	}
	if opts.MaxEvents <= 0 {
		opts.MaxEvents = 64
	}
	if opts.MaxEventBytes <= 0 {
		opts.MaxEventBytes = 16 << 20
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 10 * time.Second
	}
	c := &Client{conn: conn, opts: opts, clientID: "initializing-client", pending: make(map[string]chan Reply), done: make(chan struct{}), eventsReady: make(chan struct{}, 1), writer: make(chan struct{}, 1)}
	go c.readLoop()
	return c
}

func (c *Client) Done() <-chan struct{}        { return c.done }
func (c *Client) EventsReady() <-chan struct{} { return c.eventsReady }
func (c *Client) NextEvent() (json.RawMessage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil || len(c.events) == 0 {
		return nil, false
	}
	body := c.events[0]
	c.events[0] = nil
	c.events = c.events[1:]
	c.eventBytes -= len(body)
	if len(c.events) > 0 {
		select {
		case c.eventsReady <- struct{}{}:
		default:
		}
	}
	return body, true
}
func (c *Client) Err() error   { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func (c *Client) Close() error { c.fail(ErrClosed); return nil }

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
		c.events = nil
		c.eventBytes = 0
		close(c.done)
		c.conn.Close()
	}
	c.mu.Unlock()
}

// Initialize registers as an honest probe, never as an execution owner.
func (c *Client) Initialize(ctx context.Context) error {
	clientType := c.opts.ClientType
	if clientType == "" {
		clientType = "ariel-compatibility-probe"
	}
	r, err := c.Call(ctx, Request{Method: "initialize", Version: 0, Params: map[string]string{"clientType": clientType}})
	if err != nil {
		return err
	}
	var result struct {
		ClientID string `json:"clientId"`
	}
	if json.Unmarshal(r.Result, &result) != nil || result.ClientID == "" {
		c.fail(ErrProtocol)
		return ErrProtocol
	}
	c.mu.Lock()
	c.clientID = result.ClientID
	c.mu.Unlock()
	return nil
}

func (c *Client) Call(ctx context.Context, req Request) (Reply, error) {
	ctx, cancel := context.WithTimeout(ctx, c.opts.RequestTimeout)
	defer cancel()
	outcome := "not_submitted"
	failure := func(err error) (Reply, error) { return Reply{}, &CallError{Cause: err, Outcome: outcome} }
	if ctx.Err() != nil {
		return failure(ctx.Err())
	}
	if req.Method == "" {
		return failure(ErrProtocol)
	}
	id := requestID()
	ch := make(chan Reply, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return failure(err)
	}
	if len(c.pending) >= c.opts.MaxPending {
		c.mu.Unlock()
		return failure(ErrOverloaded)
	}
	c.pending[id] = ch
	clientID := c.clientID
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	deadline, _ := ctx.Deadline()
	message := struct {
		Type           string `json:"type"`
		RequestID      string `json:"requestId"`
		SourceClientID string `json:"sourceClientId"`
		Version        int    `json:"version"`
		Method         string `json:"method"`
		Params         any    `json:"params"`
		TargetClientID string `json:"targetClientId,omitempty"`
		TimeoutMS      int64  `json:"timeoutMs"`
	}{"request", id, clientID, req.Version, req.Method, req.Params, req.TargetClientID, max(1, time.Until(deadline).Milliseconds())}
	body, err := json.Marshal(message)
	if err != nil {
		return failure(ErrInvalidFrame)
	}
	attempted, err := c.write(ctx, body)
	if attempted {
		outcome = "unknown"
	}
	if err != nil {
		return failure(err)
	}
	select {
	case r := <-ch:
		if req.TargetClientID != "" && r.HandledByClientID != req.TargetClientID {
			c.fail(ErrProtocol)
			return failure(ErrProtocol)
		}
		if r.ResultType == "error" {
			outcome = "rejected"
			return failure(ErrRemoteRejected)
		}
		if r.ResultType != "success" {
			c.fail(ErrProtocol)
			return failure(ErrProtocol)
		}
		return r, nil
	case <-ctx.Done():
		return failure(ctx.Err())
	case <-c.done:
		return failure(c.Err())
	}
}

// Broadcast is a transport primitive, not an arbitrary remote API for Web clients.
func (c *Client) Broadcast(ctx context.Context, method string, version int, params any, targets []string) error {
	c.mu.Lock()
	clientID := c.clientID
	c.mu.Unlock()
	body, err := json.Marshal(map[string]any{"type": "broadcast", "sourceClientId": clientID, "method": method, "version": version, "params": params, "targetClientIds": targets})
	if err != nil {
		return ErrInvalidFrame
	}
	_, err = c.write(ctx, body)
	return err
}

func (c *Client) write(ctx context.Context, body json.RawMessage) (bool, error) {
	if uint64(len(body)) > uint64(c.opts.MaxFrameBytes) {
		return false, ErrFrameTooLarge
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.RequestTimeout)
	defer cancel()
	select {
	case c.writer <- struct{}{}:
	case <-ctx.Done():
		return false, ctx.Err()
	case <-c.done:
		return false, c.Err()
	}
	defer func() { <-c.writer }()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err := c.Err(); err != nil {
		return false, err
	}
	deadline, _ := ctx.Deadline()
	c.conn.SetWriteDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { c.conn.SetWriteDeadline(time.Now()) })
	err := WriteFrame(c.conn, body, c.opts.MaxFrameBytes)
	if !stop() { // A canceled partial write poisons framing; never reuse the connection.
		c.fail(context.Canceled)
	}
	if err != nil {
		c.fail(ErrClosed)
		return true, ErrClosed
	}
	return true, nil
}

func (c *Client) readLoop() {
	for {
		body, err := ReadFrame(c.conn, c.opts.MaxFrameBytes)
		if err != nil {
			c.fail(err)
			return
		}
		var r Reply
		if json.Unmarshal(body, &r) != nil {
			c.fail(ErrProtocol)
			return
		}
		switch r.Type {
		case "response":
			c.mu.Lock()
			ch := c.pending[r.RequestID]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- r:
				default:
				}
			}
		case "client-discovery-request":
			if r.RequestID == "" {
				c.fail(ErrProtocol)
				return
			}
			reply, _ := json.Marshal(map[string]any{"type": "client-discovery-response", "requestId": r.RequestID, "response": map[string]bool{"canHandle": false}})
			if _, err := c.write(context.Background(), reply); err != nil {
				return
			}
		case "broadcast":
			if !c.wantsEvent(body) {
				continue
			}
			c.mu.Lock()
			if len(c.events) >= c.opts.MaxEvents || len(body) > c.opts.MaxEventBytes-c.eventBytes {
				c.mu.Unlock()
				c.fail(ErrOverloaded)
				return
			}
			c.events = append(c.events, body)
			c.eventBytes += len(body)
			select {
			case c.eventsReady <- struct{}{}:
			default:
			}
			c.mu.Unlock()
		default:
			c.fail(ErrProtocol)
			return
		}
	}
}

func (c *Client) wantsEvent(body json.RawMessage) bool {
	if c.opts.ThreadID == "" {
		return true
	}
	var event struct {
		Method string `json:"method"`
		Params struct {
			ThreadID string `json:"conversationId"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &event) != nil {
		return true
	}
	return event.Method == "ipc-connection-reset" || (event.Method == "thread-stream-state-changed" && event.Params.ThreadID == c.opts.ThreadID)
}

func requestID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
