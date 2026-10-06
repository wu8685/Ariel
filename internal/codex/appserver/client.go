// Package appserver isolates the public JSONL App Server protocol from Desktop IPC.
package appserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	ErrClosed            = errors.New("App Server connection closed")
	ErrProtocol          = errors.New("App Server protocol invalid or response too large")
	ErrRemote            = errors.New("App Server rejected request")
	ErrMethodUnavailable = errors.New("PROTOCOL_UNSUPPORTED")
	ErrInvalidArgument   = errors.New("INVALID_ARGUMENT")
	ErrCapacity          = errors.New("App Server pending request limit reached")
	ErrResponseTooLarge  = errors.New("HISTORY_TOO_LARGE")
	ErrOutcomeUnknown    = errors.New("OUTCOME_UNKNOWN")
)

const DefaultMaxResponseBytes = 64 << 20

var oversizedResponseID = regexp.MustCompile(`^\s*\{\s*"id"\s*:\s*([0-9]+)\s*[,}]`)

type response struct {
	ID       json.RawMessage `json:"id"`
	Method   string          `json:"method"`
	Result   json.RawMessage `json:"result"`
	Error    json.RawMessage `json:"error"`
	TooLarge bool            `json:"-"`
}

// Session is deliberately read-oriented: unsolicited interactive requests are
// rejected instead of being approved. Production approvals go to the Desktop owner.
type Session struct {
	rw      io.ReadWriteCloser
	limit   int
	mu      sync.Mutex
	err     error
	next    uint64
	pending map[string]chan response
	done    chan struct{}
	writer  chan struct{}
}

func NewSession(rw io.ReadWriteCloser, limit int) *Session {
	if limit <= 0 {
		limit = DefaultMaxResponseBytes
	}
	s := &Session{rw: rw, limit: limit, pending: make(map[string]chan response), done: make(chan struct{}), writer: make(chan struct{}, 1)}
	go s.readLoop()
	return s
}
func (s *Session) Done() <-chan struct{} { return s.done }
func (s *Session) Err() error            { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *Session) Close() error          { s.fail(ErrClosed); return nil }
func (s *Session) fail(err error) {
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return
	}
	s.err = err
	close(s.done)
	s.mu.Unlock()
	s.rw.Close()
}

func (s *Session) Call(ctx context.Context, method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
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
			}
			return ErrRemote
		}
		if len(r.Result) == 0 {
			return ErrProtocol
		}
		if result != nil && json.Unmarshal(r.Result, result) != nil {
			return ErrProtocol
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.Err()
	}
}
func (s *Session) Initialize(ctx context.Context) error {
	if err := s.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "ariel_probe", "title": "Ariel compatibility probe", "version": "0.1.0"}, "capabilities": map[string]bool{"experimentalApi": true}}, nil); err != nil {
		return err
	}
	return s.send(ctx, map[string]any{"method": "initialized", "params": map[string]any{}})
}
func (s *Session) send(ctx context.Context, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil || len(body) > s.limit {
		return ErrProtocol
	}
	select {
	case s.writer <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.Err()
	}
	defer func() { <-s.writer }()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	stop := context.AfterFunc(ctx, func() { s.fail(context.Canceled) })
	defer stop()
	body = append(body, '\n')
	for len(body) > 0 {
		n, err := s.rw.Write(body)
		if err != nil || n <= 0 || n > len(body) {
			s.fail(ErrClosed)
			return ErrClosed
		}
		body = body[n:]
	}
	return nil
}
func (s *Session) readLoop() {
	reader := bufio.NewReaderSize(s.rw, 32<<10)
	for {
		body, prefix, tooLarge, readErr := readBoundedLine(reader, s.limit)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				s.fail(ErrClosed)
			} else {
				s.fail(ErrProtocol)
			}
			return
		}
		if tooLarge {
			match := oversizedResponseID.FindSubmatch(prefix)
			if len(match) != 2 {
				s.fail(ErrProtocol)
				return
			}
			s.mu.Lock()
			ch := s.pending[string(match[1])]
			s.mu.Unlock()
			if ch == nil {
				s.fail(ErrProtocol)
				return
			}
			select {
			case ch <- response{TooLarge: true}:
			default:
			}
			continue
		}
		var r response
		if !utf8.Valid(body) || json.Unmarshal(body, &r) != nil {
			s.fail(ErrProtocol)
			return
		}
		if r.Method != "" {
			if len(r.ID) > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				err := s.send(ctx, map[string]any{"id": r.ID, "error": map[string]any{"code": -32601, "message": "Read-only probe does not handle interactive requests"}})
				cancel()
				if err != nil {
					return
				}
			}
			continue
		}
		if len(r.ID) == 0 {
			s.fail(ErrProtocol)
			return
		}
		s.mu.Lock()
		ch := s.pending[string(r.ID)]
		s.mu.Unlock()
		if ch != nil {
			select {
			case ch <- r:
			default:
			}
		}
	}
}

// readBoundedLine drains oversize JSONL without retaining the whole response.
// The prefix is only used to correlate a response whose numeric id is first.
func readBoundedLine(reader *bufio.Reader, limit int) ([]byte, []byte, bool, error) {
	var body, prefix []byte
	tooLarge := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(prefix) < 256 {
			prefix = append(prefix, fragment[:min(len(fragment), 256-len(prefix))]...)
		}
		if !tooLarge {
			withoutNewline := fragment
			if err == nil {
				withoutNewline = bytes.TrimSuffix(fragment, []byte{'\n'})
			}
			if len(withoutNewline) > limit-len(body) {
				tooLarge = true
				body = nil
			} else {
				body = append(body, withoutNewline...)
			}
		}
		if err == nil || (errors.Is(err, io.EOF) && len(fragment) > 0) {
			return body, prefix, tooLarge, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, prefix, tooLarge, err
		}
	}
}

type pipes struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p pipes) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipes) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipes) Close() error                { p.w.Close(); return p.r.Close() }

type Process struct {
	*Session
	cmd    *exec.Cmd
	exited chan struct{}
}

func Start(ctx context.Context, binary, cwd string) (*Process, error) {
	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://")
	cmd.Dir = cwd
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, errors.New("failed to start configured Codex binary")
	}
	p := &Process{Session: NewSession(pipes{stdout, stdin}, 0), cmd: cmd, exited: make(chan struct{})}
	go func() { cmd.Wait(); p.Session.fail(ErrClosed); close(p.exited) }()
	if err := p.Initialize(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}
func (p *Process) Close() error {
	p.Session.Close()
	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
		p.cmd.Process.Kill()
		<-p.exited
	}
	return nil
}
