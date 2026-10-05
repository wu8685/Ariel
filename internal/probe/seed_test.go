package probe

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSeedRequiresGuardAndVerifiesLiteralReply(t *testing.T) {
	workspace := t.TempDir()
	os.WriteFile(filepath.Join(workspace, ".ariel-fixture"), []byte("guard"), 0600)
	m := Manifest{SchemaVersion: 1, ThreadID: "fixture", Workspace: workspace, CreatedByProbe: true, CreatedAt: time.Now(), GuardID: "guard"}
	for _, tc := range []struct {
		enabled bool
		reply   string
		ok      bool
	}{{false, "ARIEL_SEED_OK", false}, {true, "ARIEL_SEED_OK", true}, {true, "unexpected", false}} {
		f := &seedRPC{cwd: workspace, reply: tc.reply}
		err := SeedFixture(context.Background(), f, m, tc.enabled, time.Millisecond)
		if (err == nil) != tc.ok {
			t.Fatalf("case %+v got %v", tc, err)
		}
		if !tc.enabled && f.started {
			t.Fatal("write happened without flag")
		}
	}
}

func TestSeedReadsCompletedTurnWhenDefaultHistoryIsPaged(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, ".ariel-fixture"), []byte("guard"), 0600); err != nil {
		t.Fatal(err)
	}
	m := Manifest{SchemaVersion: 1, ThreadID: "fixture", Workspace: workspace, CreatedByProbe: true, CreatedAt: time.Now(), GuardID: "guard"}
	f := &seedRPC{cwd: workspace, reply: "ARIEL_SEED_OK", paged: true}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := SeedFixture(ctx, f, m, true, time.Millisecond); err != nil {
		t.Fatalf("completed seed was not verified: %v", err)
	}
	if !f.fullReadSeen {
		t.Fatal("seed verifier did not request the completed turn")
	}
}

type seedRPC struct {
	cwd, reply   string
	started      bool
	paged        bool
	fullReadSeen bool
}

func (f *seedRPC) Call(ctx context.Context, method string, params any, result any) error {
	var value any
	switch method {
	case "thread/read":
		turns := []any{}
		includeTurns := params.(map[string]any)["includeTurns"] == true
		f.fullReadSeen = f.fullReadSeen || includeTurns
		if f.started && (!f.paged || includeTurns) {
			turns = append(turns, map[string]any{"id": "seed-turn", "status": "completed", "items": []any{map[string]any{"type": "agentMessage", "text": f.reply}}})
		}
		value = map[string]any{"thread": map[string]any{"id": "fixture", "cwd": f.cwd, "turns": turns}}
	case "turn/start":
		f.started = true
		value = map[string]any{"turn": map[string]any{"id": "seed-turn"}}
	default:
		return errors.New("unexpected method")
	}
	b, _ := json.Marshal(value)
	return json.Unmarshal(b, result)
}
