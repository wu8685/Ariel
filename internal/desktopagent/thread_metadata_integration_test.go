package desktopagent

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/probe"
)

// Opt-in because it creates, persists, renames, pins, unpins and finally
// deletes one isolated Codex thread. It never starts a turn.
func TestRealRenameAndPinIsolatedThread(t *testing.T) {
	if os.Getenv("ARIEL_TEST_THREAD_METADATA") != "1" {
		t.Skip("set ARIEL_TEST_THREAD_METADATA=1 to verify isolated native metadata operations")
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
	endpoint, err := appserver.Start(ctx, probe.BundledBinary(cfg.AppPath), cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	rpc, err := appserver.NewRestartingRPC(ctx, func(context.Context) (appserver.ReadOnlyEndpoint, error) { return endpoint, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
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
			t.Errorf("delete isolated metadata fixture: %v", err)
		}
	}()
	var injected struct{}
	if err := endpoint.Session.Call(ctx, "thread/inject_items", map[string]any{"threadId": created.ID, "items": []any{map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "isolated metadata fixture"}}}}}, &injected); err != nil {
		t.Fatal(err)
	}
	reader := appserver.HistoryReader{RPC: rpc}
	before, err := reader.ReadFull(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	writer := appserver.ThreadMetadataWriter{RPC: rpc}
	name := "Ariel metadata fixture " + rand.Text()
	renamed, err := writer.Rename(ctx, created.ID, name)
	if err != nil || renamed.Name != name {
		t.Fatalf("rename stored=%+v err=%v", renamed, err)
	}
	pinned, err := writer.Pin(ctx, created.ID, true)
	if err != nil || pinned.Section == nil || pinned.Section.ID != appserver.PinnedSectionID {
		t.Fatalf("pin stored=%+v err=%v", pinned, err)
	}
	unpinned, err := writer.Pin(ctx, created.ID, false)
	if err != nil || (unpinned.Section != nil && unpinned.Section.ID == appserver.PinnedSectionID) {
		t.Fatalf("unpin stored=%+v err=%v", unpinned, err)
	}
	projection, err := NormalizeStored(unpinned)
	if err != nil || projection["isPinned"] != false {
		t.Fatalf("unpin projection=%+v err=%v", projection, err)
	}
	after, err := reader.ReadFull(ctx, created.ID)
	if err != nil || after.Name != name || len(after.Turns) != len(before.Turns) {
		t.Fatalf("metadata mutation changed conversation: before=%+v after=%+v err=%v", before, after, err)
	}
}
