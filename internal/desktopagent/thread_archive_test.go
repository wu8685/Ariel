package desktopagent

import (
	"context"
	"errors"
	"testing"
)

type fakeThreadArchiver struct {
	archived   []string
	unarchived []string
	err        error
}

func (f *fakeThreadArchiver) Archive(_ context.Context, threadID string) error {
	f.archived = append(f.archived, threadID)
	return f.err
}

func (f *fakeThreadArchiver) Unarchive(_ context.Context, threadID string) error {
	f.unarchived = append(f.unarchived, threadID)
	return f.err
}

func archiveService(t *testing.T, busy bool) (*Service, *fakeThreadArchiver, string) {
	t.Helper()
	id := "00000000-0000-4000-8000-000000000001"
	live := &fakeLive{busy: busy, updates: make(chan struct{}, 1)}
	s := NewService(fakeHistory{}, nil)
	s.threads[id] = &threadController{id: id, title: "fixture", cwd: "/fixture", live: live, subs: map[string]*subscription{}}
	a := &fakeThreadArchiver{}
	s.SetThreadArchiver(a)
	return s, a, id
}

func TestArchiveThreadRequiresVerifiedIdleLiveOwnerAndCreatesOneShotUndo(t *testing.T) {
	active, activeArchiver, id := archiveService(t, true)
	defer active.Close()
	if _, err := active.ArchiveThread(context.Background(), id); !errors.Is(err, ErrArchiveStateUnknown) || len(activeArchiver.archived) != 0 {
		t.Fatalf("active archive was not fenced: err=%v calls=%v", err, activeArchiver.archived)
	}

	idle, archiver, id := archiveService(t, false)
	defer idle.Close()
	operationID, err := idle.ArchiveThread(context.Background(), id)
	if err != nil || operationID == "" || len(archiver.archived) != 1 {
		t.Fatalf("idle archive failed: operation=%q err=%v calls=%v", operationID, err, archiver.archived)
	}
	if err := idle.UndoArchive(context.Background(), id, operationID); err != nil || len(archiver.unarchived) != 1 {
		t.Fatalf("undo failed: err=%v calls=%v", err, archiver.unarchived)
	}
	if err := idle.UndoArchive(context.Background(), id, operationID); !errors.Is(err, ErrArchiveUndoInvalid) || len(archiver.unarchived) != 1 {
		t.Fatalf("undo was reusable: err=%v calls=%v", err, archiver.unarchived)
	}
}

func TestArchiveUnknownOutcomeDoesNotCreateUndoRecord(t *testing.T) {
	s, archiver, id := archiveService(t, false)
	defer s.Close()
	archiver.err = errors.New("OUTCOME_UNKNOWN")
	if operationID, err := s.ArchiveThread(context.Background(), id); err == nil || operationID != "" {
		t.Fatalf("unknown archive created undo: operation=%q err=%v", operationID, err)
	}
}
