package desktopagent

import (
	"context"
	"encoding/json"
	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/probe"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Creates and authorizes its own no-tools fixture; never accepts business IDs.
func TestRealEditStoppedLatestUserMessage(t *testing.T) {
	if os.Getenv("ARIEL_TEST_EDIT_WRITE") != "1" {
		t.Skip("requires explicit isolated edit gate")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if !verifiedEditBinary(ctx, probe.BundledBinary(cfg.AppPath)) {
		t.Fatal("installed edit profile not verified")
	}
	process, err := appserver.StartInteractive(ctx, probe.BundledBinary(cfg.AppPath), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	parent := t.TempDir()
	m, err := probe.CreateFixture(ctx, process, probe.CreateOptions{Parent: parent, ManifestPath: filepath.Join(parent, "fixture.json"), WriteEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Authorize(true, m.ThreadID, m.Workspace); err != nil {
		t.Fatal(err)
	}
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		p, e := appserver.Start(clean, probe.BundledBinary(cfg.AppPath), m.Workspace)
		if e == nil {
			defer p.Close()
			_ = (appserver.ThreadArchiver{RPC: p}).Archive(clean, m.ThreadID)
		}
	}()
	var seed struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err = process.Call(ctx, "turn/start", map[string]any{"threadId": m.ThreadID, "input": []any{map[string]any{"type": "text", "text": "Reply exactly ARIEL_EDIT_SEED. No tools.", "text_elements": []any{}}}}, &seed); err != nil {
		t.Fatal("seed", err)
	}
	for {
		select {
		case frame := <-process.Incoming():
			if frame.IsRequest() {
				t.Fatal("unexpected no-tools request")
			}
			if frame.Method == "turn/completed" {
				goto seeded
			}
		case <-ctx.Done():
			t.Fatal("seed timeout")
		}
	}
seeded:
	_ = process.Close()
	t.Log("isolated seed completed")
	live, err := OpenFollower(ctx, cfg.Socket, m.ThreadID, m.Workspace)
	if err != nil {
		t.Fatal("owner", err)
	}
	defer live.Close()
	f, ok := live.(*desktopipc.Follower)
	if !ok {
		t.Fatal("unexpected follower wrapper")
	}
	old, err := f.Start(ctx, fixtureMessageID(t), "Without tools, print integers 1 through 5000, one per line.")
	if err != nil {
		t.Fatal("start", err)
	}
	if err = f.Interrupt(ctx, old); err != nil {
		t.Fatal("interrupt", err)
	}
	var target desktopipc.EditTarget
	for {
		raw, e := f.Current()
		if e != nil {
			t.Fatal("state", e)
		}
		target, e = desktopipc.LatestEditTarget(raw, m.Workspace)
		if e == nil && target.TurnID == old {
			break
		}
		select {
		case <-f.Updates():
		case <-ctx.Done():
			t.Fatal("interrupted plain target not observed", e)
		}
	}
	t.Log("exact stopped target verified")
	settingsBefore, err := f.ExecutionSettings()
	if err != nil {
		t.Fatal("settings before edit", err)
	}
	beforeRaw, err := f.Current()
	if err != nil {
		t.Fatal(err)
	}
	var permissionsBefore map[string]json.RawMessage
	_ = json.Unmarshal(beforeRaw, &permissionsBefore)
	write, stop := context.WithTimeout(ctx, 20*time.Second)
	replacement, err := f.EditLatest(write, target.TurnID, target.ItemID, target.Fingerprint, "Reply exactly ARIEL_EDIT_REPLACED. No tools.")
	stop()
	if err != nil {
		raw, _ := f.Current()
		var state map[string]any
		_ = json.Unmarshal(raw, &state)
		if h, ok := state["turnHistory"].(map[string]any); ok {
			if history, ok := h["history"].(map[string]any); ok {
				if entities, ok := history["entitiesByKey"].(map[string]any); ok {
					for _, value := range entities {
						v, _ := value.(map[string]any)
						p, _ := v["params"].(map[string]any)
						t.Log("edit diagnostic", "old", v["turnId"] == old, "status", v["status"], "clientPresent", p["clientUserMessageId"] != nil, "sameClient", p["clientUserMessageId"] == target.ClientID, "trigger", p["turnTrigger"])
					}
				}
			}
		}
		t.Fatal("edit", err)
	}
	if replacement == old || replacement == "" {
		t.Fatal("no new turn identity")
	}
	t.Log("native owner replacement verified")
	settingsAfter, err := f.ExecutionSettings()
	if err != nil || settingsBefore != settingsAfter {
		t.Fatal("edit changed execution settings", err)
	}
	afterRaw, err := f.Current()
	if err != nil {
		t.Fatal(err)
	}
	var permissionsAfter map[string]json.RawMessage
	_ = json.Unmarshal(afterRaw, &permissionsAfter)
	for _, key := range []string{"approvalPolicy", "sandboxPolicy"} {
		if string(permissionsBefore[key]) != string(permissionsAfter[key]) {
			t.Fatal("edit changed permission policy")
		}
	}
	if _, err = f.EditLatest(ctx, target.TurnID, target.ItemID, target.Fingerprint, "Stale edit must never be sent"); err == nil {
		t.Fatal("stale edit accepted after replacement")
	}
	read, err := appserver.Start(ctx, probe.BundledBinary(cfg.AppPath), m.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	reader := appserver.HistoryReader{RPC: read}
	for {
		page, e := reader.Turns(ctx, m.ThreadID, "", 3, appserver.TurnItemsFull)
		if e != nil {
			t.Fatal("persisted", e)
		}
		found := false
		seedFound := false
		for _, turn := range page.Data {
			if turn.ID == old {
				t.Fatal("old turn still durable")
			}
			if turn.ID == seed.Turn.ID {
				seedFound = true
			}
			if turn.ID == replacement {
				body, _ := json.Marshal(turn.Items)
				found = turn.Status == "completed" && strings.Contains(string(body), "ARIEL_EDIT_REPLACED") && strings.Contains(string(body), "Reply exactly ARIEL_EDIT_REPLACED. No tools.")
			}
		}
		if found && seedFound {
			break
		}
		select {
		case <-time.After(150 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("durable replacement not confirmed")
		}
	}
	t.Log("durable prefix and completed replacement verified")
}
