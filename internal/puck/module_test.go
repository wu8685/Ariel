package puck

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/relay"
)

type fakeModuleBridge struct {
	mu             sync.Mutex
	calls          []string
	brains         map[relay.BrainThread]string
	ensureFailures int
	ensureThreads  []string
	events         chan relay.BrainEvent
	params         []json.RawMessage
}

func (f *fakeModuleBridge) Devices() []map[string]any {
	return []map[string]any{{"deviceId": "brain-a", "capabilities": map[string]bool{"puckBrainReady": true}}}
}
func (f *fakeModuleBridge) BrainThreads(v map[relay.BrainThread]string) { f.brains = v }
func (f *fakeModuleBridge) Forward(_ context.Context, _ relay.Origin, _ string, method string, params json.RawMessage) relay.PuckResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	f.params = append(f.params, append(json.RawMessage(nil), params...))
	if method == "puck.brain.ensure" {
		if f.ensureFailures > 0 {
			f.ensureFailures--
			return relay.PuckResponse{Outcome: "rejected", Error: &relay.PuckError{Code: "BRAIN_UNAVAILABLE", Message: "fixture unavailable"}}
		}
		threadID := "brain-thread-a"
		if len(f.ensureThreads) > 0 {
			threadID, f.ensureThreads = f.ensureThreads[0], f.ensureThreads[1:]
		}
		return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{"threadId": threadID}}
	}
	if method == "puck.brain.turn.start" {
		return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{"turnId": "brain-turn-a"}}
	}
	return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{}}
}

func (f *fakeModuleBridge) hasToolResult(callID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, method := range f.calls {
		if method == "puck.brain.toolResult" && strings.Contains(string(f.params[i]), `"callId":"`+callID+`"`) {
			return true
		}
	}
	return false
}
func (f *fakeModuleBridge) BrainEvents() <-chan relay.BrainEvent {
	if f.events == nil {
		f.events = make(chan relay.BrainEvent, 4)
	}
	return f.events
}

func TestModuleRetriesProvisioningWithoutCreatingAnotherAssistant(t *testing.T) {
	bridge := &fakeModuleBridge{ensureFailures: 1}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	failed := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	if failed.Error == nil {
		t.Fatalf("create=%+v", failed)
	}
	listed := module.Handle(context.Background(), "puck.list", nil)
	items := listed.Data["assistants"].([]map[string]any)
	if len(items) != 1 || items[0]["status"] != "provisioning" {
		t.Fatalf("assistants=%+v", items)
	}
	id := items[0]["assistantId"].(string)
	retried := module.Handle(context.Background(), "puck.update", mustJSON(map[string]any{"assistantId": id, "description": "retry"}))
	if retried.Error != nil || retried.Data["assistant"].(map[string]any)["status"] != "ready" {
		t.Fatalf("retry=%+v", retried)
	}
	listed = module.Handle(context.Background(), "puck.list", nil)
	if got := len(listed.Data["assistants"].([]map[string]any)); got != 1 {
		t.Fatalf("assistant count=%d", got)
	}
}

func TestModuleExecutesTrustedBrainToolCallAndReturnsResult(t *testing.T) {
	bridge := &fakeModuleBridge{events: make(chan relay.BrainEvent, 1)}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	bridge.events <- relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.toolCall", Payload: json.RawMessage(`{"callId":"call-a","tool":"list_devices","arguments":{}}`)}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if bridge.hasToolResult("call-a") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("tool result was not returned")
}

func TestModuleRotatesBrainBeforeRetiringOldThread(t *testing.T) {
	bridge := &fakeModuleBridge{ensureThreads: []string{"brain-old", "brain-new"}}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	updated := module.Handle(context.Background(), "puck.update", mustJSON(map[string]any{"assistantId": id, "brainDeviceId": "brain-a", "name": "Renamed"}))
	if updated.Error != nil {
		t.Fatalf("update=%+v", updated)
	}
	a := updated.Data["assistant"].(map[string]any)
	if a["brainThreadId"] != "brain-new" || a["name"] != "Renamed" {
		t.Fatalf("assistant=%+v", a)
	}
	if len(bridge.calls) < 3 || bridge.calls[len(bridge.calls)-2] != "puck.brain.ensure" || bridge.calls[len(bridge.calls)-1] != "puck.brain.retire" {
		t.Fatalf("calls=%v", bridge.calls)
	}
}

func TestModuleCreateMessageHistoryAndDelete(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a","allowedScope":{}}`))
	if created.Error != nil {
		t.Fatalf("create=%+v", created)
	}
	assistant := created.Data["assistant"].(map[string]any)
	id := assistant["assistantId"].(string)
	if assistant["status"] != "ready" || bridge.brains[relay.BrainThread{DeviceID: "brain-a", ThreadID: "brain-thread-a"}] != id {
		t.Fatalf("assistant=%+v brains=%v", assistant, bridge.brains)
	}
	message := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000130", "text": "hello"}))
	if message.Error != nil || message.Data["state"] != "processing" {
		t.Fatalf("message=%+v", message)
	}
	history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id}))
	if history.Error != nil || len(history.Data["messages"].([]Message)) != 1 {
		t.Fatalf("history=%+v", history)
	}
	deleted := module.Handle(context.Background(), "puck.delete", mustJSON(map[string]any{"assistantId": id}))
	if deleted.Error != nil {
		t.Fatalf("delete=%+v", deleted)
	}
	if got := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})); got.Error == nil || got.Error.Code != "PUCK_ASSISTANT_NOT_FOUND" {
		t.Fatalf("deleted history=%+v", got)
	}
}
