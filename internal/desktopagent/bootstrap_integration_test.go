package desktopagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

func TestAppServerBootstrapperAgainstIsolatedCodex(t *testing.T) {
	binary := os.Getenv("ARIEL_CODEX_BINARY")
	if binary == "" {
		t.Skip("set ARIEL_CODEX_BINARY for the isolated compatibility fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	messageID := "00000000-0000-4000-8000-000000000076"
	result, err := (AppServerBootstrapper{Binary: binary}).Bootstrap(ctx, cwd, messageID, "Reply with exactly ARIEL_PRODUCT_BOOTSTRAP_OK. Do not use tools.", nil)
	if err != nil {
		t.Fatal(err)
	}
	live := result.Live.(terminalBootstrapLive)
	t.Cleanup(func() {
		_ = live.Close()
		reader, startErr := appserver.Start(context.Background(), binary, cwd)
		if startErr == nil {
			_ = (appserver.ThreadArchiver{RPC: reader}).Archive(context.Background(), result.ThreadID)
			_ = reader.Close()
		}
	})
	select {
	case <-live.Terminal():
	case <-live.Done():
		owner := live.(*appServerBootstrapLive)
		t.Fatalf("bootstrap owner closed before terminal state: session=%v owner=%v", owner.process.Err(), owner.Failure())
	case <-ctx.Done():
		t.Fatal("bootstrap turn did not reach terminal state")
	}
	if err := live.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	thread, err := live.CurrentThread()
	if err != nil || thread["threadId"] != result.ThreadID || thread["cwd"] != cwd || thread["runtime"] != "idle" {
		t.Fatalf("thread=%+v err=%v", thread, err)
	}
}
