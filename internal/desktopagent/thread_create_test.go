package desktopagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

type fakeThreadCreator struct {
	cwd    string
	thread appserver.Thread
}

func (f *fakeThreadCreator) Create(_ context.Context, cwd string) (appserver.Thread, error) {
	f.cwd = cwd
	f.thread.CWD = cwd
	return f.thread, nil
}

func TestServiceCreateThreadCanonicalizesExistingDirectory(t *testing.T) {
	real := t.TempDir()
	canonical, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "project-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	creator := &fakeThreadCreator{thread: appserver.Thread{ID: "00000000-0000-4000-8000-000000000001", Status: json.RawMessage(`{"type":"notLoaded"}`)}}
	service := NewService(fakeHistory{}, nil)
	service.SetThreadCreator(creator)
	thread, err := service.CreateThread(context.Background(), link)
	if err != nil || creator.cwd != canonical || thread["threadId"] != creator.thread.ID || thread["cwd"] != canonical || thread["runtime"] != "notLoaded" {
		t.Fatalf("thread=%+v creator=%+v err=%v", thread, creator, err)
	}
}

func TestServiceCreateThreadRejectsUnsafeOrMissingTargetsBeforeMutation(t *testing.T) {
	creator := &fakeThreadCreator{thread: appserver.Thread{ID: "00000000-0000-4000-8000-000000000001"}}
	service := NewService(fakeHistory{}, nil)
	service.SetThreadCreator(creator)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{"", "relative", file, filepath.Join(t.TempDir(), "missing"), string([]byte{'/', 'x', 0, 'y'})} {
		if _, err := service.CreateThread(context.Background(), cwd); err == nil || creator.cwd != "" {
			t.Fatalf("unsafe cwd %q accepted or creator called: %v %q", cwd, err, creator.cwd)
		}
	}
}
