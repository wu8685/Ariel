package desktopipc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const inputPending = `{"cwd":"/fixture","requests":[{"id":42,"method":"item/tool/requestUserInput","params":{"threadId":"thread","turnId":"turn","itemId":"item","isBlocking":true,"questions":[{"id":"choice","header":"Choice","question":"Choose","options":[{"label":"Blue","description":"blue"},{"label":"Green","description":"green"}]},{"id":"note","header":"Note","question":"Text"}]}}],"turns":[{"turnId":"turn","status":"inProgress"}]}`

func TestPendingUserInputValidatesIdentityAndQuestions(t *testing.T) {
	p, err := pendingUserInput(json.RawMessage(inputPending), "thread", "turn", "/fixture")
	if err != nil || string(p.ID) != "42" || len(p.Params.Questions) != 2 {
		t.Fatalf("pending: %+v %v", p, err)
	}
	for _, state := range []string{
		`{"cwd":"/fixture","requests":null}`,
		`{"cwd":"/other","requests":[]}`,
		`{"cwd":"/fixture","requests":[{"id":true,"method":"item/tool/requestUserInput","params":{"threadId":"thread","turnId":"turn"}}]}`,
		`{"cwd":"/fixture","requests":[{"id":"r","method":"unknown","params":{}}]}`,
		`{"cwd":"/fixture","requests":[{"id":"r","method":"item/tool/requestUserInput","params":{"threadId":"other","turnId":"turn","questions":[]}}]}`,
		`{"cwd":"/fixture","requests":[{"id":"r","method":"item/tool/requestUserInput","params":{"threadId":"thread","turnId":"turn","questions":[{"id":"q","isSecret":true}]}}]}`,
		`{"cwd":"/fixture","requests":[{"id":"r","method":"item/tool/requestUserInput","params":{"threadId":"thread","turnId":"turn","questions":[{"id":"q"},{"id":"q"}]}}]}`,
	} {
		if _, err := pendingUserInput(json.RawMessage(state), "thread", "turn", "/fixture"); err == nil {
			t.Fatalf("accepted invalid state %s", state)
		}
	}
}

type inputCaller struct {
	calls int
	req   Request
}

func (c *inputCaller) Call(_ context.Context, r Request) (Reply, error) {
	c.calls++
	c.req = r
	return Reply{Result: json.RawMessage(`{"ok":true}`)}, nil
}

func TestUserInputResponsePreservesIDAndRechecksPending(t *testing.T) {
	p, err := pendingUserInput(json.RawMessage(inputPending), "thread", "turn", "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	c := &inputCaller{}
	answers := map[string][]string{"choice": {"Blue"}, "note": {"free text"}}
	if err := submitUserInput(context.Background(), c, "owner", "/fixture", json.RawMessage(inputPending), p, answers); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(c.req.Params)
	var got struct {
		RequestID json.RawMessage `json:"requestId"`
		Response  struct {
			Answers map[string]struct {
				Answers []string `json:"answers"`
			} `json:"answers"`
		} `json:"response"`
	}
	json.Unmarshal(body, &got)
	if string(got.RequestID) != "42" || got.Response.Answers["note"].Answers[0] != "free text" || c.req.Method != "thread-follower-submit-user-input" || !c.req.Mutating {
		t.Fatalf("wrong mapping: %s", body)
	}
	for _, bad := range []map[string][]string{{"choice": {"Blue"}}, {"choice": {"Blue"}, "note": {"ok"}, "unknown": {"bad"}}, {"choice": {}, "note": {"ok"}}} {
		if err := submitUserInput(context.Background(), c, "owner", "/fixture", json.RawMessage(inputPending), p, bad); err == nil {
			t.Fatal("invalid answers sent")
		}
	}
	if err := submitUserInput(context.Background(), c, "owner", "/fixture", json.RawMessage(`{"cwd":"/fixture","requests":[]}`), p, answers); err == nil {
		t.Fatal("expired request sent")
	}
	if c.calls != 1 {
		t.Fatalf("unsafe sends: %d", c.calls)
	}
}

func TestUserInputRejectsInterruptedTurnEvenIfRequestLingers(t *testing.T) {
	state := json.RawMessage(strings.Replace(inputPending, `"status":"inProgress"`, `"status":"interrupted"`, 1))
	p, err := pendingUserInput(state, "thread", "turn", "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	c := &inputCaller{}
	answers := map[string][]string{"choice": {"Blue"}, "note": {"free text"}}
	if err := submitUserInput(context.Background(), c, "owner", "/fixture", state, p, answers); err == nil || c.calls != 0 {
		t.Fatal("submitted answer to an interrupted turn")
	}
}

func TestUserInputRejectsPendingWithoutKnownActiveTurn(t *testing.T) {
	state := json.RawMessage(strings.Replace(inputPending, `,"turns":[{"turnId":"turn","status":"inProgress"}]`, "", 1))
	p, err := pendingUserInput(state, "thread", "turn", "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	c := &inputCaller{}
	answers := map[string][]string{"choice": {"Blue"}, "note": {"free text"}}
	if err := submitUserInput(context.Background(), c, "owner", "/fixture", state, p, answers); err == nil || c.calls != 0 {
		t.Fatal("submitted answer without a known active turn")
	}
}

func TestUserInputEchoRequiresExactCompletedResponse(t *testing.T) {
	p, _ := pendingUserInput(json.RawMessage(inputPending), "thread", "turn", "/fixture")
	a := map[string][]string{"choice": {"Blue"}, "note": {"free text"}}
	state := json.RawMessage(`{"requests":[],"turns":[{"items":[{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":true,"answers":{"choice":["Blue"],"note":["free text"]}}]}]}`)
	if !userInputEchoMatches(state, p, a) {
		t.Fatal("matching echo not found")
	}
	for _, bad := range []string{
		`{"requests":[],"turns":[{"items":[{"type":"userInputResponse","requestId":"42","turnId":"turn","completed":true,"answers":{"choice":["Blue"],"note":["free text"]}}]}]}`,
		`{"requests":[],"turns":[{"items":[{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":true,"answers":{"choice":["Green"],"note":["free text"]}}]}]}`,
		`{"requests":[],"turns":[{"items":[{"type":"userInputResponse","requestId":42,"turnId":"other","completed":true,"answers":{"choice":["Blue"],"note":["free text"]}}]}]}`,
		`{"requests":[],"turns":[{"items":[{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":false,"answers":{"choice":["Blue"],"note":["free text"]}}]}]}`,
		`{"requests":null,"turns":[]}`,
	} {
		if userInputEchoMatches(json.RawMessage(bad), p, a) {
			t.Fatal("false echo accepted")
		}
	}
}

func TestUserInputEchoReadsCanonicalHistoryOnly(t *testing.T) {
	p, _ := pendingUserInput(json.RawMessage(inputPending), "thread", "turn", "/fixture")
	a := map[string][]string{"choice": {"Blue"}, "note": {"free text"}}
	state := `{"requests":[],"turns":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"entry"}]}],"entitiesByKey":{"entry":{"turnId":"turn","items":[{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":true,"answers":{"choice":["Blue"],"note":["free text"]}}]}}}}}`
	if !userInputEchoMatches(json.RawMessage(state), p, a) {
		t.Fatal("canonical echo ignored")
	}
	if userInputEchoMatches(json.RawMessage(strings.Replace(state, `"value":"entry"`, `"value":"missing"`, 1)), p, a) {
		t.Fatal("unreferenced echo accepted")
	}
	if userInputEchoMatches(json.RawMessage(strings.Replace(state, `"entries":[{"value":"entry"}]`, `"entries":[]`, 1)), p, a) {
		t.Fatal("orphaned entity accepted")
	}
}

func TestInteractionIDBindsFullRequestWithoutLeakingContents(t *testing.T) {
	a := json.RawMessage(`{"id":42,"method":"item/tool/requestUserInput","params":{"question":"secret","turnId":"t"}}`)
	reordered := json.RawMessage(`{"params":{"turnId":"t","question":"secret"},"method":"item/tool/requestUserInput","id":42}`)
	changed := json.RawMessage(`{"id":42,"method":"item/tool/requestUserInput","params":{"question":"changed","turnId":"t"}}`)
	id, err := InteractionID(a)
	if err != nil || id == "" || strings.Contains(id, "secret") || len(id) > 128 {
		t.Fatalf("unsafe interaction ID: %q %v", id, err)
	}
	other, err := InteractionID(reordered)
	if err != nil || other != id {
		t.Fatalf("semantic order changed ID: %q %q %v", id, other, err)
	}
	other, err = InteractionID(changed)
	if err != nil || other == id {
		t.Fatal("changed request reused ID")
	}
}

func TestFileInteractionIDChangesWhenDiffChanges(t *testing.T) {
	request := json.RawMessage(`{"id":7,"method":"item/fileChange/requestApproval","params":{"itemId":"file"}}`)
	a, err := InteractionIDWithContext(request, json.RawMessage(`{"id":"file","type":"fileChange","changes":[{"diff":"old"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := InteractionIDWithContext(request, json.RawMessage(`{"id":"file","type":"fileChange","changes":[{"diff":"new"}]}`))
	if err != nil || a == b {
		t.Fatal("changed diff reused approval card")
	}
}
