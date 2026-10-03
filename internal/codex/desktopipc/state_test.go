package desktopipc

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestReducerSnapshotAndTransactionalPatches(t *testing.T) {
	r := NewReducer(4096)
	if err := r.Apply(json.RawMessage(`{"type":"snapshot","revision":5,"conversationState":{"items":[{"id":"a","text":"old"}],"requests":[],"large":9007199254740993}}`)); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(json.RawMessage(`{"type":"patches","baseRevision":5,"revision":7,"patches":[{"op":"replace","path":["items",0,"text"],"value":"新文本"},{"op":"add","path":["items",1],"value":{"id":"b"}},{"op":"remove","path":["items",0]},{"op":"add","path":["empty"],"value":null}]}`)); err != nil {
		t.Fatal(err)
	}
	state, revision, ok := r.Snapshot()
	if !ok || revision != 7 {
		t.Fatalf("revision=%d valid=%v", revision, ok)
	}
	var got map[string]json.RawMessage
	json.Unmarshal(state, &got)
	if string(got["items"]) != `[{"id":"b"}]` || string(got["large"]) != "9007199254740993" || string(got["empty"]) != "null" {
		t.Fatalf("state: %s", state)
	}
	state[0] = 'x'
	second, _, _ := r.Snapshot()
	if second[0] != '{' {
		t.Fatal("caller mutated reducer storage")
	}
}

func TestReducerInvalidatesOnGapsAndMalformedPatches(t *testing.T) {
	for _, change := range []string{
		`{"type":"patches","baseRevision":0,"revision":2,"patches":[]}`,
		`{"type":"patches","baseRevision":1,"revision":1,"patches":[]}`,
		`{"type":"patches","baseRevision":1,"revision":2}`,
		`{"type":"patches","baseRevision":1,"revision":2,"patches":null}`,
		`{"type":"patches","baseRevision":1,"revision":2,"patches":[{"op":"move","path":["items",0]}]}`,
		`{"type":"patches","baseRevision":1,"revision":2,"patches":[{"op":"replace","path":["missing"],"value":1}]}`,
		`{"type":"patches","baseRevision":1,"revision":2,"patches":[{"op":"remove","path":["items",9]}]}`,
		`{"type":"patches","baseRevision":1,"revision":2,"patches":[{"op":"remove","path":["items",-1]}]}`,
		`{"type":"patches","baseRevision":1,"revision":2,"patches":[{"op":"remove","path":["items",0.5]}]}`,
		`{"type":"patches","baseRevision":1,"revision":2,"patches":[{"op":"replace","path":["items"],"value":[]},{"op":"replace","path":["missing"],"value":1}]}`,
		`{"type":"patches","baseRevision":1,"revision":2,"patches":[{"op":"replace","path":["items"]}]}`,
		`{"type":"unexpected","revision":2}`,
		`{"type":"snapshot","revision":2,"conversationState":null}`,
		`{"type":"snapshot","revision":0,"conversationState":{}}`,
		`{"type":"snapshot","conversationState":{}}`,
	} {
		r := NewReducer(4096)
		if err := r.Apply(json.RawMessage(`{"type":"snapshot","revision":1,"conversationState":{"items":[{"id":"a"}]}}`)); err != nil {
			t.Fatal(err)
		}
		if err := r.Apply(json.RawMessage(change)); !errors.Is(err, ErrResyncRequired) {
			t.Fatalf("change %s: %v", change, err)
		}
		if _, _, ok := r.Snapshot(); ok {
			t.Fatalf("invalid state still usable after %s", change)
		}
		if err := r.Apply(json.RawMessage(`{"type":"snapshot","revision":9,"conversationState":{"items":[]}}`)); err != nil {
			t.Fatal(err)
		}
		if _, rev, ok := r.Snapshot(); !ok || rev != 9 {
			t.Fatal("fresh snapshot did not restore")
		}
	}
}

func TestReducerBoundsMemoryAndRequiresSnapshot(t *testing.T) {
	r := NewReducer(32)
	if err := r.Apply(json.RawMessage(`{"type":"patches","baseRevision":0,"revision":1,"patches":[]}`)); !errors.Is(err, ErrResyncRequired) {
		t.Fatal(err)
	}
	if err := r.Apply(json.RawMessage(`{"type":"snapshot","revision":0,"conversationState":{"text":"this payload exceeds the maximum size"}}`)); !errors.Is(err, ErrResyncRequired) {
		t.Fatal(err)
	}
	if _, _, ok := r.Snapshot(); ok {
		t.Fatal("oversize state retained")
	}
}
