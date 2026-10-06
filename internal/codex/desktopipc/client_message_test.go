package desktopipc

import (
	"encoding/json"
	"testing"
)

func TestTurnContainsExactClientMessageInCanonicalHistory(t *testing.T) {
	state := json.RawMessage(`{"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"key"}]}],"entitiesByKey":{"key":{"turnId":"turn-new","status":"inProgress","items":[{"id":"native-item","clientId":"client-message","type":"userMessage","content":[{"type":"text","text":"hello"}]}]}}}}}`)
	if !TurnContainsClientMessage(state, "turn-new", "client-message", "hello") {
		t.Fatal("matching native clientId was not recognized")
	}
	for _, candidate := range []struct{ turn, client, text string }{{"other", "client-message", "hello"}, {"turn-new", "other-client", "hello"}, {"turn-new", "client-message", "different"}} {
		if TurnContainsClientMessage(state, candidate.turn, candidate.client, candidate.text) {
			t.Fatalf("wrong message accepted: %+v", candidate)
		}
	}
}

func TestTurnContainsExactClientMessageInLegacyHistory(t *testing.T) {
	state := json.RawMessage(`{"turns":[{"turnId":"turn-new","status":"completed","items":[{"id":"native-item","clientId":"client-message","type":"userMessage","content":[{"type":"text","text":"hello"}]}]}]}`)
	if !TurnContainsClientMessage(state, "turn-new", "client-message", "hello") {
		t.Fatal("legacy clientId missing")
	}
}

func TestTurnContainsAcceptedQueuedSteeringMessage(t *testing.T) {
	state := json.RawMessage(`{"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"key"}]}],"entitiesByKey":{"key":{"turnId":"turn-new","status":"inProgress","items":[{"type":"steeringUserMessage","id":"owner-item","targetTurnId":"turn-new","status":"accepted","clientUserMessageId":"client-message","input":[{"type":"text","text":"guide"},{"type":"image","url":"data:image/png;base64,AAAA"}],"restoreMessage":{"id":"queue-item","serverQueuedMessageId":"queue-item"}}]}}}}}`)
	if !TurnContainsSteeringMessageImages(state, "turn-new", "queue-item", "client-message", "guide", 1) {
		t.Fatal("matching accepted steering message was not recognized")
	}
	for _, candidate := range []struct {
		turn, queue, client, text string
		images                    int
	}{{"other", "queue-item", "client-message", "guide", 1}, {"turn-new", "other", "client-message", "guide", 1}, {"turn-new", "queue-item", "other", "guide", 1}, {"turn-new", "queue-item", "client-message", "different", 1}, {"turn-new", "queue-item", "client-message", "guide", 0}} {
		if TurnContainsSteeringMessageImages(state, candidate.turn, candidate.queue, candidate.client, candidate.text, candidate.images) {
			t.Fatalf("wrong steering message accepted: %+v", candidate)
		}
	}
	pending := json.RawMessage(`{"turns":[{"turnId":"turn-new","items":[{"type":"steeringUserMessage","targetTurnId":"turn-new","status":"pending","clientUserMessageId":"client-message","input":[{"type":"text","text":"guide"}],"restoreMessage":{"id":"queue-item","serverQueuedMessageId":"queue-item"}}]}]}`)
	if TurnContainsSteeringMessageImages(pending, "turn-new", "queue-item", "client-message", "guide", 0) {
		t.Fatal("optimistic pending steering message must not confirm delivery")
	}
}
