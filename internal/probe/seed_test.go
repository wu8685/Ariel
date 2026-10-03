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

type seedRPC struct {
	cwd, reply string
	started    bool
}

func (f *seedRPC) Call(ctx context.Context, method string, params any, result any) error {
	var value any
	switch method {
	case "thread/read":
		turns := []any{}
		if f.started {
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
