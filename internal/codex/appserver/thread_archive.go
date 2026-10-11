package appserver

import "context"

// ThreadArchiver changes only Codex-owned archive state and verifies the
// result against both active and archived indexes. It never retries a write.
type ThreadArchiver struct{ RPC RPC }

func (a ThreadArchiver) Archive(ctx context.Context, threadID string) error {
	if a.RPC == nil || threadID == "" {
		return ErrInvalidArgument
	}
	var out struct{}
	if err := a.RPC.Call(ctx, "thread/archive", map[string]any{"threadId": threadID}, &out); err != nil {
		return err
	}
	return a.verify(ctx, threadID, true)
}

func (a ThreadArchiver) Unarchive(ctx context.Context, threadID string) error {
	if a.RPC == nil || threadID == "" {
		return ErrInvalidArgument
	}
	var out struct {
		Thread Thread `json:"thread"`
	}
	if err := a.RPC.Call(ctx, "thread/unarchive", map[string]any{"threadId": threadID}, &out); err != nil {
		return err
	}
	if out.Thread.ID != threadID {
		return ErrOutcomeUnknown
	}
	return a.verify(ctx, threadID, false)
}

func (a ThreadArchiver) verify(ctx context.Context, threadID string, archived bool) error {
	active, err := a.list(ctx, false)
	if err != nil {
		return ErrOutcomeUnknown
	}
	stored, err := a.list(ctx, true)
	if err != nil {
		return ErrOutcomeUnknown
	}
	inActive := containsThread(active.Data, threadID)
	inArchived := containsThread(stored.Data, threadID)
	if archived == (!inActive && inArchived) {
		return nil
	}
	return ErrOutcomeUnknown
}

func (a ThreadArchiver) list(ctx context.Context, archived bool) (Page, error) {
	var out Page
	err := a.RPC.Call(ctx, "thread/list", map[string]any{
		"limit":       100,
		"sortKey":     "updated_at",
		"sourceKinds": []string{"cli", "vscode", "appServer"},
		"archived":    archived,
	}, &out)
	return out, err
}

func containsThread(threads []Thread, threadID string) bool {
	for _, thread := range threads {
		if thread.ID == threadID {
			return true
		}
	}
	return false
}
