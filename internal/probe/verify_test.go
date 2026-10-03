package probe

import (
	"encoding/json"
	"testing"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

func TestVerifyExerciseChecksExactTurnsAndStatuses(t *testing.T) {
	turns := []appserver.Turn{
		{ID: "reply", Status: "completed", Items: []json.RawMessage{json.RawMessage(`{"type":"agentMessage","text":"MARKER"}`)}},
		{ID: "stop", Status: "interrupted"},
	}
	if err := VerifyExercise(turns, "reply", "stop", "MARKER"); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func([]appserver.Turn){
		func(t []appserver.Turn) { t[0].ID = "different" },
		func(t []appserver.Turn) { t[0].Status = "failed" },
		func(t []appserver.Turn) { t[1].Status = "completed" },
		func(t []appserver.Turn) { t[0].Items = nil },
	} {
		copyTurns := append([]appserver.Turn(nil), turns...)
		change(copyTurns)
		if err := VerifyExercise(copyTurns, "reply", "stop", "MARKER"); err == nil {
			t.Fatal("false success")
		}
	}
}

func TestVerifyCompletedReplyDoesNotAcceptOtherTurns(t *testing.T) {
	turns := []appserver.Turn{{ID: "target", Status: "completed", Items: []json.RawMessage{json.RawMessage(`{"type":"agentMessage","text":"MARKER"}`)}}}
	if err := VerifyCompletedReply(turns, "target", "MARKER"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "other"} {
		if VerifyCompletedReply(turns, id, "MARKER") == nil {
			t.Fatal("wrong identity")
		}
	}
	if VerifyCompletedReply(turns, "target", "") == nil || VerifyCompletedReply(turns, "target", "other") == nil {
		t.Fatal("wrong marker")
	}
	turns[0].Status = "interrupted"
	if VerifyCompletedReply(turns, "target", "MARKER") == nil {
		t.Fatal("wrong terminal state")
	}
}

func TestVerifyDeclinedCommandChecksNativeItem(t *testing.T) {
	turns := []appserver.Turn{{ID: "turn", Status: "completed", Items: []json.RawMessage{json.RawMessage(`{"id":"item","type":"commandExecution","status":"declined"}`)}}}
	if VerifyDeclinedCommand(turns, "turn", "item") != nil {
		t.Fatal("decline not found")
	}
	if VerifyDeclinedCommand(turns, "other", "item") == nil || VerifyDeclinedCommand(turns, "turn", "other") == nil {
		t.Fatal("wrong identity")
	}
	turns[0].Items = []json.RawMessage{json.RawMessage(`{"id":"item","type":"commandExecution","status":"completed"}`)}
	if VerifyDeclinedCommand(turns, "turn", "item") == nil {
		t.Fatal("executed command accepted as declined")
	}
}

func TestVerifyCancelledCommandRequiresInterruptedTurn(t *testing.T) {
	turns := []appserver.Turn{{ID: "turn", Status: "interrupted", Items: []json.RawMessage{json.RawMessage(`{"id":"item","type":"commandExecution","status":"declined"}`)}}}
	if err := VerifyCancelledCommand(turns, "turn", "item"); err != nil {
		t.Fatal(err)
	}
	turns[0].Status = "completed"
	if VerifyCancelledCommand(turns, "turn", "item") == nil {
		t.Fatal("completed turn accepted as cancelled")
	}
	turns[0].Status = "interrupted"
	turns[0].Items = []json.RawMessage{json.RawMessage(`{"id":"item","type":"commandExecution","status":"completed"}`)}
	if VerifyCancelledCommand(turns, "turn", "item") == nil {
		t.Fatal("executed command accepted as cancelled")
	}
	turns[0].Items = []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"reason"}`)}
	if VerifyCancelledCommand(turns, "turn", "item") != nil {
		t.Fatal("nested command item omitted from public history")
	}
	if CommandItemInHistory(turns, "turn", "item") {
		t.Fatal("invented command item")
	}
}

func TestVerifyInterruptedTurnRequiresExactIdentity(t *testing.T) {
	turns := []appserver.Turn{{ID: "target", Status: "interrupted"}, {ID: "other", Status: "completed"}}
	if err := VerifyInterruptedTurn(turns, "target"); err != nil {
		t.Fatal(err)
	}
	if VerifyInterruptedTurn(turns, "other") == nil || VerifyInterruptedTurn(turns, "missing") == nil || VerifyInterruptedTurn(turns, "") == nil {
		t.Fatal("accepted wrong turn or status")
	}
}
