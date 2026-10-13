package desktopagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

type fakeThreadMetadataWriter struct {
	renamed []string
	pinned  []bool
}

type blockingThreadMetadataWriter struct {
	mu      sync.Mutex
	current int
	maximum int
	entered chan string
	release chan struct{}
}

func (f *blockingThreadMetadataWriter) Rename(_ context.Context, threadID, name string) (appserver.Thread, error) {
	f.mu.Lock()
	f.current++
	if f.current > f.maximum {
		f.maximum = f.current
	}
	f.mu.Unlock()
	f.entered <- threadID
	<-f.release
	f.mu.Lock()
	f.current--
	f.mu.Unlock()
	return appserver.Thread{ID: threadID, Name: name, CWD: "/fixture"}, nil
}

func (f *blockingThreadMetadataWriter) Pin(context.Context, string, bool) (appserver.Thread, error) {
	panic("unexpected pin")
}

type metadataHistory struct{ thread appserver.Thread }

func (h metadataHistory) List(context.Context, string, int) (appserver.Page, error) {
	return appserver.Page{Data: []appserver.Thread{h.thread}}, nil
}

func (h metadataHistory) Read(_ context.Context, id string) (appserver.Thread, error) {
	thread := h.thread
	thread.ID = id
	return thread, nil
}

func (f *fakeThreadMetadataWriter) Rename(_ context.Context, threadID, name string) (appserver.Thread, error) {
	f.renamed = append(f.renamed, threadID+"\x00"+name)
	return appserver.Thread{ID: threadID, Name: name, CWD: "/fixture"}, nil
}

func (f *fakeThreadMetadataWriter) Pin(_ context.Context, threadID string, pinned bool) (appserver.Thread, error) {
	f.pinned = append(f.pinned, pinned)
	thread := appserver.Thread{ID: threadID, Name: "Fixture", CWD: "/fixture"}
	if pinned {
		thread.Section = &appserver.ThreadSection{ID: appserver.PinnedSectionID, Name: "Pinned"}
	}
	return thread, nil
}

func TestThreadMetadataMutationsRequireKnownThreadAndReturnVerifiedProjection(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	history := metadataHistory{thread: appserver.Thread{ID: id, Name: "Fixture", CWD: "/fixture"}}
	s := NewService(history, nil)
	writer := &fakeThreadMetadataWriter{}
	s.SetThreadMetadataWriter(writer)
	if _, _, err := s.List(context.Background(), 20, ""); err != nil {
		t.Fatal(err)
	}
	renamed, err := s.RenameThread(context.Background(), id, "  新名称  ")
	if err != nil || renamed["title"] != "新名称" || len(writer.renamed) != 1 {
		t.Fatalf("renamed=%+v calls=%v err=%v", renamed, writer.renamed, err)
	}
	pinned, err := s.PinThread(context.Background(), id, true)
	if err != nil || pinned["isPinned"] != true || len(writer.pinned) != 1 || !writer.pinned[0] {
		t.Fatalf("pinned=%+v calls=%v err=%v", pinned, writer.pinned, err)
	}
	if _, err := s.PinThread(context.Background(), "00000000-0000-4000-8000-000000000002", true); !errors.Is(err, ErrThreadMetadataTargetUnknown) || len(writer.pinned) != 1 {
		t.Fatalf("unknown target err=%v calls=%v", err, writer.pinned)
	}
}

func TestRenameThreadRejectsInvalidUnicodeAndControlCharactersBeforeMutation(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	s := NewService(metadataHistory{thread: appserver.Thread{ID: id, Name: "Fixture", CWD: "/fixture"}}, nil)
	writer := &fakeThreadMetadataWriter{}
	s.SetThreadMetadataWriter(writer)
	if _, _, err := s.List(context.Background(), 20, ""); err != nil {
		t.Fatal(err)
	}
	invalid := []string{"", "   ", "bad\nname", strings.Repeat("界", 201)}
	for _, name := range invalid {
		if _, err := s.RenameThread(context.Background(), id, name); err == nil {
			t.Fatalf("invalid title accepted: %q", name)
		}
	}
	if len(writer.renamed) != 0 {
		t.Fatalf("invalid titles reached writer: %v", writer.renamed)
	}
}

func TestThreadMetadataMutationsSerializePerThreadAndAllowTwoThreadsGlobally(t *testing.T) {
	ids := []string{
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003",
	}
	s := NewService(metadataHistory{thread: appserver.Thread{ID: ids[0], Name: "Fixture", CWD: "/fixture"}}, nil)
	writer := &blockingThreadMetadataWriter{entered: make(chan string, 4), release: make(chan struct{}, 4)}
	s.SetThreadMetadataWriter(writer)
	for _, id := range ids {
		s.rememberThread(id)
	}
	done := make(chan error, 3)
	for index, id := range ids {
		go func(threadID, name string) {
			_, err := s.RenameThread(context.Background(), threadID, name)
			done <- err
		}(id, string(rune('A'+index)))
	}
	for range 2 {
		select {
		case <-writer.entered:
		case <-time.After(time.Second):
			t.Fatal("two independent metadata mutations did not enter")
		}
	}
	select {
	case id := <-writer.entered:
		t.Fatalf("third metadata mutation exceeded the global limit: %s", id)
	case <-time.After(30 * time.Millisecond):
	}
	writer.release <- struct{}{}
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("third metadata mutation did not enter after a slot was released")
	}
	writer.release <- struct{}{}
	writer.release <- struct{}{}
	for range 3 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	writer.mu.Lock()
	maximum := writer.maximum
	writer.mu.Unlock()
	if maximum != 2 {
		t.Fatalf("maximum concurrency=%d want=2", maximum)
	}

	sameWriter := &blockingThreadMetadataWriter{entered: make(chan string, 2), release: make(chan struct{}, 2)}
	s.SetThreadMetadataWriter(sameWriter)
	for _, name := range []string{"D", "E"} {
		go func(value string) { _, err := s.RenameThread(context.Background(), ids[0], value); done <- err }(name)
	}
	<-sameWriter.entered
	select {
	case <-sameWriter.entered:
		t.Fatal("same-thread metadata mutations overlapped")
	case <-time.After(30 * time.Millisecond):
	}
	sameWriter.release <- struct{}{}
	<-sameWriter.entered
	sameWriter.release <- struct{}{}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
