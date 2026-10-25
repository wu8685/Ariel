package desktopagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/relay"
	"github.com/wu8685/Ariel/protocol"
)

func TestTaskExecutionFailureReasonPreservesOnlyTrustedCompatibilityCode(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{errors.New("NATIVE_COMPATIBILITY_UNVERIFIED"), "NATIVE_COMPATIBILITY_UNVERIFIED"},
		{errors.New("OFFICIAL_PROVIDER_UNVERIFIED"), "OFFICIAL_PROVIDER_UNVERIFIED"},
		{errors.New("private account and project configuration must never leave this host"), "OFFICIAL_PROVIDER_UNVERIFIED"},
	} {
		if got := taskExecutionFailureReason(test.err); got != test.want {
			t.Fatalf("reason=%s, want %s", got, test.want)
		}
	}
}

type fixtureTaskBackend struct {
	mu               sync.Mutex
	command          protocol.TaskCommand
	claimed          bool
	trackingReturned bool
	validations      int
	receipts         chan protocol.TaskReceipt
	facts            chan protocol.TaskFact
}

type readinessTaskBackend struct {
	*fixtureTaskBackend
	ready bool
}

func (b *readinessTaskBackend) Ready() bool { return b.ready }

func TestTaskBackendOutageBuffersReceiptAndLatestTerminalFact(t *testing.T) {
	backend := &readinessTaskBackend{fixtureTaskBackend: &fixtureTaskBackend{receipts: make(chan protocol.TaskReceipt, 2), facts: make(chan protocol.TaskFact, 2)}}
	runtime := newTaskRuntime(&agent{deviceID: "manager"}, Config{TasksEnabled: true, TaskBackend: backend})
	c := protocol.TaskCommand{CommandID: "command", TaskID: "task", AttemptID: "attempt", LeaseToken: "lease", BindingGeneration: 1}
	f := protocol.TaskFrame{Type: "task.dispatch", DispatchID: "dispatch", Command: &c}
	runtime.manager[f.DispatchID] = f
	receipt := f.CloneAs("task.receipt")
	receipt.Receipt = &protocol.TaskReceipt{CommandID: c.CommandID, LeaseToken: c.LeaseToken, Outcome: "accepted", NativeThreadID: "thread", NativeTurnID: "turn"}
	runtime.saveReceipt(context.Background(), receipt)
	for index := range 130 {
		fact := f.CloneAs("task.fact")
		fact.Fact = &protocol.TaskFact{EventID: strings.Repeat("x", index+1), CommandID: c.CommandID, TaskID: c.TaskID, AttemptID: c.AttemptID, BindingGeneration: c.BindingGeneration, NativeThreadID: "thread", NativeTurnID: "turn", Status: "running"}
		if index == 129 {
			fact.Fact.Status = "completed"
		}
		runtime.saveFact(context.Background(), fact)
	}
	if len(runtime.receipts) != 1 || len(runtime.facts) != 1 || len(backend.receipts) != 0 || len(backend.facts) != 0 {
		t.Fatalf("outage lost or did not coalesce bounded observations: receipts=%d facts=%d", len(runtime.receipts), len(runtime.facts))
	}
	backend.ready = true
	runtime.flush(context.Background())
	if len(backend.receipts) != 1 || len(backend.facts) != 1 {
		t.Fatal("restored backend did not receive the accepted identity and terminal observation")
	}
	if fact := <-backend.facts; fact.Status != "completed" {
		t.Fatalf("stale progress displaced terminal snapshot: %+v", fact)
	}
	if len(runtime.receipts) != 0 || len(runtime.facts) != 0 || len(runtime.manager) != 0 {
		t.Fatal("committed terminal state retained a dead local dispatch")
	}
}

func TestTaskBackendOutageDisconnectDoesNotEraseAcceptedIdentity(t *testing.T) {
	backend := &readinessTaskBackend{fixtureTaskBackend: &fixtureTaskBackend{receipts: make(chan protocol.TaskReceipt, 2)}}
	runtime := newTaskRuntime(&agent{deviceID: "manager"}, Config{TasksEnabled: true, TaskBackend: backend})
	c := protocol.TaskCommand{CommandID: "command", LeaseToken: "lease", ClientMessageID: "client-message"}
	f := protocol.TaskFrame{Type: "task.dispatch", DispatchID: "dispatch", Command: &c}
	runtime.manager[f.DispatchID] = f
	reply := f.CloneAs("task.receipt")
	reply.Receipt = &protocol.TaskReceipt{CommandID: c.CommandID, LeaseToken: c.LeaseToken, Outcome: "accepted", NativeThreadID: "thread", NativeTurnID: "turn", ClientMessageID: c.ClientMessageID}
	runtime.saveReceipt(context.Background(), reply)
	reply.Receipt = &protocol.TaskReceipt{CommandID: c.CommandID, LeaseToken: c.LeaseToken, Outcome: "unknown", Reason: "OUTCOME_UNKNOWN"}
	runtime.saveReceipt(context.Background(), reply)
	backend.ready = true
	runtime.flush(context.Background())
	if receipt := <-backend.receipts; receipt.Outcome != "unknown" || receipt.NativeThreadID != "thread" || receipt.NativeTurnID != "turn" || receipt.ClientMessageID != c.ClientMessageID {
		t.Fatalf("transport disconnection erased already observed accepted identity: %+v", receipt)
	}
}

func (b *fixtureTaskBackend) Ready() bool         { return true }
func (b *fixtureTaskBackend) TaskStoreID() string { return "store" }
func (b *fixtureTaskBackend) Call(_ context.Context, method string, params, output any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var result any = map[string]any{}
	switch method {
	case "task.command.tracking":
		commands := []protocol.TaskCommand{}
		if b.command.State == "accepted" && !b.trackingReturned {
			commands = append(commands, b.command)
			b.trackingReturned = true
		}
		result = map[string]any{"commands": commands}
	case "task.command.claim":
		commands := []protocol.TaskCommand{}
		if !b.claimed {
			commands = append(commands, b.command)
			b.claimed = true
		}
		result = map[string]any{"commands": commands}
	case "task.command.validate":
		b.validations++
		result = map[string]any{"valid": true, "command": b.command}
	case "task.command.record":
		data, _ := json.Marshal(params)
		var receipt protocol.TaskReceipt
		json.Unmarshal(data, &receipt)
		b.receipts <- receipt
	case "task.fact.record":
		data, _ := json.Marshal(params)
		var fact protocol.TaskFact
		json.Unmarshal(data, &fact)
		b.facts <- fact
	case "task.list":
		result = map[string]any{"taskStoreId": "store", "tasks": []any{}}
	}
	if output != nil {
		data, _ := json.Marshal(result)
		return json.Unmarshal(data, output)
	}
	return nil
}

type taskFixtureHistory struct {
	fakeHistory
	cwd string
}

func (h taskFixtureHistory) Read(context.Context, string) (appserver.Thread, error) {
	return appserver.Thread{ID: "thread", CWD: h.cwd, Status: json.RawMessage(`{"type":"idle"}`)}, nil
}

type taskFixtureLive struct {
	*fakeLive
	cwd string
}

func (l taskFixtureLive) Current() (json.RawMessage, error) {
	raw, err := l.fakeLive.Current()
	cwd, _ := json.Marshal(l.cwd)
	return json.RawMessage(strings.Replace(string(raw), `"/fixture"`, string(cwd), 1)), err
}

func TestTaskManagerWithoutCodexDispatchesThroughRelayWithoutBrowser(t *testing.T) {
	runTaskRoundTripFixture(t, false, false)
}
func TestTaskNativeDisconnectPreservesIdentityForReadOnlyRecovery(t *testing.T) {
	runTaskRoundTripFixture(t, false, true)
}
func TestReadOnlyTrackingBusyDoesNotRequeueAcceptedNativeWork(t *testing.T) {
	backend := &fixtureTaskBackend{receipts: make(chan protocol.TaskReceipt, 1)}
	a := &agent{deviceID: "manager"}
	runtime := newTaskRuntime(a, Config{TasksEnabled: true, TaskBackend: backend})
	c := protocol.TaskCommand{CommandID: "command", LeaseToken: "lease", State: "accepted"}
	f := protocol.TaskFrame{Type: "task.track", DispatchID: "dispatch", Command: &c}
	runtime.manager[f.DispatchID] = f
	reply := f.CloneAs("task.receipt")
	reply.Receipt = &protocol.TaskReceipt{CommandID: c.CommandID, LeaseToken: c.LeaseToken, Outcome: "busy", Reason: "DEVICE_OFFLINE"}
	runtime.saveReceipt(context.Background(), reply)
	if len(backend.receipts) != 0 || len(runtime.manager) != 0 {
		t.Fatal("readonly retry changed backend accepted state or retained a dead local route")
	}
}
func TestTaskTrackingDirectoryMustMatchNativeSnapshot(t *testing.T) {
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !taskSnapshotDirectoryMatches(map[string]any{"cwd": cwd}, cwd) || taskSnapshotDirectoryMatches(map[string]any{"cwd": "/another/private/project"}, cwd) {
		t.Fatal("readonly tracking accepted an unrelated native work directory")
	}
}
func TestTaskTurnObservationCannotWaitOnAnotherTurn(t *testing.T) {
	thread := map[string]any{"turns": []any{map[string]any{"turnId": "task-turn", "status": "completed"}, map[string]any{"turnId": "manual-turn", "status": "inProgress"}}, "pendingInteractions": []any{map[string]any{"interactionId": "manual-input"}}}
	status, matched := taskTurnObservation(thread, "task-turn")
	if status != "completed" || matched == nil {
		t.Fatalf("another native turn overrode exact completed turn: %q", status)
	}
	if status, matched := taskTurnObservation(thread, "not-present"); status != "" || matched != nil {
		t.Fatal("missing turn must remain unsynchronized, not be invented as running")
	}
}
func TestTaskManagerRestoresAcceptedStartWithoutStartingAgain(t *testing.T) {
	runTaskRoundTripFixture(t, true, false)
}
func runTaskRoundTripFixture(t *testing.T, recoverTracking, disconnectNative bool) {
	r, err := relay.New(relay.Config{Token: "legacy", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}, TasksEnabled: true, TaskDevices: map[string]relay.TaskDeviceCredential{
		"manager":  {Token: "manager-private-token-000000000000000001", TaskStoreIDs: []string{"store"}, ExecutionTargets: []string{"executor"}},
		"executor": {Token: "executor-private-token-00000000000000001", Execute: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	live := taskFixtureLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}, cwd: cwd}
	service := NewService(taskFixtureHistory{cwd: cwd}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executorReady := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		errs <- RunWithService(ctx, Config{URL: url, Token: "executor-private-token-00000000000000001", DeviceID: "executor", DeviceName: "Executor", TasksEnabled: true, TaskAllowedManagers: []string{"manager"}, TaskAllowedRoots: []string{cwd}, TaskExecutionCheck: func(context.Context, string, string) error { return nil }, OnReady: func() error { close(executorReady); return nil }}, service)
	}()
	select {
	case <-executorReady:
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("executor not ready")
	}
	backend := &fixtureTaskBackend{command: protocol.TaskCommand{CommandID: "command", TaskStoreID: "store", TaskID: "task", AttemptID: "attempt", ExecutionHostID: "executor", BindingGeneration: 1, TaskVersion: 1, Kind: "feedback", NativeThreadID: "thread", ClientMessageID: "client-message", Text: "fixture private transcript must not leave machine", LeaseToken: "lease", CWD: cwd}, receipts: make(chan protocol.TaskReceipt, 4), facts: make(chan protocol.TaskFact, 8)}
	if recoverTracking {
		backend.claimed = true
		backend.command.Kind = "start"
		backend.command.State = "accepted"
		backend.command.NativeTurnID = "one"
		live.mu.Lock()
		live.busy = true
		live.mu.Unlock()
	}
	go func() {
		errs <- RunWithService(ctx, Config{URL: url, Token: "manager-private-token-000000000000000001", DeviceID: "manager", DeviceName: "Manager", TasksEnabled: true, TasksManagerOnly: true, TaskBackend: backend, TaskPollInterval: 10 * time.Millisecond}, nil)
	}()
	select {
	case receipt := <-backend.receipts:
		if receipt.Outcome != "accepted" || receipt.NativeThreadID != "thread" || receipt.NativeTurnID != "one" {
			t.Fatalf("receipt=%+v", receipt)
		}
	case err := <-errs:
		t.Fatalf("agent ended: %v", err)
	case <-time.After(4 * time.Second):
		t.Fatal("background dispatch missing")
	}
	select {
	case fact := <-backend.facts:
		if fact.Status != "running" || fact.NativeThreadID != "thread" || fact.SourceSeq != 0 {
			t.Fatalf("fact=%+v", fact)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("background fact missing")
	}
	if disconnectNative {
		live.Close()
		select {
		case receipt := <-backend.receipts:
			if receipt.Outcome != "unknown" || receipt.NativeThreadID != "thread" || receipt.NativeTurnID != "one" || receipt.ClientMessageID != "client-message" {
				t.Fatalf("native disconnection lost exact recovery identity: %+v", receipt)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("native subscription failure left accepted route stuck")
		}
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.validations != 1 {
		t.Fatalf("lease validations=%d", backend.validations)
	}
}
