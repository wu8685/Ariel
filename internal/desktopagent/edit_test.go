package desktopagent

import (
	"context"
	"encoding/json"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"testing"
)

type editableLive struct {
	*staticLive
	writes  int
	failure error
}

func (l *editableLive) EditLatest(_ context.Context, turn, item, hash, text string) (string, error) {
	l.writes++
	return "replacement", l.failure
}
func TestEditServiceFencesTargetAndDeduplicatesUnknown(t *testing.T) {
	raw := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[{"turnId":"old","status":"interrupted","params":{"input":[{"type":"text","text":"old"}]},"items":[{"id":"user","type":"userMessage","content":[{"type":"text","text":"old"}]}]}]}`)
	live := &editableLive{staticLive: &staticLive{state: raw, fakeLive: &fakeLive{updates: make(chan struct{}, 1)}}}
	s := NewService(fakeHistory{}, nil)
	defer s.Close()
	s.editEnabled = true
	s.threads["thread"] = &threadController{id: "thread", cwd: "/fixture", live: live, current: map[string]any{"runtime": "idle"}, subs: map[string]*subscription{}}
	target, e := s.ReadEditTarget(context.Background(), "thread")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.EditLatest(context.Background(), "thread", "operation", "other", target.ItemID, target.Fingerprint, "new"); e == nil || live.writes != 0 {
		t.Fatal("stale write reached native")
	}
	live.failure = &desktopipc.CallError{Cause: context.DeadlineExceeded, Outcome: "unknown"}
	if _, e = s.EditLatest(context.Background(), "thread", "op-unknown", target.TurnID, target.ItemID, target.Fingerprint, "new"); e == nil {
		t.Fatal("unknown accepted")
	}
	if _, e = s.EditLatest(context.Background(), "thread", "op-unknown", target.TurnID, target.ItemID, target.Fingerprint, "new"); e == nil || live.writes != 1 {
		t.Fatal("unknown replayed")
	}
	s.editEnabled = false
	if _, e = s.ReadEditTarget(context.Background(), "thread"); e == nil {
		t.Fatal("unverified enabled")
	}
}
