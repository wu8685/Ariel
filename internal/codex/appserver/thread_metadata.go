package appserver

import "context"

// ThreadMetadataWriter updates Codex-owned thread metadata and verifies every
// acknowledged mutation against the persisted App Server state. Writes are
// never retried because an interrupted response does not prove the write was
// rejected.
type ThreadMetadataWriter struct{ RPC RPC }

func (w ThreadMetadataWriter) Rename(ctx context.Context, threadID, name string) (Thread, error) {
	if w.RPC == nil || threadID == "" || name == "" {
		return Thread{}, ErrInvalidArgument
	}
	var out struct{}
	if err := w.RPC.Call(ctx, "thread/name/set", map[string]any{"threadId": threadID, "name": name}, &out); err != nil {
		return Thread{}, err
	}
	stored, err := (HistoryReader{RPC: w.RPC}).Read(ctx, threadID)
	if err != nil || stored.Name != name {
		return Thread{}, ErrOutcomeUnknown
	}
	return stored, nil
}

func (w ThreadMetadataWriter) Pin(ctx context.Context, threadID string, pinned bool) (Thread, error) {
	if w.RPC == nil || threadID == "" {
		return Thread{}, ErrInvalidArgument
	}
	var sectionID any
	if pinned {
		sectionID = PinnedSectionID
	}
	var out struct{}
	if err := w.RPC.Call(ctx, "thread/section/move", map[string]any{"threadId": threadID, "sectionId": sectionID}, &out); err != nil {
		return Thread{}, err
	}
	stored, err := (HistoryReader{RPC: w.RPC}).Read(ctx, threadID)
	if err != nil {
		return Thread{}, ErrOutcomeUnknown
	}
	storedPinned := (stored.IsPinned != nil && *stored.IsPinned) || (stored.Section != nil && stored.Section.ID == PinnedSectionID)
	if storedPinned != pinned {
		return Thread{}, ErrOutcomeUnknown
	}
	pinnedPage, err := (HistoryReader{RPC: w.RPC}).ListPinned(ctx, 100)
	if err != nil || pinnedPage.NextCursor != nil || containsThread(pinnedPage.Data, threadID) != pinned {
		return Thread{}, ErrOutcomeUnknown
	}
	return stored, nil
}
