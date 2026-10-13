package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
	"unicode/utf8"
)

const interactiveIncomingLimit = 64

// IncomingFrame is an App Server notification or server-initiated request.
// A non-empty ID distinguishes requests that require an explicit reply.
type IncomingFrame struct {
	ID     json.RawMessage
	Method string
	Params json.RawMessage
}

func (f IncomingFrame) IsRequest() bool { return len(f.ID) > 0 && string(f.ID) != "null" }

// InteractiveSession is intentionally separate from Session. Session remains
// read-only and rejects interactive requests; this transport exposes them to a
// bounded bootstrap owner without ever approving one implicitly.
type InteractiveSession struct {
	rw       io.ReadWriteCloser
	limit    int
	mu       sync.Mutex
	err      error
	next     uint64
	pending  map[string]chan response
	done     chan struct{}
	incoming chan IncomingFrame
	writeMu  sync.Mutex
}

func NewInteractiveSession(rw io.ReadWriteCloser, limit int) *InteractiveSession {
	if limit <= 0 {
		limit = DefaultMaxResponseBytes
	}
	s := &InteractiveSession{rw: rw, limit: limit, pending: map[string]chan response{}, done: make(chan struct{}), incoming: make(chan IncomingFrame, interactiveIncomingLimit)}
	go s.readLoop()
	return s
}

func (s *InteractiveSession) Incoming() <-chan IncomingFrame { return s.incoming }
func (s *InteractiveSession) Done() <-chan struct{}          { return s.done }
func (s *InteractiveSession) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
func (s *InteractiveSession) Close() error { s.fail(ErrClosed); return nil }

func (s *InteractiveSession) fail(err error) {
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return
	}
	s.err = err
	close(s.done)
	s.mu.Unlock()
	_ = s.rw.Close()
}

func (s *InteractiveSession) Call(ctx context.Context, method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s.mu.Lock()
	if s.err != nil {
		err := s.err
		s.mu.Unlock()
		return err
	}
	if len(s.pending) >= 32 {
		s.mu.Unlock()
		return ErrCapacity
	}
	s.next++
	id := s.next
	key := fmt.Sprint(id)
	ch := make(chan response, 1)
	s.pending[key] = ch
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, key); s.mu.Unlock() }()
	if err := s.send(ctx, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case r := <-ch:
		return decodeResponse(r, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.Err()
	}
}

func (s *InteractiveSession) Initialize(ctx context.Context) error {
	if err := s.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "ariel", "title": "Ariel", "version": "0.1.0"}, "capabilities": map[string]bool{"experimentalApi": true}}, nil); err != nil {
		return err
	}
	return s.send(ctx, map[string]any{"method": "initialized", "params": map[string]any{}})
}

func (s *InteractiveSession) Reply(ctx context.Context, id json.RawMessage, result any, rpcErr any) error {
	if len(id) == 0 || string(id) == "null" {
		return ErrInvalidArgument
	}
	message := map[string]any{"id": id}
	if rpcErr != nil {
		message["error"] = rpcErr
	} else {
		message["result"] = result
	}
	return s.send(ctx, message)
}

func decodeResponse(r response, result any) error {
	if r.TooLarge {
		return ErrResponseTooLarge
	}
	if len(r.Error) > 0 && string(r.Error) != "null" {
		var remote struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(r.Error, &remote) != nil {
			return ErrProtocol
		}
		switch remote.Code {
		case -32601:
			return ErrMethodUnavailable
		case -32602:
			return ErrInvalidArgument
		default:
			return ErrRemote
		}
	}
	if len(r.Result) == 0 {
		return ErrProtocol
	}
	if result != nil && json.Unmarshal(r.Result, result) != nil {
		return ErrProtocol
	}
	return nil
}

func (s *InteractiveSession) send(ctx context.Context, message any) error {
	body, err := json.Marshal(message)
	if err != nil || len(body) > s.limit {
		return ErrProtocol
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.Err()
	default:
	}
	body = append(body, '\n')
	for len(body) > 0 {
		n, writeErr := s.rw.Write(body)
		if writeErr != nil || n <= 0 || n > len(body) {
			s.fail(ErrClosed)
			return ErrClosed
		}
		body = body[n:]
	}
	return nil
}

func (s *InteractiveSession) readLoop() {
	reader := bufio.NewReaderSize(s.rw, 32<<10)
	for {
		body, prefix, tooLarge, err := readBoundedLine(reader, s.limit)
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.fail(ErrClosed)
			} else {
				s.fail(ErrProtocol)
			}
			return
		}
		if tooLarge {
			match := oversizedResponseID.FindSubmatch(prefix)
			if len(match) == 2 {
				s.mu.Lock()
				ch := s.pending[string(match[1])]
				s.mu.Unlock()
				if ch != nil {
					select {
					case ch <- response{TooLarge: true}:
					default:
					}
					continue
				}
			}
			s.fail(ErrProtocol)
			return
		}
		var frame response
		if !utf8.Valid(body) || json.Unmarshal(body, &frame) != nil {
			s.fail(ErrProtocol)
			return
		}
		if frame.Method != "" {
			incoming := IncomingFrame{ID: append(json.RawMessage(nil), frame.ID...), Method: frame.Method, Params: append(json.RawMessage(nil), frame.Result...)}
			var raw struct {
				Params json.RawMessage `json:"params"`
			}
			if json.Unmarshal(body, &raw) != nil {
				s.fail(ErrProtocol)
				return
			}
			incoming.Params = raw.Params
			select {
			case s.incoming <- incoming:
			default:
				s.fail(ErrCapacity)
				return
			}
			continue
		}
		if len(frame.ID) == 0 {
			s.fail(ErrProtocol)
			return
		}
		s.mu.Lock()
		ch := s.pending[string(frame.ID)]
		s.mu.Unlock()
		if ch != nil {
			select {
			case ch <- frame:
			default:
			}
		}
	}
}

type InteractiveProcess struct {
	*InteractiveSession
	cmd    *exec.Cmd
	exited chan struct{}
}

func StartInteractive(ctx context.Context, binary, cwd string) (*InteractiveProcess, error) {
	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://")
	cmd.Dir = cwd
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, errors.New("failed to start configured Codex binary")
	}
	p := &InteractiveProcess{InteractiveSession: NewInteractiveSession(pipes{stdout, stdin}, 0), cmd: cmd, exited: make(chan struct{})}
	go func() { _ = cmd.Wait(); p.InteractiveSession.fail(ErrClosed); close(p.exited) }()
	if err := p.Initialize(ctx); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

func (p *InteractiveProcess) Close() error {
	_ = p.InteractiveSession.Close()
	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.exited
	}
	return nil
}
