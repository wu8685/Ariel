package desktopagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/probe"
)

// Opt-in because it creates, persists, archives, restores and finally deletes
// one Codex thread in a fresh temporary project. It never starts a turn.
func TestRealArchiveAndUnarchiveIsolatedThread(t *testing.T) {
	if os.Getenv("ARIEL_TEST_THREAD_ARCHIVE") != "1" {
		t.Skip("set ARIEL_TEST_THREAD_ARCHIVE=1 to verify isolated native archive operations")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	binary := probe.BundledBinary(cfg.AppPath)
	endpoint, err := appserver.Start(ctx, binary, cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	var started struct {
		Thread appserver.Thread `json:"thread"`
	}
	if err := endpoint.Session.Call(ctx, "thread/start", map[string]any{"cwd": cwd, "ephemeral": false, "serviceName": "ariel-test"}, &started); err != nil {
		t.Fatal(err)
	}
	created := started.Thread
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		var out struct{}
		if err := endpoint.Session.Call(cleanupCtx, "thread/delete", map[string]any{"threadId": created.ID}, &out); err != nil {
			t.Errorf("delete isolated archived fixture: %v", err)
		}
	}()
	var injected struct{}
	if err := endpoint.Session.Call(ctx, "thread/inject_items", map[string]any{"threadId": created.ID, "items": []any{map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "isolated archive fixture"}}}}}, &injected); err != nil {
		t.Fatal(err)
	}

	archiver := appserver.ThreadArchiver{RPC: endpoint.Session}
	if err := archiver.Archive(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := archiver.Unarchive(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := (appserver.HistoryReader{RPC: endpoint.Session}).Read(ctx, created.ID)
	if err != nil || stored.ID != created.ID || stored.CWD != cwd || len(stored.Turns) != 0 {
		t.Fatalf("restored=%+v err=%v", stored, err)
	}
}
