package relay

import (
	"encoding/json"
	"github.com/wu8685/Ariel/protocol"
	"net/http/httptest"
	"testing"
)

func TestTaskBackgroundReadinessDoesNotMisreportOnlineHostAsOffline(t *testing.T) {
	for _, online := range []bool{false, true} {
		t.Run(map[bool]string{false: "offline", true: "online-codex-not-ready"}[online], func(t *testing.T) {
			_, srv := taskFixtureRelay(t)
			m := dialTest(t, srv.URL, "")
			h := agentHello()
			h["deviceId"], h["token"], h["taskStoreId"] = "manager", "manager-private-token-000000000000000001", "store"
			h["capabilities"] = map[string]bool{"autoLoad": false, "codexReady": false, "taskManagement": true}
			sendJSON(t, m, h)
			readJSON(t, m)
			if online {
				e := dialTest(t, srv.URL, "")
				h = agentHello()
				h["deviceId"], h["token"] = "executor", "executor-private-token-00000000000000001"
				h["capabilities"] = map[string]bool{"autoLoad": false, "codexReady": false, "taskExecutionAuthorized": true, "taskExecution": false}
				sendJSON(t, e, h)
				readJSON(t, e)
			}
			c := protocol.TaskCommand{CommandID: "command", TaskStoreID: "store", TaskID: "task", AttemptID: "attempt", ExecutionHostID: "executor", BindingGeneration: 1, TaskVersion: 1, Kind: "start", ClientMessageID: "message", Text: "fixture", LeaseToken: "lease", CWD: "/fixture"}
			f := protocol.TaskFrame{Type: "task.dispatch", V: 1, DispatchID: "dispatch", ManagementDeviceID: "manager", TaskStoreID: "store", ExecutionHostID: "executor", Command: &c}
			sendJSON(t, m, f)
			got := readJSON(t, m)
			receipt, _ := got["receipt"].(map[string]any)
			wantReason := "DEVICE_OFFLINE"
			if online {
				wantReason = "TASK_EXECUTION_UNAVAILABLE"
			}
			if receipt["outcome"] != "busy" || receipt["reason"] != wantReason {
				t.Fatalf("readiness receipt=%v, want busy/%s", got, wantReason)
			}
		})
	}
}

func TestTaskPublicCreateRejectsTargetOutsideManagerAuthorization(t *testing.T) {
	s, _ := taskFixtureRelay(t)
	credential := s.cfg.TaskDevices["manager"]
	p := &peer{role: "agent", deviceID: "manager", taskCredential: &credential, taskStoreID: "store", capabilities: map[string]bool{"taskManagement": true}}
	for _, target := range []string{"executor", "outside-manager-scope"} {
		params, _ := json.Marshal(map[string]any{"taskStoreId": "store", "executionHostId": target})
		if got, want := s.taskPublicAllowed(p, "task.create", params), target == "executor"; got != want {
			t.Fatalf("create target %q allowed=%v, want %v; unauthorized tasks must never reach persistence", target, got, want)
		}
	}
}

func TestTaskBackgroundUnauthorizedTargetRejectsWithoutClosingManager(t *testing.T) {
	s, srv := taskFixtureRelay(t)
	m := dialTest(t, srv.URL, "")
	h := agentHello()
	h["deviceId"] = "manager"
	h["token"] = "manager-private-token-000000000000000001"
	h["taskStoreId"] = "store"
	h["capabilities"] = map[string]bool{"autoLoad": false, "codexReady": false, "taskManagement": true}
	sendJSON(t, m, h)
	readJSON(t, m)
	c := protocol.TaskCommand{CommandID: "command", TaskStoreID: "store", TaskID: "task", AttemptID: "attempt", ExecutionHostID: "outside-manager-scope", BindingGeneration: 1, TaskVersion: 1, Kind: "start", ClientMessageID: "message", Text: "fixture", LeaseToken: "lease", CWD: "/fixture"}
	f := protocol.TaskFrame{Type: "task.dispatch", V: 1, DispatchID: "dispatch", ManagementDeviceID: "manager", TaskStoreID: "store", ExecutionHostID: c.ExecutionHostID, Command: &c}
	sendJSON(t, m, f)
	got := readJSON(t, m)
	receipt, _ := got["receipt"].(map[string]any)
	if got["type"] != "task.receipt" || receipt["outcome"] != "failed" || receipt["reason"] != "TASK_UNAUTHORIZED" {
		t.Fatalf("unauthorized historical claim must be rejected without a native write: %+v", got)
	}
	c.ExecutionHostID, f.ExecutionHostID, f.DispatchID = "executor", "executor", "allowed-dispatch"
	sendJSON(t, m, f)
	got = readJSON(t, m)
	receipt, _ = got["receipt"].(map[string]any)
	if receipt["outcome"] != "busy" {
		t.Fatalf("authorized manager connection did not survive target rejection: %+v", got)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.taskRoutes) != 0 {
		t.Fatal("rejected target created a route")
	}
}

func taskFixtureRelay(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	r, err := New(Config{Token: "legacy", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, TasksEnabled: true, TaskDevices: map[string]TaskDeviceCredential{
		"manager":  {Token: "manager-private-token-000000000000000001", TaskStoreIDs: []string{"store"}, ExecutionTargets: []string{"executor"}},
		"executor": {Token: "executor-private-token-00000000000000001", Execute: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	t.Cleanup(s.Close)
	return r, s
}

func TestTaskBackgroundRouteWorksWithoutAnyBrowser(t *testing.T) {
	_, srv := taskFixtureRelay(t)
	m := dialTest(t, srv.URL, "")
	h := agentHello()
	h["deviceId"] = "manager"
	h["token"] = "manager-private-token-000000000000000001"
	h["taskStoreId"] = "store"
	h["capabilities"] = map[string]bool{"autoLoad": false, "codexReady": false, "taskManagement": true}
	sendJSON(t, m, h)
	readJSON(t, m)
	e := dialTest(t, srv.URL, "")
	h = agentHello()
	h["deviceId"] = "executor"
	h["token"] = "executor-private-token-00000000000000001"
	h["capabilities"] = map[string]bool{"autoLoad": true, "codexReady": true, "taskExecution": true}
	sendJSON(t, e, h)
	readJSON(t, e)
	c := protocol.TaskCommand{CommandID: "command", TaskStoreID: "store", TaskID: "task", AttemptID: "attempt", ExecutionHostID: "executor", BindingGeneration: 1, TaskVersion: 1, Kind: "start", ClientMessageID: "message", Text: "fixture", LeaseToken: "lease", CWD: "/fixture"}
	f := protocol.TaskFrame{Type: "task.dispatch", V: 1, DispatchID: "dispatch", ManagementDeviceID: "manager", TaskStoreID: "store", ExecutionHostID: "executor", Command: &c}
	sendJSON(t, m, f)
	if got := readJSON(t, e); got["type"] != "task.dispatch" {
		t.Fatalf("dispatch: %+v", got)
	}
	validate := f.CloneAs("task.validate")
	sendJSON(t, e, validate)
	if got := readJSON(t, m); got["type"] != "task.validate" {
		t.Fatalf("validate: %+v", got)
	}
	valid := true
	validated := f.CloneAs("task.validated")
	validated.Valid = &valid
	validated.Command = &c
	sendJSON(t, m, validated)
	if got := readJSON(t, e); got["type"] != "task.validated" {
		t.Fatalf("validated: %+v", got)
	}
	receipt := f.CloneAs("task.receipt")
	receipt.Receipt = &protocol.TaskReceipt{CommandID: "command", LeaseToken: "lease", Outcome: "accepted", NativeThreadID: "thread", NativeTurnID: "turn"}
	sendJSON(t, e, receipt)
	if got := readJSON(t, m); got["type"] != "task.receipt" {
		t.Fatalf("receipt: %+v", got)
	}
}

func TestTaskPublicGatewayRejectsLegacyAgentAsTaskManager(t *testing.T) {
	_, srv := taskFixtureRelay(t)
	a := dialTest(t, srv.URL, "")
	h := agentHello()
	h["deviceId"] = "ordinary"
	h["token"] = "legacy"
	h["taskStoreId"] = "store"
	h["capabilities"] = map[string]bool{"autoLoad": false, "codexReady": false, "taskManagement": true}
	sendJSON(t, a, h)
	readJSON(t, a)
	w := dialTest(t, srv.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000001", "deviceId": "ordinary", "method": "task.list", "params": map[string]any{"taskStoreId": "store"}})
	got := readJSON(t, w)
	if got["outcome"] != "rejected" {
		t.Fatalf("legacy task scope accepted: %+v", got)
	}
}
