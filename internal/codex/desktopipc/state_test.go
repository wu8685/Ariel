package desktopipc

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestReducerDoesNotExpandHTMLCharactersInTransportJSON(t *testing.T) {
	const limit = 2048
	text := strings.Repeat("<>&", 300) + "\"\\\n你好"
	quoted, _ := json.Marshal(text)
	// Match the native transport: literal HTML characters are valid JSON.
	quoted = bytes.ReplaceAll(quoted, []byte(`\u003c`), []byte("<"))
	quoted = bytes.ReplaceAll(quoted, []byte(`\u003e`), []byte(">"))
	quoted = bytes.ReplaceAll(quoted, []byte(`\u0026`), []byte("&"))
	for _, mode := range []string{"snapshot", "patch"} {
		t.Run(mode, func(t *testing.T) {
			r := NewReducer(limit)
			body := append([]byte(`{"type":"snapshot","revision":1,"conversationState":{"text":`), quoted...)
			body = append(body, []byte(`,"large":9007199254740993}}`)...)
			if mode == "patch" {
				if err := r.Apply(json.RawMessage(`{"type":"snapshot","revision":0,"conversationState":{"text":"","large":9007199254740993}}`)); err != nil {
					t.Fatal(err)
				}
				body = append([]byte(`{"type":"patches","baseRevision":0,"revision":1,"patches":[{"op":"replace","path":["text"],"value":`), quoted...)
				body = append(body, []byte(`}]}`)...)
			}
			if len(body) >= limit {
				t.Fatal("fixture does not fit native input limit")
			}
			if err := r.Apply(body); err != nil {
				t.Fatalf("valid transport JSON was expanded beyond its limit: %v", err)
			}
			state, revision, ok := r.Snapshot()
			var got struct {
				Text  string      `json:"text"`
				Large json.Number `json:"large"`
			}
			if !ok || revision != 1 || len(state) > limit || json.Unmarshal(state, &got) != nil || got.Text != text || got.Large.String() != "9007199254740993" {
				t.Fatalf("transport semantics changed: bytes=%d revision=%d valid=%v", len(state), revision, ok)
			}
		})
	}
}

func TestReducerSizeErrorsRemainInvalidAndAreClassifiedAsFrameTooLarge(t *testing.T) {
	for _, mode := range []string{"envelope", "snapshot", "patch"} {
		t.Run(mode, func(t *testing.T) {
			r := NewReducer(64)
			if err := r.Apply(json.RawMessage(`{"type":"snapshot","revision":1,"conversationState":{"text":"old"}}`)); err != nil {
				t.Fatal(err)
			}
			var body string
			switch mode {
			case "envelope":
				body = `{"type":"snapshot","revision":2,"conversationState":{"text":"` + strings.Repeat("x", 66<<10) + `"}}`
			case "snapshot":
				body = `{"type":"snapshot","revision":2,"conversationState":{"text":"` + strings.Repeat("x", 65) + `"}}`
			case "patch":
				body = `{"type":"patches","baseRevision":1,"revision":2,"patches":[{"op":"replace","path":["text"],"value":"` + strings.Repeat("x", 65) + `"}]}`
			}
			err := r.Apply(json.RawMessage(body))
			if !errors.Is(err, ErrResyncRequired) || !errors.Is(err, ErrFrameTooLarge) {
				t.Fatalf("oversize lost its capacity classification: %v", err)
			}
			if _, _, ok := r.Snapshot(); ok {
				t.Fatal("oversize state remains usable")
			}
		})
	}
}

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

func TestReducerIgnoresDetailPatchBelowProjectedSentinelButAdvancesRevision(t *testing.T) {
	r := NewReducer(4096)
	if err := r.Apply(json.RawMessage(`{"type":"snapshot","revision":1,"conversationState":{"items":[{"id":"tool","result":"__ARIEL_LAZY_OMITTED__"}],"requests":[]}}`)); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(json.RawMessage(`{"type":"patches","baseRevision":1,"revision":2,"patches":[{"op":"replace","path":["items",0,"result","content"],"value":"new detail"}]}`)); err != nil {
		t.Fatal(err)
	}
	state, revision, ok := r.Snapshot()
	if !ok || revision != 2 || !bytes.Contains(state, []byte(`"result":"__ARIEL_LAZY_OMITTED__"`)) || bytes.Contains(state, []byte("new detail")) {
		t.Fatalf("projected subtree changed or revision stalled: revision=%d state=%s", revision, state)
	}
}
