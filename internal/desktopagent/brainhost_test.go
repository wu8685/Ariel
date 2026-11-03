package desktopagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeBrainProcess struct {
	mu       sync.Mutex
	calls    []brainCall
	incoming chan brainFrame
	done     chan struct{}
	replies  []string
}
type brainCall struct {
	method string
	params map[string]any
}

func newFakeBrainProcess() *fakeBrainProcess {
	return &fakeBrainProcess{incoming: make(chan brainFrame, 8), done: make(chan struct{})}
}
func (f *fakeBrainProcess) Call(_ context.Context, method string, params any, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	encoded, _ := json.Marshal(params)
	var values map[string]any
	_ = json.Unmarshal(encoded, &values)
	f.calls = append(f.calls, brainCall{method, values})
	if method == "thread/start" {
		_ = json.Unmarshal([]byte(`{"thread":{"id":"brain-thread","cwd":"`+values["cwd"].(string)+`"}}`), result)
	}
	if method == "turn/start" {
		_ = json.Unmarshal([]byte(`{"turn":{"id":"brain-turn"}}`), result)
	}
	return nil
}
func (f *fakeBrainProcess) Incoming() <-chan brainFrame { return f.incoming }
func (f *fakeBrainProcess) Reply(_ context.Context, _ json.RawMessage, result any, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := json.Marshal(result)
	f.replies = append(f.replies, string(body))
	return nil
}
func (f *fakeBrainProcess) replyCount() int       { f.mu.Lock(); defer f.mu.Unlock(); return len(f.replies) }
func (f *fakeBrainProcess) Done() <-chan struct{} { return f.done }
func (f *fakeBrainProcess) Close() error          { return nil }
func (f *fakeBrainProcess) exit() {
	select {
	case <-f.done:
	default:
		close(f.done)
	}
}

func TestBrainHostCreatesPrivateWorkspaceAndPolicyBoundThread(t *testing.T) {
	process := newFakeBrainProcess()
	root := filepath.Join(t.TempDir(), "puck")
	host := NewBrainHost(BrainHostConfig{Enabled: true, Binary: "/fixture/codex", Root: root, Start: func(context.Context, string, string) (brainProcess, error) { return process, nil }})
	defer host.Close()
	threadID, err := host.Ensure(context.Background(), BrainEnsure{AssistantID: "assistant-a", Name: "Helper", DeveloperInstructions: "guard", ToolsetVersion: 1})
	if err != nil || threadID != "brain-thread" {
		t.Fatalf("thread=%s err=%v", threadID, err)
	}
	info, err := os.Stat(filepath.Join(root, "assistant-a"))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("workspace info=%v err=%v", info, err)
	}
	start := process.calls[0].params
	if start["approvalPolicy"] != "never" || start["approvalsReviewer"] != "user" || start["sandbox"] != "read-only" || start["developerInstructions"] != "guard" || len(start["dynamicTools"].([]any)) == 0 {
		t.Fatalf("start=%+v", start)
	}
	config := start["config"].(map[string]any)
	if config["features.code_mode_host"] != true || config["tools.shell"] != false {
		t.Fatalf("config=%+v", config)
	}
	if _, err := host.Turn(context.Background(), threadID, "client-message-a", "hello", "", ""); err != nil {
		t.Fatal(err)
	}
	turn := process.calls[1].params
	policy, _ := turn["sandboxPolicy"].(map[string]any)
	if turn["clientUserMessageId"] != "client-message-a" || turn["approvalPolicy"] != "never" || policy["type"] != "readOnly" {
		t.Fatalf("turn=%+v", turn)
	}
}

func TestBrainHostRestartsAndResumesAfterProcessExit(t *testing.T) {
	first, second := newFakeBrainProcess(), newFakeBrainProcess()
	starts := 0
	events := make(chan BrainHostEvent, 8)
	host := NewBrainHost(BrainHostConfig{Enabled: true, Binary: "codex", Root: t.TempDir(), RestartBackoff: time.Millisecond, Start: func(context.Context, string, string) (brainProcess, error) {
		starts++
		if starts == 1 {
			return first, nil
		}
		return second, nil
	}, Emit: func(event BrainHostEvent) { events <- event }})
	defer host.Close()
	if _, err := host.Ensure(context.Background(), BrainEnsure{AssistantID: "assistant-a", Name: "A", DeveloperInstructions: "guard", ToolsetVersion: 1}); err != nil {
		t.Fatal(err)
	}
	first.exit()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		second.mu.Lock()
		resumed := len(second.calls) > 0 && second.calls[0].method == "thread/resume"
		second.mu.Unlock()
		if resumed {
			break
		}
		time.Sleep(time.Millisecond)
	}
	second.mu.Lock()
	resumed := len(second.calls) > 0 && second.calls[0].method == "thread/resume"
	second.mu.Unlock()
	if !resumed || starts != 2 {
		t.Fatalf("starts=%d resumed=%v", starts, resumed)
	}
	select {
	case event := <-events:
		if event.ThreadID != "brain-thread" || event.Payload["reason"] != "turn.lost" {
			t.Fatalf("event=%+v", event)
		}
	default:
		t.Fatal("missing turn.lost event")
	}
}

func TestBrainHostRelaysToolCallAndBoundsInFlight(t *testing.T) {
	process := newFakeBrainProcess()
	events := make(chan BrainHostEvent, 8)
	host := NewBrainHost(BrainHostConfig{Enabled: true, Binary: "codex", Root: t.TempDir(), MaxInFlight: 1, Start: func(context.Context, string, string) (brainProcess, error) { return process, nil }, Emit: func(event BrainHostEvent) { events <- event }})
	defer host.Close()
	if _, err := host.Ensure(context.Background(), BrainEnsure{AssistantID: "assistant-a", Name: "A", DeveloperInstructions: "guard", ToolsetVersion: 1}); err != nil {
		t.Fatal(err)
	}
	process.incoming <- brainFrame{ID: json.RawMessage(`1`), Method: "item/tool/call", Params: json.RawMessage(`{"threadId":"brain-thread","callId":"call-a","tool":"list_devices","arguments":{}}`)}
	event := <-events
	if event.Kind != "puck.brain.toolCall" || event.CallID != "call-a" || event.AssistantID != "assistant-a" {
		t.Fatalf("event=%+v", event)
	}
	process.incoming <- brainFrame{ID: json.RawMessage(`2`), Method: "item/tool/call", Params: json.RawMessage(`{"threadId":"brain-thread","callId":"call-b","tool":"list_devices","arguments":{}}`)}
	for i := 0; i < 100 && process.replyCount() != 1; i++ {
		time.Sleep(time.Millisecond)
	}
	if process.replyCount() != 1 {
		t.Fatalf("overflow reply count=%d", process.replyCount())
	}
	if err := host.ToolResult(context.Background(), "brain-thread", "call-a", true, []map[string]any{{"type": "inputText", "text": "ok"}}); err != nil {
		t.Fatal(err)
	}
	if process.replyCount() != 2 {
		t.Fatalf("reply count=%d", process.replyCount())
	}
}
