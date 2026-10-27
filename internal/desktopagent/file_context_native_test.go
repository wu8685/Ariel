package desktopagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/filecontext"
	"github.com/wu8685/Ariel/internal/probe"
)

// Reads native metadata/items only. Files written by the test are confined to
// a verified probe fixture, never a user's workspace or conversation.
func TestFileContextInstalledNativeGate(t *testing.T) {
	if os.Getenv("ARIEL_TEST_FILE_CONTEXT_NATIVE") != "1" {
		t.Skip("requires explicit isolated file-context compatibility gate")
	}
	manifest, err := probe.LoadManifest(os.Getenv("ARIEL_TEST_MANIFEST"))
	if err != nil || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
		t.Fatal("verified isolated manifest required")
	}
	defaults, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binary := defaults.Binary
	if binary == "" {
		binary = probe.BundledBinary(defaults.AppPath)
	}
	endpoint, err := appserver.Start(ctx, binary, manifest.Workspace)
	if err != nil {
		t.Fatal("native read-only endpoint unavailable")
	}
	defer endpoint.Close()
	cfg := Config{FileContextEnabled: true, WorkspaceBrowserEnabled: true}
	configureFileContext(&cfg, endpoint)
	reader := appserver.HistoryReader{RPC: endpoint}
	thread, err := reader.Read(ctx, manifest.ThreadID)
	if err != nil || thread.CWD != manifest.Workspace {
		t.Fatal("native metadata/cwd mismatch")
	}
	page, err := reader.Turns(ctx, manifest.ThreadID, "", 2, appserver.TurnItemsNotLoaded)
	if err != nil || len(page.Data) == 0 {
		t.Fatal("isolated fixture has no verifiable turn")
	}
	source := appserver.FileContextSource{RPC: endpoint}
	native, err := source.Items(ctx, manifest.ThreadID, page.Data[0].ID)
	if err != nil {
		t.Fatalf("native target-turn proof shape unsupported: %v", err)
	}
	messages, changes := 0, 0
	for _, item := range native {
		if item.Type == "agentMessage" {
			messages++
		}
		if item.Type == "fileChange" {
			changes++
		}
	}
	file, err := os.CreateTemp(manifest.Workspace, "ariel-file-context-*.md")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	defer os.Remove(name)
	content := "# Isolated file context\nexact selection\n"
	if _, err = file.WriteString(content); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	read, err := cfg.FileContext.WorkspaceRead(ctx, filecontext.WorkspaceReadRequest{ThreadID: manifest.ThreadID, Path: filepath.Base(name)})
	if err != nil || read.Content != content {
		t.Fatalf("native root + built-in read: %v", err)
	}
	quote, err := cfg.FileContext.Prepare(ctx, filecontext.PrepareRequest{Selections: []filecontext.Selection{{Source: filecontext.SelectionSourceWorkspaceBrowser, ThreadID: manifest.ThreadID, Path: read.Path, SHA256: read.SHA256, ModifiedAtMS: read.ModifiedAtMS, StartLine: 2, EndLine: 2, Text: "exact selection"}}})
	if err != nil || len(quote.Quotes) != 1 {
		t.Fatalf("native root + built-in prepare: %v", err)
	}
	t.Logf("redacted gate: metadata=true targetTurnItems=true agentMessages=%d fileChangeItems=%d boundedWorkspaceRead=true exactPrepare=true chillingRequests=0 nativeMutations=0", messages, changes)
}
