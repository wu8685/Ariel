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

// Opt-in because it creates and then deletes one empty Codex thread. The cwd
// is a fresh temporary directory and no turn or tool execution is started.
func TestRealCreateEmptyThreadInIsolatedProject(t *testing.T) {
	if os.Getenv("ARIEL_TEST_THREAD_CREATE") != "1" {
		t.Skip("set ARIEL_TEST_THREAD_CREATE=1 to verify isolated native thread creation")
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
	created, err := appserver.NewThreadCreator(binary).Create(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := appserver.Start(ctx, binary, cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		var out struct{}
		if err := reader.Session.Call(cleanupCtx, "thread/delete", map[string]any{"threadId": created.ID}, &out); err != nil {
			t.Errorf("delete isolated thread %s: %v", created.ID, err)
		}
	}()
	stored, err := (appserver.HistoryReader{RPC: reader.Session}).Read(ctx, created.ID)
	if err != nil || stored.ID != created.ID || stored.CWD != cwd || len(stored.Turns) != 0 {
		t.Fatalf("created=%+v stored=%+v err=%v", created, stored, err)
	}
	t.Logf("created empty thread %s in isolated cwd, closed creator, and read it back independently", created.ID)
}
