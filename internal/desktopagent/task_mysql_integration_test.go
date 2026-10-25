package desktopagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	chillingclient "github.com/wu8685/Ariel/internal/chilling"
	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/relay"
)

// This opt-in fixture crosses the real Unix HTTP/task-service/MySQL boundary.
// Only the native owner is simulated; no real Codex mutation is performed.
func TestTaskMySQLCrossRepositoryBackgroundLifecycle(t *testing.T) {
	endpoint := os.Getenv("ARIEL_TASK_INTEGRATION_ENDPOINT")
	if endpoint == "" {
		t.Skip("set ARIEL_TASK_INTEGRATION_ENDPOINT to the isolated cross-repo-fixture Unix service")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	backend, err := chillingclient.NewTaskClient(ctx, chillingclient.Config{Enabled: true, Endpoint: endpoint, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if backend.TaskStoreID() != "cross-repo-fixture" {
		t.Fatal("refusing to mutate a non-fixture task store")
	}
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	managerID, executorID := "manager-"+rand.Text(), "executor-"+rand.Text()
	managerToken, executorToken := "manager-private-"+rand.Text(), "executor-private-"+rand.Text()
	server, err := relay.New(relay.Config{Token: "fixture-legacy", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}, TasksEnabled: true, TaskDevices: map[string]relay.TaskDeviceCredential{
		managerID: {Token: managerToken, TaskStoreIDs: []string{backend.TaskStoreID()}, ExecutionTargets: []string{executorID}}, executorID: {Token: executorToken, Execute: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	url := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	capture := &crossRepoTaskCapture{}
	executorURL, closeTap := crossRepoRelayTap(ctx, url, capture)
	defer closeTap()
	state := newCrossRepoNativeFixture(cwd)
	service := NewService(state, func(context.Context, string, string) (Live, error) { return state.live(false), nil })
	service.SetThreadBootstrapper(state)
	defer service.Close()
	errs := make(chan error, 2)
	executorReady := make(chan struct{})
	managerReady := make(chan struct{})
	go func() {
		errs <- RunWithService(ctx, Config{URL: executorURL, Token: executorToken, DeviceID: executorID, DeviceName: "Cross-repo executor", TasksEnabled: true, TaskAllowedManagers: []string{managerID}, TaskAllowedRoots: []string{cwd}, TaskExecutionCheck: func(context.Context, string, string) error { return nil }, OnReady: func() error { close(executorReady); return nil }}, service)
	}()
	crossRepoWaitReady(t, ctx, executorReady, errs)
	go func() {
		errs <- RunWithService(ctx, Config{URL: url, Token: managerToken, DeviceID: managerID, DeviceName: "Cross-repo manager", TasksEnabled: true, TasksManagerOnly: true, TaskBackend: backend, TaskPollInterval: 20 * time.Millisecond, OnReady: func() error { close(managerReady); return nil }}, nil)
	}()
	crossRepoWaitReady(t, ctx, managerReady, errs)
	defer func() {
		cancel()
		for range 2 {
			select {
			case <-errs:
			case <-time.After(3 * time.Second):
				t.Error("fixture Agent did not stop")
			}
		}
	}()
	task := crossRepoCall(t, ctx, backend, "task.create", map[string]any{"taskStoreId": backend.TaskStoreID(), "clientSubmissionId": "create-" + rand.Text(), "title": "Cross-repository fixture", "goal": "Verify real local persistence and bounded background routing", "executionHostId": executorID, "cwd": cwd})
	task = crossRepoCall(t, ctx, backend, "task.action", map[string]any{"taskStoreId": backend.TaskStoreID(), "taskId": task["taskId"], "expectedVersion": task["version"], "clientSubmissionId": "start-" + rand.Text(), "action": "start"})
	task = crossRepoWaitTask(t, ctx, backend, task["taskId"], func(task map[string]any) bool {
		return task["status"] == "running" && task["nativeThreadId"] == state.threadID
	})
	state.completeCurrent()
	task = crossRepoWaitTask(t, ctx, backend, task["taskId"], func(task map[string]any) bool { return task["status"] == "review" && task["unread"] == true })
	if task["status"] == "done" {
		t.Fatal("native completion bypassed user acceptance")
	}
	task = crossRepoCall(t, ctx, backend, "task.readAck", map[string]any{"taskStoreId": backend.TaskStoreID(), "taskId": task["taskId"], "readSeq": task["lastSeq"]})
	if task["unread"] != false {
		t.Fatal("real read cursor was not persisted")
	}
	task = crossRepoCall(t, ctx, backend, "task.feedback", map[string]any{"taskStoreId": backend.TaskStoreID(), "taskId": task["taskId"], "expectedVersion": task["version"], "clientSubmissionId": "feedback-" + rand.Text(), "body": "Follow-up in the same isolated native thread"})
	if task["unread"] != false {
		t.Fatal("own saved feedback generated unread")
	}
	task = crossRepoWaitTask(t, ctx, backend, task["taskId"], func(task map[string]any) bool { return task["status"] == "running" && state.turnCount() == 2 })
	if task["nativeThreadId"] != state.threadID {
		t.Fatal("feedback silently changed native thread")
	}
	state.completeCurrent()
	task = crossRepoWaitTask(t, ctx, backend, task["taskId"], func(task map[string]any) bool { return task["status"] == "review" && crossRepoFeedbackResponded(task) })
	if task["unread"] != true || state.bootstrapCount() != 1 || state.turnCount() != 2 {
		t.Fatalf("unexpected persisted result: task=%v bootstrap=%d turns=%d", task, state.bootstrapCount(), state.turnCount())
	}
	if task["latestProgress"] != "本轮执行结束，待验收；已完成 2/2 项工具活动，含 1 项文件变更活动" {
		t.Fatalf("safe metadata progress did not persist: %v", task["latestProgress"])
	}
	capture.assertSafe(t, state)
}

type crossRepoTaskCapture struct {
	mu     sync.Mutex
	frames [][]byte
}

func (c *crossRepoTaskCapture) record(raw []byte) {
	var header struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &header) == nil && strings.HasPrefix(header.Type, "task.") {
		c.mu.Lock()
		c.frames = append(c.frames, append([]byte(nil), raw...))
		c.mu.Unlock()
	}
}
func (c *crossRepoTaskCapture) assertSafe(t *testing.T, state *crossRepoNativeFixture) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.frames) == 0 {
		t.Fatal("fixture did not observe actual executor-to-Relay task frames")
	}
	state.mu.Lock()
	nativeBody, _ := json.Marshal(state.turns)
	sentinels := append([]string(nil), state.sentinels...)
	state.mu.Unlock()
	for _, sentinel := range sentinels {
		if !strings.Contains(string(nativeBody), sentinel) {
			t.Fatal("negative fixture sentinel was not actually present in native state")
		}
	}
	facts := 0
	for _, raw := range c.frames {
		for _, sentinel := range sentinels {
			if strings.Contains(string(raw), sentinel) {
				t.Fatal("private native/tool/file sentinel escaped in an executor task frame")
			}
		}
		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			t.Fatal("invalid captured frame")
		}
		if frame["type"] != "task.fact" {
			continue
		}
		facts++
		fact, _ := frame["fact"].(map[string]any)
		allowed := map[string]bool{"eventId": true, "commandId": true, "taskId": true, "attemptId": true, "executionHostId": true, "bindingGeneration": true, "nativeThreadId": true, "nativeTurnId": true, "status": true, "progress": true, "sourceSeq": true, "feedbackIds": true}
		for key := range fact {
			if !allowed[key] {
				t.Fatalf("non-whitelisted native field published: %s", key)
			}
		}
		progress, _ := fact["progress"].(string)
		if !strings.Contains(progress, "已完成 2/2 项工具活动") {
			t.Fatalf("task fact did not contain only expected safe metadata: %v", fact)
		}
	}
	if facts < 2 {
		t.Fatalf("expected real routed task facts, observed %d", facts)
	}
}

// An executor-only transparent WebSocket tap records plaintext application
// frames immediately before forwarding them to the real Relay. It is not a
// fake task router and does not inspect/persist hello credentials.
func crossRepoRelayTap(ctx context.Context, target string, capture *crossRepoTaskCapture) (string, func()) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer local.CloseNow()
		remote, _, err := websocket.Dial(ctx, target, nil)
		if err != nil {
			return
		}
		defer remote.CloseNow()
		go func() {
			for {
				kind, raw, err := remote.Read(ctx)
				if err != nil {
					return
				}
				if local.Write(ctx, kind, raw) != nil {
					return
				}
			}
		}()
		for {
			kind, raw, err := local.Read(ctx)
			if err != nil {
				return
			}
			capture.record(raw)
			if remote.Write(ctx, kind, raw) != nil {
				return
			}
		}
	}))
	return "ws" + strings.TrimPrefix(server.URL, "http"), server.Close
}

func crossRepoCall(t *testing.T, ctx context.Context, backend *chillingclient.TaskClient, method string, input any) map[string]any {
	t.Helper()
	var response struct {
		Task map[string]any `json:"task"`
	}
	if err := backend.Call(ctx, method, input, &response); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	if response.Task == nil {
		t.Fatalf("%s returned no persisted task", method)
	}
	return response.Task
}
func crossRepoWaitReady(t *testing.T, ctx context.Context, ready <-chan struct{}, errs <-chan error) {
	t.Helper()
	select {
	case <-ready:
	case err := <-errs:
		t.Fatalf("fixture Agent ended: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func crossRepoWaitTask(t *testing.T, ctx context.Context, backend *chillingclient.TaskClient, id any, check func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var last map[string]any
	for {
		last = crossRepoCall(t, ctx, backend, "task.read", map[string]any{"taskStoreId": backend.TaskStoreID(), "taskId": id})
		if check(last) {
			return last
		}
		select {
		case <-deadline.C:
			t.Fatalf("persisted task did not reach expected state: %v", last)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
func crossRepoFeedbackResponded(task map[string]any) bool {
	messages, _ := task["messages"].([]any)
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["kind"] == "feedback" && message["feedbackDelivery"] == "responded" {
			return true
		}
	}
	return false
}

type crossRepoNativeFixture struct {
	mu            sync.Mutex
	threadID, cwd string
	turns         []map[string]any
	lives         []*crossRepoFixtureLive
	bootstraps    int
	terminal      chan struct{}
	terminalOnce  sync.Once
	sentinels     []string
}

func newCrossRepoNativeFixture(cwd string) *crossRepoNativeFixture {
	// UUID shape is required by the actual native owner bootstrap boundary.
	id := fmt.Sprintf("%08x-%04x-4000-8000-%012x", time.Now().UnixNano()&0xffffffff, time.Now().UnixNano()&0xffff, time.Now().UnixNano()&0xffffffffffff)
	return &crossRepoNativeFixture{threadID: id, cwd: cwd, terminal: make(chan struct{}), sentinels: []string{"PRIVATE_NATIVE_" + rand.Text(), "PRIVATE_TOOL_" + rand.Text(), "PRIVATE_FILE_" + rand.Text()}}
}
func (s *crossRepoNativeFixture) List(context.Context, string, int) (appserver.Page, error) {
	return appserver.Page{}, nil
}
func (s *crossRepoNativeFixture) Read(_ context.Context, id string) (appserver.Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.threadID {
		return appserver.Thread{}, appserver.ErrRemote
	}
	return appserver.Thread{ID: s.threadID, CWD: s.cwd, Status: json.RawMessage(`{"type":"idle"}`)}, nil
}
func (s *crossRepoNativeFixture) Bootstrap(_ context.Context, cwd, messageID, text string, _ []string) (BootstrapResult, error) {
	s.mu.Lock()
	s.bootstraps++
	turnID := s.appendTurnLocked(messageID, text)
	s.mu.Unlock()
	return BootstrapResult{ThreadID: s.threadID, TurnID: turnID, CWD: cwd, Live: s.live(true)}, nil
}
func (s *crossRepoNativeFixture) appendTurnLocked(messageID, text string) string {
	turnID := "turn-" + rand.Text()
	s.turns = append(s.turns, map[string]any{"turnId": turnID, "status": "inProgress", "items": []any{
		map[string]any{"itemId": "user-" + rand.Text(), "role": "user", "text": text, "clientMessageId": messageID},
		map[string]any{"itemId": "private-native-" + rand.Text(), "role": "assistant", "text": s.sentinels[0]},
		map[string]any{"itemId": "private-tool-" + rand.Text(), "role": "tool", "text": s.sentinels[1], "activity": map[string]any{"kind": "commandExecution", "status": "completed", "label": s.sentinels[1], "command": s.sentinels[1], "detail": s.sentinels[1]}},
		map[string]any{"itemId": "private-file-" + rand.Text(), "role": "tool", "text": s.sentinels[2], "activity": map[string]any{"kind": "fileChange", "status": "completed", "path": s.sentinels[2], "detail": s.sentinels[2]}},
	}})
	s.notifyLocked()
	return turnID
}
func (s *crossRepoNativeFixture) notifyLocked() {
	for _, live := range s.lives {
		select {
		case live.updates <- struct{}{}:
		default:
		}
	}
}
func (s *crossRepoNativeFixture) live(bootstrap bool) *crossRepoFixtureLive {
	live := &crossRepoFixtureLive{state: s, updates: make(chan struct{}, 4), done: make(chan struct{}), bootstrap: bootstrap}
	s.mu.Lock()
	s.lives = append(s.lives, live)
	s.mu.Unlock()
	live.updates <- struct{}{}
	return live
}
func (s *crossRepoNativeFixture) completeCurrent() {
	s.mu.Lock()
	if len(s.turns) > 0 {
		turn := s.turns[len(s.turns)-1]
		turn["status"] = "completed"
		turn["items"] = append(turn["items"].([]any), map[string]any{"itemId": "assistant-" + rand.Text(), "role": "assistant", "text": "Completed isolated fixture work"})
	}
	s.notifyLocked()
	first := len(s.turns) == 1
	s.mu.Unlock()
	if first {
		s.terminalOnce.Do(func() { close(s.terminal) })
	}
}
func (s *crossRepoNativeFixture) turnCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.turns)
}
func (s *crossRepoNativeFixture) bootstrapCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bootstraps
}

type crossRepoFixtureLive struct {
	state         *crossRepoNativeFixture
	updates, done chan struct{}
	bootstrap     bool
	closeOnce     sync.Once
}

func (l *crossRepoFixtureLive) Current() (json.RawMessage, error) { return nil, nil }
func (l *crossRepoFixtureLive) CurrentThread() (map[string]any, error) {
	l.state.mu.Lock()
	defer l.state.mu.Unlock()
	runtime := "idle"
	if n := len(l.state.turns); n > 0 && l.state.turns[n-1]["status"] == "inProgress" {
		runtime = "inProgress"
	}
	thread := map[string]any{"threadId": l.state.threadID, "title": "Isolated native fixture", "cwd": l.state.cwd, "updatedAt": time.Now().UTC().Format(time.RFC3339Nano), "runtime": runtime, "turns": l.state.turns, "pendingInteractions": []any{}, "permissions": map[string]any{"sandbox": "unknown", "approval": "unknown"}, "historyComplete": true, "recentComplete": true}
	raw, err := json.Marshal(thread)
	if err != nil {
		return nil, err
	}
	var copied map[string]any
	err = json.Unmarshal(raw, &copied)
	return copied, err
}
func (l *crossRepoFixtureLive) Start(_ context.Context, messageID, text string) (string, error) {
	l.state.mu.Lock()
	defer l.state.mu.Unlock()
	if n := len(l.state.turns); n > 0 && l.state.turns[n-1]["status"] == "inProgress" {
		return "", desktopipc.ErrTurnBusy
	}
	return l.state.appendTurnLocked(messageID, text), nil
}
func (l *crossRepoFixtureLive) Interrupt(context.Context, string) error { return nil }
func (l *crossRepoFixtureLive) Respond(context.Context, string, string, map[string][]string) error {
	return nil
}
func (l *crossRepoFixtureLive) Updates() <-chan struct{}     { return l.updates }
func (l *crossRepoFixtureLive) Done() <-chan struct{}        { return l.done }
func (l *crossRepoFixtureLive) Terminal() <-chan struct{}    { return l.state.terminal }
func (l *crossRepoFixtureLive) Verify(context.Context) error { return nil }
func (l *crossRepoFixtureLive) Close() error                 { l.closeOnce.Do(func() { close(l.done) }); return nil }
