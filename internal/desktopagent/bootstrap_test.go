package desktopagent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

type bootstrapHistory struct {
	thread appserver.Thread
	ready  chan struct{}
}

func (h *bootstrapHistory) List(context.Context, string, int) (appserver.Page, error) {
	return appserver.Page{}, nil
}
func (h *bootstrapHistory) Read(context.Context, string) (appserver.Thread, error) {
	select {
	case <-h.ready:
		return h.thread, nil
	default:
		return appserver.Thread{}, appserver.ErrRemote
	}
}

type normalizedBootstrapLive struct {
	thread   map[string]any
	updates  chan struct{}
	done     chan struct{}
	terminal chan struct{}
}

func (l *normalizedBootstrapLive) Current() (json.RawMessage, error) { return nil, nil }
func (l *normalizedBootstrapLive) CurrentThread() (map[string]any, error) {
	body, _ := json.Marshal(l.thread)
	var copied map[string]any
	_ = json.Unmarshal(body, &copied)
	return copied, nil
}
func (l *normalizedBootstrapLive) Start(context.Context, string, string) (string, error) {
	return "", appserver.ErrMethodUnavailable
}
func (l *normalizedBootstrapLive) Interrupt(context.Context, string) error { return nil }
func (l *normalizedBootstrapLive) Respond(context.Context, string, string, map[string][]string) error {
	return nil
}
func (l *normalizedBootstrapLive) Updates() <-chan struct{}     { return l.updates }
func (l *normalizedBootstrapLive) Done() <-chan struct{}        { return l.done }
func (l *normalizedBootstrapLive) Terminal() <-chan struct{}    { return l.terminal }
func (l *normalizedBootstrapLive) Verify(context.Context) error { return nil }
func (l *normalizedBootstrapLive) Close() error                 { return nil }

type fakeBootstrapper struct {
	result BootstrapResult
	calls  int
}

func (f *fakeBootstrapper) Bootstrap(_ context.Context, cwd, messageID, text string, images []string) (BootstrapResult, error) {
	f.calls++
	f.result.CWD = cwd
	return f.result, nil
}

func TestBootstrapThreadInstallsOwnerBeforeReceiptAndMigratesWithoutDroppingSubscription(t *testing.T) {
	cwd, _ := filepath.EvalSymlinks(t.TempDir())
	id, turnID := "00000000-0000-4000-8000-000000000076", "turn-bootstrap"
	bootstrap := &normalizedBootstrapLive{thread: map[string]any{
		"threadId": id, "title": "", "cwd": cwd, "updatedAt": time.Now().UTC().Format(time.RFC3339Nano), "runtime": "inProgress",
		"turns":               []any{map[string]any{"turnId": turnID, "status": "inProgress", "items": []any{map[string]any{"itemId": "user", "role": "user", "text": "hello", "clientMessageId": "00000000-0000-4000-8000-000000000077"}}}},
		"pendingInteractions": []any{}, "permissions": map[string]any{"sandbox": "unknown", "approval": "unknown"}, "historyComplete": true, "recentComplete": true,
	}, updates: make(chan struct{}, 2), done: make(chan struct{}), terminal: make(chan struct{})}
	factory := &fakeBootstrapper{result: BootstrapResult{ThreadID: id, TurnID: turnID, Live: bootstrap}}
	history := &bootstrapHistory{ready: make(chan struct{}), thread: appserver.Thread{ID: id, CWD: cwd, Status: json.RawMessage(`{"type":"idle"}`), Turns: []appserver.Turn{{ID: turnID, Status: "completed"}}}}
	desktop := &staticLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}, state: json.RawMessage(`{"cwd":` + strconvQuote(cwd) + `,"threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[{"turnId":"turn-bootstrap","status":"completed","items":[]}]}`)}
	service := NewService(history, func(context.Context, string, string) (Live, error) { return desktop, nil })
	service.SetThreadBootstrapper(factory)
	defer service.Close()

	thread, gotTurn, err := service.BootstrapThread(context.Background(), cwd, "00000000-0000-4000-8000-000000000077", "hello", nil)
	if err != nil || gotTurn != turnID || thread["threadId"] != id || factory.calls != 1 {
		t.Fatalf("bootstrap: thread=%+v turn=%q calls=%d err=%v", thread, gotTurn, factory.calls, err)
	}
	subID, _, snapshot, activate, err := service.Subscribe(context.Background(), id, func(map[string]any) {})
	if err != nil || snapshot["runtime"] != "inProgress" {
		t.Fatalf("subscribe to bootstrap owner: %+v %v", snapshot, err)
	}
	activate()
	defer service.Unsubscribe(subID)

	close(history.ready)
	close(bootstrap.terminal)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		service.mu.Lock()
		controller := service.threads[id]
		service.mu.Unlock()
		controller.mu.Lock()
		migrated := controller.live == desktop && len(controller.subs) == 1
		controller.mu.Unlock()
		if migrated {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("bootstrap owner did not migrate atomically to Desktop follower")
}

func strconvQuote(value string) string {
	body, _ := json.Marshal(value)
	return string(body)
}
