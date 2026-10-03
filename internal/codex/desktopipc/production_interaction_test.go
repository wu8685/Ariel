package desktopipc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestProductionUserInputRequiresCurrentCardAndExactEcho(t *testing.T) {
	state := json.RawMessage(inputPending)
	var s struct {
		Requests []json.RawMessage `json:"requests"`
	}
	if err := json.Unmarshal(state, &s); err != nil {
		t.Fatal(err)
	}
	id, _ := InteractionID(s.Requests[0])
	caller := &inputCaller{}
	answers := map[string][]string{"choice": {"Blue"}, "note": {"free text"}}
	receipt, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", state, id, "answer", answers)
	if err != nil || caller.calls != 1 || receipt.Kind != "user_input" {
		t.Fatalf("submit: %+v %v calls=%d", receipt, err, caller.calls)
	}
	if receipt.Confirmed(json.RawMessage(`{"requests":[],"turns":[{"items":[{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":true,"answers":{"choice":["Green"],"note":["free text"]}}]}]}`)) {
		t.Fatal("another client's answer counted as this answer")
	}
	if !receipt.Confirmed(json.RawMessage(`{"requests":[],"turns":[{"items":[{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":true,"answers":{"choice":["Blue"],"note":["free text"]}}]}]}`)) {
		t.Fatal("exact echo not confirmed")
	}
	changed := json.RawMessage(strings.Replace(inputPending, `"question":"Choose"`, `"question":"Changed"`, 1))
	if _, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", changed, id, "answer", answers); err != ErrStaleInteraction || caller.calls != 1 {
		t.Fatalf("stale request was sent: %v calls=%d", err, caller.calls)
	}
	if _, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", state, id, "answer", map[string][]string{"choice": {"Blue"}}); err == nil || caller.calls != 1 {
		t.Fatal("incomplete answers were sent")
	}
}

func TestProductionCommandCancelRequiresNativeOfferAndInterruptedTurn(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/fixture","requests":[{"id":9,"method":"item/commandExecution/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"item","cwd":"/fixture","command":"/usr/bin/true","availableDecisions":["accept","cancel"]}}],"turns":[{"turnId":"turn","status":"inProgress"}]}`)
	var s struct {
		Requests []json.RawMessage `json:"requests"`
	}
	_ = json.Unmarshal(state, &s)
	id, _ := InteractionID(s.Requests[0])
	caller := &inputCaller{}
	receipt, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", state, id, "deny_and_stop", nil)
	if err != nil || caller.calls != 1 || caller.req.Method != "thread-follower-command-approval-decision" {
		t.Fatalf("submit: %+v %v calls=%d", receipt, err, caller.calls)
	}
	if receipt.Confirmed(json.RawMessage(`{"requests":[],"turns":[{"turnId":"turn","status":"inProgress"}]}`)) {
		t.Fatal("pending clear without interrupted turn accepted")
	}
	if !receipt.Confirmed(json.RawMessage(`{"requests":[],"turns":[{"turnId":"turn","status":"interrupted"}]}`)) {
		t.Fatal("cancel terminal state not confirmed")
	}
	if _, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", state, id, "deny", nil); err == nil || caller.calls != 1 {
		t.Fatal("unoffered decline was sent")
	}
}

func TestProductionCommandAcceptMapsOnlyNativeSingleAccept(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/fixture","requests":[{"id":11,"method":"item/commandExecution/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"item","cwd":"/fixture","command":"/usr/bin/true","availableDecisions":["accept",{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["/usr/bin/true"]}},"cancel"]}}],"turns":[{"turnId":"turn","status":"inProgress"}]}`)
	var s struct {
		Requests []json.RawMessage `json:"requests"`
	}
	_ = json.Unmarshal(state, &s)
	id, _ := InteractionID(s.Requests[0])
	caller := &inputCaller{}
	receipt, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", state, id, "accept_once", nil)
	if err != nil || caller.calls != 1 || receipt.Decision != "accept_once" {
		t.Fatalf("accept: %+v %v calls=%d", receipt, err, caller.calls)
	}
	encoded, _ := json.Marshal(caller.req.Params)
	if !strings.Contains(string(encoded), `"decision":"accept"`) || strings.Contains(string(encoded), "acceptWithExecpolicyAmendment") {
		t.Fatalf("unsafe native decision: %s", encoded)
	}
	if !receipt.Confirmed(json.RawMessage(`{"requests":[],"turns":[{"turnId":"turn","status":"inProgress"}]}`)) {
		t.Fatal("cleared exact approval not confirmed")
	}
	unoffered := json.RawMessage(strings.Replace(string(state), `"accept",`, ``, 1))
	var u struct {
		Requests []json.RawMessage `json:"requests"`
	}
	_ = json.Unmarshal(unoffered, &u)
	changedID, _ := InteractionID(u.Requests[0])
	if _, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", unoffered, changedID, "accept_once", nil); err == nil || caller.calls != 1 {
		t.Fatal("accept without literal native offer was sent")
	}
}

func TestProductionFileApprovalBindsDiffAndSendsLiteralOneTimeDecision(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/fixture","requests":[{"id":9,"method":"item/fileChange/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"file","grantRoot":null}}],"turns":[{"turnId":"turn","status":"inProgress","items":[{"id":"file","type":"fileChange","status":"inProgress","changes":[{"path":"/fixture/note.txt","kind":{"type":"add"},"diff":"approved\\n"}]}]}]}`)
	var s struct {
		Requests []json.RawMessage `json:"requests"`
	}
	_ = json.Unmarshal(state, &s)
	item, _ := FileChangeItem(state, "turn", "file")
	id, _ := InteractionIDWithContext(s.Requests[0], item)
	caller := &inputCaller{}
	receipt, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", state, id, "accept_once", nil)
	if err != nil || receipt.Kind != "file_approval" || caller.req.Method != "thread-follower-file-approval-decision" {
		t.Fatalf("file submit: %+v %v call=%+v", receipt, err, caller.req)
	}
	encoded, _ := json.Marshal(caller.req.Params)
	if !strings.Contains(string(encoded), `"decision":"accept"`) || strings.Contains(string(encoded), "acceptForSession") {
		t.Fatalf("unsafe decision: %s", encoded)
	}
	changed := json.RawMessage(strings.Replace(string(state), "approved", "changed", 1))
	if _, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", changed, id, "accept_once", nil); err != ErrStaleInteraction || caller.calls != 1 {
		t.Fatalf("changed diff sent: %v calls=%d", err, caller.calls)
	}
	completed := json.RawMessage(strings.Replace(strings.Replace(string(state), `"requests":[{"id":9,"method":"item/fileChange/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"file","grantRoot":null}}]`, `"requests":[]`, 1), `"status":"inProgress","changes"`, `"status":"completed","changes"`, 1))
	if !receipt.Confirmed(completed) {
		t.Fatal("completed exact file change not confirmed")
	}
}

func TestProductionPermissionRequestCopiesOnlyExactRequestedTurnProfile(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/fixture","requests":[{"id":91,"method":"item/permissions/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"permission","cwd":"/fixture","reason":"write fixture","permissions":{"network":null,"fileSystem":{"read":null,"write":["/fixture"],"entries":[]}}}}],"turns":[{"turnId":"turn","status":"inProgress"}]}`)
	var s struct {
		Requests []json.RawMessage `json:"requests"`
	}
	_ = json.Unmarshal(state, &s)
	id, _ := InteractionID(s.Requests[0])
	caller := &inputCaller{}
	receipt, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", state, id, "accept_once", nil)
	if err != nil || receipt.Kind != "permission_request" || caller.req.Method != "thread-follower-permissions-request-approval-response" {
		t.Fatalf("submit: %+v %v call=%+v", receipt, err, caller.req)
	}
	encoded, _ := json.Marshal(caller.req.Params)
	if !strings.Contains(string(encoded), `"scope":"turn"`) || !strings.Contains(string(encoded), `"write":["/fixture"]`) || strings.Contains(string(encoded), `"network"`) {
		t.Fatalf("unsafe grant: %s", encoded)
	}
	if !receipt.Confirmed(json.RawMessage(`{"requests":[],"turns":[{"turnId":"turn","status":"inProgress"}]}`)) {
		t.Fatal("resolved current request not confirmed")
	}
	changed := json.RawMessage(strings.Replace(string(state), `"write":["/fixture"]`, `"write":["/other"]`, 1))
	if _, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", changed, id, "accept_once", nil); err != ErrStaleInteraction || caller.calls != 1 {
		t.Fatalf("changed scope sent: %v calls=%d", err, caller.calls)
	}
}

func TestProductionPermissionDenialSendsEmptyTurnGrant(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/fixture","requests":[{"id":92,"method":"item/permissions/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"permission","cwd":"/fixture","reason":"network","permissions":{"network":{"enabled":true},"fileSystem":null}}}],"turns":[{"turnId":"turn","status":"inProgress"}]}`)
	var s struct {
		Requests []json.RawMessage `json:"requests"`
	}
	_ = json.Unmarshal(state, &s)
	id, _ := InteractionID(s.Requests[0])
	caller := &inputCaller{}
	receipt, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", state, id, "deny", nil)
	if err != nil || receipt.Kind != "permission_request" {
		t.Fatalf("denial: %+v %v", receipt, err)
	}
	encoded, _ := json.Marshal(caller.req.Params)
	if !strings.Contains(string(encoded), `"permissions":{}`) || !strings.Contains(string(encoded), `"scope":"turn"`) {
		t.Fatalf("denial profile: %s", encoded)
	}
	if _, err := SubmitProductionInteraction(context.Background(), caller, "owner", "thread", "/fixture", state, id, "deny_and_stop", nil); err != ErrStaleInteraction || caller.calls != 1 {
		t.Fatalf("unsupported permission decision sent: %v", err)
	}
}
