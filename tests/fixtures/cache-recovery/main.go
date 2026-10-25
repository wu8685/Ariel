// This loopback-only E2E fixture runs the production Desktop Agent and Relay.
// Only the Codex native owner is simulated; it cannot write to real threads.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/desktopagent"
	"github.com/wu8685/Ariel/internal/relay"
)

const fixtureThread = "00000000-0000-4000-8000-000000000083"
const fixtureCWD = "/tmp/native-cache-recovery"

type owner struct {
	history *history
	updates chan struct{}
	done    chan struct{}
	once    sync.Once
}

func (o *owner) Current() (json.RawMessage, error) {
	o.history.mu.Lock()
	defer o.history.mu.Unlock()
	status, text := "completed", "原生会话上一轮已完成"
	runtime := "idle"
	if o.history.processing {
		status, text, runtime = "inProgress", "原生会话仍在处理", "inProgress"
	} else if o.history.continued {
		text = "原生会话最终回复自动到达"
	}
	return json.Marshal(map[string]any{
		"cwd": fixtureCWD, "threadRuntimeStatus": map[string]any{"type": runtime}, "requests": []any{},
		"latestThreadSettings": map[string]any{"approvalPolicy": "on-request", "sandboxPolicy": map[string]any{"type": "dangerFullAccess"}},
		"turns": []any{map[string]any{"turnId": "native-turn", "status": status, "items": []any{
			map[string]any{"type": "userMessage", "id": "native-user", "content": []any{map[string]any{"type": "text", "text": "从 Codex App 继续会话"}}},
			map[string]any{"type": "agentMessage", "id": "native-reply", "text": text},
		}}},
	})
}

func (o *owner) Refresh(ctx context.Context) (json.RawMessage, error) {
	o.history.refreshes.Add(1)
	o.history.mu.Lock()
	blocked := o.history.blocked
	o.history.mu.Unlock()
	if blocked != nil {
		select {
		case <-blocked:
			o.history.finished.Add(1)
			return nil, errors.New("injected late recovery failure")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return o.Current()
}
func (o *owner) Updates() <-chan struct{} { return o.updates }
func (o *owner) Done() <-chan struct{}    { return o.done }
func (o *owner) Close() error             { o.once.Do(func() { close(o.done) }); return nil }
func (*owner) Start(context.Context, string, string) (string, error) {
	return "", errors.New("fixture forbids mutations")
}
func (*owner) Interrupt(context.Context, string) error {
	return errors.New("fixture forbids mutations")
}
func (*owner) Respond(context.Context, string, string, map[string][]string) error {
	return errors.New("fixture forbids mutations")
}

type history struct {
	mu                            sync.Mutex
	processing, continued         bool
	current                       *owner
	blocked                       chan struct{}
	attaches, refreshes, finished atomic.Int32
}

func (*history) Read(context.Context, string) (appserver.Thread, error) {
	return appserver.Thread{ID: fixtureThread, Name: "真实 Agent 恢复验收", CWD: fixtureCWD, Status: json.RawMessage(`{"type":"idle"}`)}, nil
}
func (h *history) List(ctx context.Context, _ string, _ int) (appserver.Page, error) {
	thread, err := h.Read(ctx, fixtureThread)
	return appserver.Page{Data: []appserver.Thread{thread}}, err
}
func (h *history) attach(context.Context, string, string) (desktopagent.Live, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attaches.Add(1)
	h.current = &owner{history: h, updates: make(chan struct{}, 1), done: make(chan struct{})}
	return h.current, nil
}
func (h *history) action(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch r.URL.Query().Get("action") {
	case "continue-blocked":
		h.processing, h.continued = true, true
		h.blocked = make(chan struct{})
	case "continue":
		h.processing, h.continued = true, true
	case "complete":
		h.processing = false
	case "disconnect":
		if h.current != nil {
			h.current.Close()
		}
	case "release":
		if h.blocked != nil {
			close(h.blocked)
			h.blocked = nil
		}
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if h.current != nil && r.URL.Query().Get("action") != "release" {
		select {
		case h.current.updates <- struct{}{}:
		default:
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func main() {
	listen := flag.String("listen", "127.0.0.1:4182", "loopback fixture listen address")
	dist := flag.String("dist", "web/dist", "built Web assets")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		log.Fatal("fixture must bind a loopback IP")
	}
	r, err := relay.New(relay.Config{Token: "isolated-cache-agent-token", WebPIN: "012345", AllowedOrigins: []string{"http://" + *listen}})
	if err != nil {
		log.Fatal(err)
	}
	h := &history{}
	service := desktopagent.NewService(h, h.attach)
	defer service.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	mux := http.NewServeMux()
	mux.Handle("/ws", r.Handler())
	mux.HandleFunc("/fixture/action", h.action)
	mux.HandleFunc("/fixture/state", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"attaches": h.attaches.Load(), "refreshes": h.refreshes.Load(), "finished": h.finished.Load()})
	})
	mux.Handle("/", http.FileServer(http.Dir(*dist)))
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: time.Second}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	go server.Serve(listener)
	go func() {
		if err := desktopagent.RunWithService(ctx, desktopagent.Config{
			URL: "ws://" + *listen + "/ws", Token: "isolated-cache-agent-token", DeviceID: "fixture-mac", DeviceName: "隔离 Native Fixture",
			CachePublishInterval: 20 * time.Millisecond, CacheRecoveryGuardDelay: 100 * time.Millisecond, CacheLeaseInterval: 50 * time.Millisecond,
		}, service); err != nil && ctx.Err() == nil {
			log.Printf("fixture agent stopped: %v", err)
			cancel()
		}
	}()
	<-ctx.Done()
	server.Close()
}
