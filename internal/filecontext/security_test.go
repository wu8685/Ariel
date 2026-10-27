package filecontext

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestBoundedFilesystemRejectsAncestorLinksAndSpecialFiles(t *testing.T) {
	s, f, path := fixture(t)
	s.workspaceBrowser = true
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.thread.CWD, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(f.thread.CWD, "fifo.md"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"escape/secret.md", "fifo.md", ".git/config", "internal/../escape/secret.md"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := s.WorkspaceRead(ctx, WorkspaceReadRequest{ThreadID: f.thread.ID, Path: rel})
		cancel()
		if CodeOf(err) != CodePathNotAllowed {
			t.Fatalf("%s: %v", rel, err)
		}
	}
	// Sparse files must be rejected from descriptor metadata, not allocated.
	file, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err = s.WorkspaceRead(context.Background(), WorkspaceReadRequest{ThreadID: f.thread.ID, Path: "internal/hello.go"}); CodeOf(err) != CodeFileTooLarge {
		t.Fatal(err)
	}
}

func TestChangesBudgetIsAggregateRatherThanPerTurn(t *testing.T) {
	s, f, _ := fixture(t)
	f.items[0].Changes = nil
	for i := 0; i < 20; i++ {
		f.items[0].Changes = append(f.items[0].Changes, Change{Path: strconv.Itoa(i) + ".go", Kind: "update", Diff: strings.Repeat("x", 64<<10)})
	}
	if _, err := s.Changes(context.Background(), ChangesRequest{ThreadID: "thread-a", TurnIDs: []string{"turn-a"}}); CodeOf(err) != CodeOverloaded {
		t.Fatalf("oversized changes accepted: %v", err)
	}
}

func TestEmptyDirectoryAndCancellation(t *testing.T) {
	s, f := workspaceFixture(t)
	got, err := s.WorkspaceList(context.Background(), WorkspaceListRequest{ThreadID: f.thread.ID, Path: "docs"})
	if err != nil || len(got.Entries) != 0 || got.NextCursor != "" {
		t.Fatalf("empty: %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.WorkspaceRead(ctx, WorkspaceReadRequest{ThreadID: f.thread.ID, Path: "README.md"}); CodeOf(err) != CodeTimeout {
		t.Fatalf("cancel: %v", err)
	}
}

func TestPrepareRejectsUnknownAndContradictorySources(t *testing.T) {
	s, _, _ := fixture(t)
	read, err := s.Read(context.Background(), ReadRequest{ThreadID: "thread-a", TurnID: "turn-a", ItemID: "item-a", Path: "internal/hello.go"})
	if err != nil {
		t.Fatal(err)
	}
	selection := Selection{Source: "forged", ThreadID: "thread-a", TurnID: "turn-a", ItemID: "item-a", Path: read.Path, SHA256: read.SHA256, ModifiedAtMS: read.ModifiedAtMS, StartLine: 1, EndLine: 1, Text: "package hello"}
	if _, err = s.Prepare(context.Background(), PrepareRequest{Selections: []Selection{selection}}); CodeOf(err) != CodeInvalidArgument {
		t.Fatalf("unknown source accepted: %v", err)
	}
	selection.Source = SelectionSourceMessageReference
	if _, err = s.Prepare(context.Background(), PrepareRequest{Selections: []Selection{selection}}); CodeOf(err) != CodeInvalidArgument {
		t.Fatalf("reference without proof accepted: %v", err)
	}
}

func TestGitMetadataAliasesAreNeverReadable(t *testing.T) {
	for _, path := range []string{".git/config", ".GIT/config", "nested/.GiT/HEAD"} {
		if _, ok := safeRelativePath(path); ok {
			t.Fatalf("Git metadata alias accepted: %q", path)
		}
	}
}
