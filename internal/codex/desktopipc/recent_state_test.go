package desktopipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// The public Follow path, including framing and the real consumer. The peer
// permits only read-only follow/load and explicit fixture barriers.
func recentStateFollower(t testing.TB, state any) (*Follower, func(any)) {
	t.Helper()
	local, peer := net.Pipe()
	client := NewClient(local, Options{ThreadID: "thread", MaxFrameBytes: 64 << 20, MaxWireFrameBytes: 256 << 20, MaxEvents: 1024, MaxEventBytes: 128 << 20, RequestTimeout: 10 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	changes := make(chan any, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer peer.Close()
		for {
			raw, err := ReadFrame(peer, 64<<20)
			if err != nil {
				return
			}
			var request map[string]any
			if json.Unmarshal(raw, &request) != nil {
				return
			}
			method, _ := request["method"].(string)
			var change any
			switch method {
			case "thread-stream-following-changed":
				continue
			case "thread-follower-load-complete-history":
				change = map[string]any{"type": "snapshot", "revision": 1, "conversationState": state}
				state = nil
			case "fixture-change":
				change = <-changes
			default:
				return // Never allow native mutation in this fixture.
			}
			reply, _ := json.Marshal(map[string]any{"type": "response", "requestId": request["requestId"], "resultType": "success", "handledByClientId": "owner", "result": map[string]any{}})
			if WriteFrame(peer, reply, 64<<20) != nil {
				return
			}
			body, _ := json.Marshal(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"conversationId": "thread", "hostId": "local", "change": change}})
			if WriteFrame(peer, body, 64<<20) != nil {
				return
			}
		}
	}()
	f, err := Follow(ctx, client, "thread", "owner", "/fixture")
	if err != nil {
		cancel()
		peer.Close()
		client.Close()
		<-done
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); peer.Close(); f.Close(); <-done })
	apply := func(change any) {
		t.Helper()
		changes <- change
		if _, err := client.Call(ctx, Request{Method: "fixture-change"}); err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(change)
		var expected struct {
			Revision int64 `json:"revision"`
		}
		json.Unmarshal(encoded, &expected)
		for {
			f.stateMu.Lock()
			revision, failure := f.o.summary.Revision, f.err
			f.stateMu.Unlock()
			if failure != nil {
				return
			}
			if revision >= expected.Revision {
				return
			}
			select {
			case <-f.wake:
			case <-f.Done():
				return
			case <-ctx.Done():
				t.Fatal("change was not consumed")
			}
		}
	}
	return f, apply
}

func recentCanonicalState(oldBytes int) map[string]any {
	entities := map[string]any{}
	entries := []any{}
	for i := range 4 {
		key := fmt.Sprint(i)
		text := "recent-" + key
		if i == 0 {
			text = strings.Repeat("old-body", oldBytes/8)
		}
		entities[key] = map[string]any{"turnId": "turn-" + key, "status": "completed", "items": []any{map[string]any{"type": "agentMessage", "id": "item-" + key, "text": text}}}
		entries = append(entries, map[string]any{"value": key})
	}
	return map[string]any{"cwd": "/fixture", "requests": []any{}, "threadRuntimeStatus": map[string]any{"type": "idle"}, "turnHistory": map[string]any{"kind": "canonical", "history": map[string]any{"islands": []any{map[string]any{"entries": entries}}, "entitiesByKey": entities}}}
}

func TestFollowerReclaimsHistoricalBodiesWithoutReindexing(t *testing.T) {
	f, apply := recentStateFollower(t, recentCanonicalState(1<<20))
	raw, err := f.Current()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 16<<10 || bytes.Contains(raw, []byte("old-body")) {
		t.Fatalf("historical bodies still retained/exported: %d bytes", len(raw))
	}
	for _, marker := range []string{"turn-0", "turn-1", "recent-2", "recent-3"} {
		if !bytes.Contains(raw, []byte(marker)) {
			t.Fatalf("lost identity/recent body %q", marker)
		}
	}
	apply(map[string]any{"type": "patches", "baseRevision": 1, "revision": 2, "patches": []any{
		map[string]any{"op": "replace", "path": []any{"turnHistory", "history", "entitiesByKey", "0", "items", 0, "text"}, "value": strings.Repeat("retired-update", 1024)},
		map[string]any{"op": "replace", "path": []any{"turnHistory", "history", "entitiesByKey", "3", "items", 0, "text"}, "value": "latest complete"},
	}})
	raw, err = f.Current()
	if err != nil || bytes.Contains(raw, []byte("retired-update")) || !bytes.Contains(raw, []byte("latest complete")) {
		t.Fatalf("patch lost continuity or resurrected retired body: %v", err)
	}
}

func TestRecentReducerReclaimsFiftyMiBHistoricalBodies(t *testing.T) {
	r := NewReducer(64 << 20)
	r.recent = true
	body, _ := json.Marshal(map[string]any{"type": "snapshot", "revision": 1, "conversationState": recentCanonicalState(50 << 20)})
	if err := r.Apply(body); err != nil {
		t.Fatal(err)
	}
	raw, _, ok := r.Snapshot()
	if !ok || len(raw) > 16<<10 || r.stateBytes != len(raw) || bytes.Contains(raw, []byte("old-body")) {
		t.Fatalf("50 MiB state not reclaimed: %d", len(raw))
	}
}

func TestRetiredPayloadDoesNotBypassPatchPathValidation(t *testing.T) {
	for _, part := range []any{false, nil, json.Number("-1"), json.Number("0.5")} {
		r := NewReducer(1 << 20)
		r.recent = true
		initial, _ := json.Marshal(map[string]any{"type": "snapshot", "revision": 1, "conversationState": recentCanonicalState(1024)})
		if err := r.Apply(initial); err != nil {
			t.Fatal(err)
		}
		change, _ := json.Marshal(map[string]any{"type": "patches", "baseRevision": 1, "revision": 2, "patches": []any{map[string]any{"op": "replace", "path": []any{"turnHistory", "history", "entitiesByKey", "0", "items", 0, "text", part}, "value": "must fail"}}})
		if err := r.Apply(change); err == nil {
			t.Fatalf("retired body accepted invalid path component %v", part)
		}
	}
}

func TestRecentWindowPreservesSharedCanonicalEntity(t *testing.T) {
	state := recentCanonicalState(1024)
	history := state["turnHistory"].(map[string]any)["history"].(map[string]any)
	island := history["islands"].([]any)[0].(map[string]any)
	island["entries"] = append(island["entries"].([]any), map[string]any{"value": "0"})
	body, _ := json.Marshal(map[string]any{"type": "snapshot", "revision": 1, "conversationState": state})
	r := NewReducer(1 << 20)
	r.recent = true
	if err := r.Apply(body); err != nil {
		t.Fatalf("shared recent entity retired through an older reference: %v", err)
	}
	raw, _, ok := r.Snapshot()
	if !ok || !bytes.Contains(raw, []byte("old-body")) {
		t.Fatal("latest reference lost its body")
	}
}

func TestFollowerRetainsActiveAndPendingTurnBodiesOutsideWindow(t *testing.T) {
	for _, pending := range []bool{false, true} {
		state := recentCanonicalState(1024)
		entity := state["turnHistory"].(map[string]any)["history"].(map[string]any)["entitiesByKey"].(map[string]any)["0"].(map[string]any)
		if pending {
			state["requests"] = []any{map[string]any{"id": 1, "method": "item/tool/requestUserInput", "params": map[string]any{"turnId": "turn-0"}}}
		} else {
			entity["status"] = "inProgress"
		}
		f, _ := recentStateFollower(t, state)
		raw, err := f.Current()
		if err != nil || !bytes.Contains(raw, []byte("old-body")) {
			t.Fatalf("active/pending evidence was retired: %v", err)
		}
	}
}

func TestFollowerDoesNotPublishEvictedTurnAfterRollback(t *testing.T) {
	f, apply := recentStateFollower(t, recentCanonicalState(1024))
	apply(map[string]any{"type": "patches", "baseRevision": 1, "revision": 2, "patches": []any{
		map[string]any{"op": "remove", "path": []any{"turnHistory", "history", "islands", 0, "entries", 3}},
		map[string]any{"op": "remove", "path": []any{"turnHistory", "history", "islands", 0, "entries", 2}},
	}})
	if _, err := f.Current(); err == nil {
		t.Fatal("rollback published an evicted turn as complete instead of requiring a new baseline")
	}
}

func BenchmarkFollowerRecentStatePatch(b *testing.B) {
	for _, size := range []int{1 << 20, 50 << 20} {
		b.Run(fmt.Sprintf("history-%dMiB", size>>20), func(b *testing.B) {
			f, _ := recentStateFollower(b, recentCanonicalState(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				body := json.RawMessage(fmt.Sprintf(`{"type":"patches","baseRevision":%d,"revision":%d,"patches":[{"op":"replace","path":["turnHistory","history","entitiesByKey","3","items",0,"text"],"value":"working"}]}`, i+1, i+2))
				f.stateMu.Lock()
				err := f.o.reducer.Apply(body)
				f.stateMu.Unlock()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestFollowerPinsMutationEvidenceUntilVerificationCompletes(t *testing.T) {
	f, apply := recentStateFollower(t, recentCanonicalState(1024))
	release := f.pinHistoryForMutation()
	patches := []any{}
	for i := 4; i < 6; i++ {
		key := fmt.Sprint(i)
		patches = append(patches, map[string]any{"op": "add", "path": []any{"turnHistory", "history", "entitiesByKey", key}, "value": map[string]any{"turnId": "turn-" + key, "status": "completed", "items": []any{}}}, map[string]any{"op": "add", "path": []any{"turnHistory", "history", "islands", 0, "entries", i}, "value": map[string]any{"value": key}})
	}
	apply(map[string]any{"type": "patches", "baseRevision": 1, "revision": 2, "patches": patches})
	raw, err := f.Current()
	if err != nil || !bytes.Contains(raw, []byte("recent-2")) {
		t.Fatalf("in-flight proof was rotated out: %v", err)
	}
	release()
	release() // Release is idempotent, including cancellation cleanup.
	raw, err = f.Current()
	if err != nil || bytes.Contains(raw, []byte("recent-2")) {
		t.Fatalf("unpinned old body was not released: %v", err)
	}
}

func TestReducerRemovalClearsArrayBackingReferences(t *testing.T) {
	r := NewReducer(4096)
	if err := r.Apply(json.RawMessage(`{"type":"snapshot","revision":1,"conversationState":{"items":[{"text":"a"},{"text":"b"}]}}`)); err != nil {
		t.Fatal(err)
	}
	backing := r.state["items"].([]any)
	if err := r.Apply(json.RawMessage(`{"type":"patches","baseRevision":1,"revision":2,"patches":[{"op":"remove","path":["items",1]}]}`)); err != nil {
		t.Fatal(err)
	}
	if backing[1] != nil {
		t.Fatal("removed body is still retained by the slice backing array")
	}
}

func TestDecodedJSONSizeMatchesNonHTMLEscapingEncoder(t *testing.T) {
	for _, raw := range []string{`null`, `true`, `false`, `9007199254740993`, `"\\\"\n\r\t\b\f\u0001中😀<>&\u2028\u2029"`, `{"中\u2028":[{},[],true,null,1e+10,"<>&"]}`} {
		value, err := decodeValue([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
		if got, want := encodedValueSize(value), buffer.Len()-1; got != want {
			t.Fatalf("size=%d encoded=%d for %s", got, want, raw)
		}
	}
}
