package desktopipc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const commandPending = `{"cwd":"/fixture","requests":[{"id":"request","method":"item/commandExecution/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"item","command":"/usr/bin/true","cwd":"/fixture","availableDecisions":["accept","decline"]}}]}`

func TestCommandDeclineMapsExactRequestWithoutGrant(t *testing.T) {
	p, err := pendingCommand(json.RawMessage(commandPending), "thread", "turn", "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	c := &inputCaller{}
	if err := declineCommand(context.Background(), c, "owner", "/fixture", json.RawMessage(commandPending), p); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c.req.Params)
	var got map[string]json.RawMessage
	json.Unmarshal(b, &got)
	if string(got["decision"]) != `"decline"` || string(got["requestId"]) != `"request"` || c.req.Method != "thread-follower-command-approval-decision" || c.req.Version != 1 {
		t.Fatalf("wrong decision: %s", b)
	}
	if err := declineCommand(context.Background(), c, "owner", "/fixture", json.RawMessage(`{"cwd":"/fixture","requests":[]}`), p); err == nil || c.calls != 1 {
		t.Fatal("expired decision sent")
	}
	for _, ids := range [][3]string{{"other", "turn", "/fixture"}, {"thread", "other", "/fixture"}, {"thread", "turn", "/other"}} {
		if _, err := pendingCommand(json.RawMessage(commandPending), ids[0], ids[1], ids[2]); err == nil {
			t.Fatal("wrong identity accepted")
		}
	}
}

func TestCommandDeclineExperimentOnlySendsOneDecision(t *testing.T) {
	h := &inputHarness{states: []json.RawMessage{json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`), json.RawMessage(commandPending), json.RawMessage(commandPending), json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`)}}
	r, err := exerciseCommandDecline(context.Background(), h, "owner", "thread", "/fixture")
	if err != nil || !r.ReceiptOK || !r.PendingCleared || r.ItemID != "item" || len(h.calls) != 2 {
		t.Fatalf("result: %+v %v", r, err)
	}
}

func TestCommandDeclineReportsUnsupportedPendingWithoutGrant(t *testing.T) {
	h := &inputHarness{states: []json.RawMessage{json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`), json.RawMessage(`{"cwd":"/fixture","requests":[{"id":1,"method":"item/permissions/requestApproval","params":{"threadId":"thread","turnId":"turn"}}]}`)}}
	r, err := exerciseCommandDecline(context.Background(), h, "owner", "thread", "/fixture")
	if err == nil || !strings.Contains(err.Error(), "command approval") || r.PendingMethod != "item/permissions/requestApproval" || r.ReceiptOK {
		t.Fatalf("diagnostics: %+v %v", r, err)
	}
	for _, c := range h.calls {
		if c.Method == "thread-follower-command-approval-decision" {
			t.Fatal("unexpected approval decision")
		}
	}
}

func TestCommandDeclineDiagnosesMultipleNativeRequests(t *testing.T) {
	h := &inputHarness{states: []json.RawMessage{json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`), json.RawMessage(`{"cwd":"/fixture","requests":[{"id":1,"method":"item/tool/call","params":{}},{"id":2,"method":"item/commandExecution/requestApproval","params":{}}]}`)}}
	r, err := exerciseCommandDecline(context.Background(), h, "owner", "thread", "/fixture")
	if err == nil || r.PendingCount != 2 || len(r.PendingMethods) != 2 || !r.StateCWDMatches {
		t.Fatalf("diagnostics: %+v %v", r, err)
	}
}

func TestCommandDeclineReportsActualDecisionSet(t *testing.T) {
	s := strings.Replace(commandPending, `"accept","decline"`, `"accept","cancel"`, 1)
	h := &inputHarness{states: []json.RawMessage{json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`), json.RawMessage(s)}}
	r, err := exerciseCommandDecline(context.Background(), h, "owner", "thread", "/fixture")
	if err == nil || len(r.DecisionKinds) != 2 || r.DecisionKinds[1] != "cancel" || !r.ItemIDPresent {
		t.Fatalf("decision diagnostics: %+v %v", r, err)
	}
}

func TestCommandCancelOnlyWhenNativeOffersIt(t *testing.T) {
	withCancel := strings.Replace(commandPending, `"accept","decline"`, `"accept",{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["/usr/bin/true"]}},"cancel"`, 1)
	p, err := pendingCommandForDecision(json.RawMessage(withCancel), "thread", "turn", "/fixture", "cancel")
	if err != nil {
		t.Fatal(err)
	}
	c := &inputCaller{}
	if err := respondCommandDecision(context.Background(), c, "owner", "/fixture", json.RawMessage(withCancel), p, "cancel"); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c.req.Params)
	var got map[string]json.RawMessage
	json.Unmarshal(b, &got)
	if string(got["decision"]) != `"cancel"` || string(got["requestId"]) != `"request"` || c.req.Method != "thread-follower-command-approval-decision" {
		t.Fatalf("wrong cancel mapping: %s", b)
	}
	if _, err := pendingCommandForDecision(json.RawMessage(commandPending), "thread", "turn", "/fixture", "cancel"); err == nil {
		t.Fatal("cancel was not offered")
	}
	if _, err := pendingCommandForDecision(json.RawMessage(withCancel), "thread", "turn", "/fixture", "decline"); err == nil {
		t.Fatal("decline was not offered")
	}
	for _, choice := range []string{"accept", "acceptForSession", "acceptWithExecpolicyAmendment", "unknown"} {
		if err := respondCommandDecision(context.Background(), c, "owner", "/fixture", json.RawMessage(withCancel), p, choice); err == nil {
			t.Fatalf("unsafe decision %s sent", choice)
		}
	}
	if err := respondCommandDecision(context.Background(), c, "owner", "/fixture", json.RawMessage(`{"cwd":"/fixture","requests":[]}`), p, "cancel"); err == nil {
		t.Fatal("expired cancel sent")
	}
	if c.calls != 1 {
		t.Fatalf("unexpected decision count: %d", c.calls)
	}
}

func TestCommandCancelExperimentReportsObservedStop(t *testing.T) {
	withCancel := strings.Replace(commandPending, `"accept","decline"`, `"accept","cancel"`, 1)
	h := &inputHarness{states: []json.RawMessage{
		json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`),
		json.RawMessage(withCancel), json.RawMessage(withCancel),
		json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[{"turnId":"turn","status":"interrupted"}]}`),
	}}
	r, err := exerciseCommandCancel(context.Background(), h, "owner", "thread", "/fixture")
	if err != nil || !r.ReceiptOK || !r.PendingCleared || !r.LiveTurnStopped || r.CleanupInterrupted || len(h.calls) != 2 {
		t.Fatalf("result: %+v %v", r, err)
	}
	var p map[string]json.RawMessage
	b, _ := json.Marshal(h.calls[1].Params)
	json.Unmarshal(b, &p)
	if string(p["decision"]) != `"cancel"` {
		t.Fatalf("sent: %s", b)
	}
}

func TestCancelTurnStatusUsesExactCanonicalEntry(t *testing.T) {
	state := json.RawMessage(`{"turns":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"entry"}]}],"entitiesByKey":{"entry":{"turnId":"target","status":"interrupted"},"orphan":{"turnId":"other","status":"interrupted"}}}}}`)
	if status, ok := nativeTurnStatus(state, "target"); !ok || status != "interrupted" {
		t.Fatalf("status %q %v", status, ok)
	}
	if _, ok := nativeTurnStatus(state, "other"); ok {
		t.Fatal("orphaned entry accepted")
	}
	if _, ok := nativeTurnStatus(state, "missing"); ok {
		t.Fatal("wrong turn accepted")
	}
}
