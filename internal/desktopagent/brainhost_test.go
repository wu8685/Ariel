package desktopagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
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
	image := "data:image/png;base64,AAAA"
	if _, err := host.Turn(context.Background(), threadID, "client-message-a", "hello", []string{image}, "", ""); err != nil {
		t.Fatal(err)
	}
	turn := process.calls[1].params
	policy, _ := turn["sandboxPolicy"].(map[string]any)
	if turn["clientUserMessageId"] != "client-message-a" || turn["approvalPolicy"] != "never" || policy["type"] != "readOnly" {
		t.Fatalf("turn=%+v", turn)
	}
	input := turn["input"].([]any)
	if len(input) != 2 || input[0].(map[string]any)["text"] != "hello" || input[1].(map[string]any)["type"] != "image" || input[1].(map[string]any)["url"] != image {
		t.Fatalf("input=%+v", input)
	}
}

func TestBrainHostPublishesCanonicalMutationToolSchemas(t *testing.T) {
	tools := puckDynamicTools(toolcontract.Version)
	byName := map[string]map[string]any{}
	for _, tool := range tools {
		byName[tool["name"].(string)] = tool
	}
	for _, name := range []string{"start_task", "send_turn", "ask_user", "share_result", "request_authorization"} {
		schema := byName[name]["inputSchema"].(map[string]any)
		required, _ := schema["required"].([]string)
		_, hasOneOf := schema["oneOf"]
		if schema["additionalProperties"] != false || len(required) == 0 && !hasOneOf {
			t.Fatalf("%s schema=%+v", name, schema)
		}
	}
	if byName["list_devices"]["inputSchema"].(map[string]any)["type"] != "object" {
		t.Fatal("read-only tool disappeared")
	}
}

// This opt-in compatibility test exercises the installed Codex app-server.
// It deliberately uses a new isolated workspace and never resumes an existing
// thread. Release smoke runs it with ARIEL_TEST_REAL_BRAIN=1.
func TestBrainHostRealAppServerRegistersDynamicTools(t *testing.T) {
	if os.Getenv("ARIEL_TEST_REAL_BRAIN") != "1" {
		t.Skip("set ARIEL_TEST_REAL_BRAIN=1 for the installed Codex compatibility smoke")
	}
	binary := os.Getenv("ARIEL_CODEX_BINARY")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("codex")
		if err != nil {
			t.Fatal(err)
		}
	}
	events := make(chan BrainHostEvent, 16)
	host := NewBrainHost(BrainHostConfig{
		Enabled: true,
		Binary:  binary,
		Root:    filepath.Join(t.TempDir(), "puck"),
		Emit:    func(event BrainHostEvent) { events <- event },
	})
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	threadID, err := host.Ensure(ctx, BrainEnsure{
		AssistantID:           "fixture-assistant",
		Name:                  "Fixture",
		DeveloperInstructions: "这是隔离兼容性测试。收到请求后必须调用 list_devices；不要输出普通文本。",
		ToolsetVersion:        2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Turn(ctx, threadID, "00000000-0000-4000-8000-000000000901", "调用 list_devices。", nil, "", "low"); err != nil {
		t.Fatal(err)
	}
	firstCallID := ""
	for firstCallID == "" {
		select {
		case event := <-events:
			if event.Kind == "puck.brain.toolCall" {
				if event.Tool != "list_devices" {
					t.Fatalf("unexpected first tool call %q", event.Tool)
				}
				firstCallID = event.CallID
			}
		case <-ctx.Done():
			t.Fatal("installed Codex did not expose the dynamic tool before timeout")
		}
	}
	if err := host.ToolResult(ctx, threadID, firstCallID, true, []map[string]any{{"type": "inputText", "text": `{"devices":[]}`}}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event := <-events:
			if event.Kind == "puck.brain.event" && event.Payload["method"] == "turn/completed" {
				goto secondTurn
			}
		case <-ctx.Done():
			t.Fatal("first installed Codex turn did not complete")
		}
	}

secondTurn:
	secondTurnID, err := host.Turn(ctx, threadID, "00000000-0000-4000-8000-000000000902", "再次调用 list_devices。", nil, "", "low")
	if err != nil || secondTurnID == "" {
		t.Fatalf("second turn id=%q err=%v", secondTurnID, err)
	}
	for {
		select {
		case event := <-events:
			if event.Kind == "puck.brain.toolCall" {
				if event.Tool != "list_devices" || event.TurnID != "" && event.TurnID != secondTurnID {
					t.Fatalf("unexpected second tool call: %+v", event)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("installed Codex did not expose the dynamic tool on the second turn")
		}
	}
}

func TestBrainHostRejectsUnknownFutureToolset(t *testing.T) {
	host := NewBrainHost(BrainHostConfig{Enabled: true, Binary: "codex", Root: t.TempDir(), Start: func(context.Context, string, string) (brainProcess, error) {
		return newFakeBrainProcess(), nil
	}})
	defer host.Close()
	if _, err := host.Ensure(context.Background(), BrainEnsure{AssistantID: "assistant-a", Name: "A", DeveloperInstructions: "guard", ToolsetVersion: toolcontract.Version + 1}); err == nil {
		t.Fatal("future toolset was accepted")
	}
}

func TestBrainHostDefaultToolTimeoutCoversSlowMutationWindow(t *testing.T) {
	host := NewBrainHost(BrainHostConfig{Enabled: true, Binary: "codex", Root: t.TempDir()})
	defer host.Close()
	if host.cfg.ToolTimeout <= 3*time.Minute {
		t.Fatalf("tool timeout=%s; must exceed three-minute mutation window", host.cfg.ToolTimeout)
	}
}

func TestBrainHostLogsSanitizedPhaseLatency(t *testing.T) {
	capture := &capturedPhases{}
	restore := SetPhaseLogger(slog.New(capture))
	defer restore()
	process := newFakeBrainProcess()
	host := NewBrainHost(BrainHostConfig{Enabled: true, Binary: "codex", Root: t.TempDir(), Start: func(context.Context, string, string) (brainProcess, error) {
		return process, nil
	}})
	defer host.Close()
	const assistantID = "assistant-secret-id"
	threadID, err := host.Ensure(context.Background(), BrainEnsure{AssistantID: assistantID, Name: "A", DeveloperInstructions: "guard", ToolsetVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Turn(context.Background(), threadID, "client-message", "secret prompt", nil, "", "low"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(capture.phases(), ",")
	for _, phase := range []string{"brain-process-start", "brain-thread-ensure", "brain-turn-start"} {
		if !strings.Contains(joined, phase) {
			t.Fatalf("missing phase %q in %s", phase, joined)
		}
	}
	for _, record := range capture.records {
		if strings.Contains(record.Message, assistantID) || strings.Contains(record.Message, "secret prompt") {
			t.Fatalf("brain phase log leaked sensitive input: %v", record)
		}
	}
}

func TestBrainHostLogsSafeStructuredToolRejectionDiagnostic(t *testing.T) {
	capture := &capturedPhases{}
	restore := SetPhaseLogger(slog.New(capture))
	defer restore()
	process := newFakeBrainProcess()
	events := make(chan BrainHostEvent, 1)
	host := NewBrainHost(BrainHostConfig{Enabled: true, Binary: "codex", Root: t.TempDir(), Start: func(context.Context, string, string) (brainProcess, error) { return process, nil }, Emit: func(event BrainHostEvent) { events <- event }})
	defer host.Close()
	if _, err := host.Ensure(context.Background(), BrainEnsure{AssistantID: "assistant-a", Name: "A", DeveloperInstructions: "guard", ToolsetVersion: 1}); err != nil {
		t.Fatal(err)
	}
	process.incoming <- brainFrame{ID: json.RawMessage(`1`), Method: "item/tool/call", Params: json.RawMessage(`{"threadId":"brain-thread","callId":"call-a","tool":"ask_user","arguments":{}}`)}
	<-events
	content := `{"code":"INVALID_ARGUMENT","diagnostic":{"tool":"ask_user","details":[{"field":"multiple","reason":"additional property is not allowed","expected":"only documented properties"}],"shape":{"type":"object","fields":{"multiple":{"type":"boolean"}}}},"untrusted":"private-user-text"}`
	if err := host.ToolResult(context.Background(), "brain-thread", "call-a", false, []map[string]any{{"type": "inputText", "text": content}}); err != nil {
		t.Fatal(err)
	}
	if capture.leaks("private-user-text") {
		t.Fatal("tool result log leaked content outside the safe diagnostic")
	}
	var found bool
	capture.mu.Lock()
	for _, record := range capture.records {
		fields := map[string]slog.Value{}
		record.Attrs(func(attr slog.Attr) bool { fields[attr.Key] = attr.Value; return true })
		if fields["phase"].String() == "brain-tool-result" && fields["tool"].String() == "ask_user" && fields["rejectedField"].String() == "multiple" {
			found = true
		}
	}
	capture.mu.Unlock()
	if !found {
		t.Fatalf("safe rejection diagnostic missing: phases=%v", capture.phases())
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
