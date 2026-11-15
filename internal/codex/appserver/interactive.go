package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"
)

const (
	interactiveIncomingByteLimit  = 64 << 20
	interactiveIncomingFrameBytes = int(unsafe.Sizeof(IncomingFrame{}))
	interactiveExitDiagnosticWait = time.Second
)

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
// bounded bootstrap owner without ever approving one implicitly. Incoming frame
// metadata and payload storage are bounded by the same 64 MiB maximum as one
// transport frame.
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

	incomingMu    sync.Mutex
	incomingQueue []IncomingFrame
	incomingHead  int
	incomingBytes int
	incomingReady chan struct{}
}

func NewInteractiveSession(rw io.ReadWriteCloser, limit int) *InteractiveSession {
	if limit <= 0 {
		limit = DefaultMaxResponseBytes
	}
	s := &InteractiveSession{rw: rw, limit: limit, pending: map[string]chan response{}, done: make(chan struct{}), incoming: make(chan IncomingFrame), incomingReady: make(chan struct{}, 1)}
	go s.readLoop()
	go s.deliverIncoming()
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

func incomingFrameBytes(frame IncomingFrame) int {
	return interactiveIncomingFrameBytes + len(frame.ID) + len(frame.Method) + len(frame.Params)
}

func (s *InteractiveSession) compactIncoming() {
	remaining := copy(s.incomingQueue, s.incomingQueue[s.incomingHead:])
	clear(s.incomingQueue[remaining:])
	s.incomingQueue = s.incomingQueue[:remaining]
	s.incomingHead = 0
}

// The reader only appends to this bounded FIFO. A separate delivery goroutine
// may wait for the consumer without delaying responses behind notifications.
func (s *InteractiveSession) enqueueIncoming(frame IncomingFrame) error {
	size := incomingFrameBytes(frame)
	s.incomingMu.Lock()
	defer s.incomingMu.Unlock()
	select {
	case <-s.done:
		return s.Err()
	default:
	}
	if size > interactiveIncomingByteLimit-s.incomingBytes {
		return ErrCapacity
	}
	if s.incomingHead > 0 {
		s.compactIncoming()
	}
	s.incomingQueue = append(s.incomingQueue, frame)
	s.incomingBytes += size
	select {
	case s.incomingReady <- struct{}{}:
	default:
	}
	return nil
}

func (s *InteractiveSession) deliverIncoming() {
	for {
		s.incomingMu.Lock()
		if s.incomingHead == len(s.incomingQueue) {
			s.incomingQueue = nil
			s.incomingHead = 0
			s.incomingMu.Unlock()
			select {
			case <-s.incomingReady:
			case <-s.done:
				return
			}
			continue
		}
		frame := s.incomingQueue[s.incomingHead]
		s.incomingMu.Unlock()
		select {
		case s.incoming <- frame:
			s.incomingMu.Lock()
			s.incomingQueue[s.incomingHead] = IncomingFrame{}
			s.incomingHead++
			s.incomingBytes -= incomingFrameBytes(frame)
			if s.incomingHead == len(s.incomingQueue) {
				s.incomingQueue = nil
				s.incomingHead = 0
			} else if s.incomingHead >= len(s.incomingQueue)-s.incomingHead {
				s.compactIncoming()
			}
			s.incomingMu.Unlock()
		case <-s.done:
			return
		}
	}
}

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
			if err := s.enqueueIncoming(incoming); err != nil {
				s.fail(err)
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
	ctx    context.Context
	exited chan struct{}
}

type InteractiveProcessExitDiagnostic struct {
	Kind             string
	ExitCode         int
	Signal           string
	Transport        string
	ContextCancelled bool
}

func newInteractiveProcessExitDiagnostic(ctx context.Context, state *os.ProcessState, sessionErr error, exited bool) InteractiveProcessExitDiagnostic {
	diagnostic := InteractiveProcessExitDiagnostic{Kind: "none", ExitCode: -1, Transport: ErrorClass(sessionErr)}
	if diagnostic.Transport == "" {
		diagnostic.Transport = "none"
		if !exited {
			diagnostic.Transport = "closed"
		}
	}
	if ctx != nil {
		diagnostic.ContextCancelled = ctx.Err() != nil
	}
	if !exited || state == nil {
		return diagnostic
	}
	diagnostic.ExitCode = state.ExitCode()
	diagnostic.Kind = "exit"
	if status, ok := processWaitStatus(state); ok && status.signaled() {
		diagnostic.Kind = "signal"
		diagnostic.Signal = status.signal()
	}
	return diagnostic
}

func (p *InteractiveProcess) ExitDiagnostic() InteractiveProcessExitDiagnostic {
	select {
	case <-p.Done():
		timer := time.NewTimer(interactiveExitDiagnosticWait)
		defer timer.Stop()
		select {
		case <-p.exited:
		case <-timer.C:
			return newInteractiveProcessExitDiagnostic(p.ctx, nil, p.Err(), false)
		}
	default:
		return newInteractiveProcessExitDiagnostic(p.ctx, nil, p.Err(), false)
	}
	return newInteractiveProcessExitDiagnostic(p.ctx, p.cmd.ProcessState, p.Err(), true)
}

func StartInteractive(ctx context.Context, binary, cwd string) (*InteractiveProcess, error) {
	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://")
	return startInteractiveCommand(ctx, cmd, cwd)
}

func StartBrainInteractive(ctx context.Context, binary, cwd string) (*InteractiveProcess, error) {
	discoveryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	discovery := exec.CommandContext(discoveryCtx, binary, "features", "list")
	discovery.Dir = cwd
	featureOutput, err := discovery.Output()
	if err != nil {
		return nil, errors.New("failed to inspect configured Codex features")
	}
	args, err := brainAppServerArgs(featureOutput)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	return startInteractiveCommand(ctx, cmd, cwd)
}

func brainAppServerArgs(featureOutput []byte) ([]string, error) {
	features := map[string]bool{}
	for _, line := range strings.Split(string(featureOutput), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			features[fields[0]] = fields[1] != "removed"
		}
	}
	// Dynamic Puck tools are exposed through code_mode_host. Local Codex tools
	// stay available so the brain can run commands, files, and automations.
	if enabled, known := features["code_mode_host"]; !known || !enabled {
		return nil, errors.New("configured Codex is missing code_mode_host")
	}
	return []string{"app-server", "--listen", "stdio://"}, nil
}

func startInteractiveCommand(ctx context.Context, cmd *exec.Cmd, cwd string) (*InteractiveProcess, error) {
	cmd.Dir = cwd
	for _, value := range os.Environ() {
		if utf8.ValidString(value) {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Stderr = appServerDiagnosticWriter()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := startCommand(cmd); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, errors.New("failed to start configured Codex binary")
	}
	p := &InteractiveProcess{InteractiveSession: NewInteractiveSession(pipes{stdout, stdin}, 0), cmd: cmd, ctx: ctx, exited: make(chan struct{})}
	go func() { _ = cmd.Wait(); p.InteractiveSession.fail(ErrClosed); close(p.exited) }()
	if err := p.Initialize(ctx); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

func (p *InteractiveProcess) Close() error {
	_ = p.InteractiveSession.Close()
	stopCommand(p.cmd, p.exited)
	return nil
}
