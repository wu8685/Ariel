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
