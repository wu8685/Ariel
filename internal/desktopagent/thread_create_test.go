package desktopagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

type delayedCreatedThreadHistory struct{ reads int }

func (*delayedCreatedThreadHistory) List(context.Context, string, int) (appserver.Page, error) {
	return appserver.Page{}, nil
}

func (h *delayedCreatedThreadHistory) Read(context.Context, string) (appserver.Thread, error) {
	h.reads++
	return appserver.Thread{}, errors.New("NOT_FOUND")
}

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

func TestServiceCreateThreadBootstrapsImmediateSubscriptionBeforeHistoryIsVisible(t *testing.T) {
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-4000-8000-000000000099"
	history := &delayedCreatedThreadHistory{}
	creator := &fakeThreadCreator{thread: appserver.Thread{ID: id, Name: "", Status: json.RawMessage(`{"type":"notLoaded"}`)}}
	live := &staticLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}, state: json.RawMessage(`{"cwd":` + strconv.Quote(cwd) + `,"threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[]}`)}
	var attachedID, attachedCWD string
	service := NewService(history, func(_ context.Context, threadID, gotCWD string) (Live, error) {
		attachedID, attachedCWD = threadID, gotCWD
		return live, nil
	})
	defer service.Close()
	service.SetThreadCreator(creator)

	if _, err = service.CreateThread(context.Background(), cwd); err != nil {
		t.Fatal(err)
	}
	subscriptionID, _, snapshot, activate, err := service.Subscribe(context.Background(), id, func(map[string]any) {})
	if err != nil {
		t.Fatalf("immediate subscribe failed during history visibility window: %v", err)
	}
	activate()
	defer service.Unsubscribe(subscriptionID)
	if attachedID != id || attachedCWD != cwd {
		t.Fatalf("unexpected bootstrap route: id=%q cwd=%q", attachedID, attachedCWD)
	}
	if history.reads != 0 {
		t.Fatalf("bootstrap subscription unexpectedly read persistent history %d times", history.reads)
	}
	if snapshot["threadId"] != id || snapshot["cwd"] != cwd {
		t.Fatalf("wrong bootstrap snapshot: %+v", snapshot)
	}
}

func TestServiceInvalidCreateResultDoesNotSeedBootstrapController(t *testing.T) {
	cwd := t.TempDir()
	creator := &fakeThreadCreator{thread: appserver.Thread{ID: "not-a-thread-id", Status: json.RawMessage(`{"type":"notLoaded"}`)}}
	service := NewService(&delayedCreatedThreadHistory{}, nil)
	service.SetThreadCreator(creator)

	if _, err := service.CreateThread(context.Background(), cwd); !errors.Is(err, appserver.ErrOutcomeUnknown) {
		t.Fatalf("invalid create result: %v", err)
	}
	if len(service.threads) != 0 {
		t.Fatalf("invalid create seeded bootstrap controllers: %+v", service.threads)
	}
}
