package appserver

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/wu8685/Ariel/internal/filecontext"
)

// FileContextSource shares the existing read-only endpoint. Native wire shapes
// remain in this adapter; filecontext never sees a native RPC method name.
type FileContextSource struct{ RPC RPC }

func (s FileContextSource) Thread(ctx context.Context, id string) (filecontext.Thread, error) {
	if s.RPC == nil {
		return filecontext.Thread{}, fileSourceFailure(ErrMethodUnavailable)
	}
	thread, err := (HistoryReader{RPC: s.RPC}).Read(ctx, id)
	if err != nil {
		return filecontext.Thread{}, fileSourceFailure(err)
	}
	if thread.ID != id || thread.CWD == "" {
		return filecontext.Thread{}, fileSourceFailure(ErrProtocol)
	}
	return filecontext.Thread{ID: id, CWD: thread.CWD}, nil
}

func (s FileContextSource) Items(ctx context.Context, threadID, turnID string) ([]filecontext.Item, error) {
	if s.RPC == nil || threadID == "" || turnID == "" {
		return nil, fileSourceFailure(ErrProtocol)
	}
	cursor := ""
	seen := map[string]bool{}
	bytes := 0
	all := []filecontext.Item{}
	for page := 0; page < 32; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var out struct {
			Data       []json.RawMessage `json:"data"`
			NextCursor *string           `json:"nextCursor"`
		}
		p := map[string]any{"threadId": threadID, "turnId": turnID, "limit": 100, "sortDirection": "asc"}
		if cursor != "" {
			p["cursor"] = cursor
		}
		if err := s.RPC.Call(ctx, "thread/items/list", p, &out); err != nil {
			return nil, fileSourceFailure(err)
		}
		if len(out.Data) > 100 {
			return nil, fileSourceFailure(ErrProtocol)
		}
		for _, raw := range out.Data {
			bytes += len(raw)
			if bytes > 8<<20 {
				return nil, &filecontext.Error{Code: filecontext.CodeOverloaded}
			}
			var entry struct {
				TurnID   string          `json:"turnId"`
				ThreadID string          `json:"threadId"`
				Item     json.RawMessage `json:"item"`
			}
			if json.Unmarshal(raw, &entry) != nil {
				return nil, fileSourceFailure(ErrProtocol)
			}
			if entry.TurnID != "" && entry.TurnID != turnID || entry.ThreadID != "" && entry.ThreadID != threadID {
				return nil, fileSourceFailure(ErrProtocol)
			}
			if len(entry.Item) > 0 && string(entry.Item) != "null" {
				raw = entry.Item
			}
			var item struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Text    string `json:"text"`
				Changes []struct {
					Path string          `json:"path"`
					Kind json.RawMessage `json:"kind"`
					Diff string          `json:"diff"`
				} `json:"changes"`
			}
			if json.Unmarshal(raw, &item) != nil || item.ID == "" || item.Type == "" {
				return nil, fileSourceFailure(ErrProtocol)
			}
			if item.Type != "fileChange" && item.Type != "agentMessage" {
				continue
			}
			mapped := filecontext.Item{ID: item.ID, Type: item.Type, Text: item.Text}
			for _, change := range item.Changes {
				var kind struct {
					Type string `json:"type"`
				}
				var label string
				if json.Unmarshal(change.Kind, &kind) == nil {
					label = kind.Type
				} else if json.Unmarshal(change.Kind, &label) != nil {
					return nil, fileSourceFailure(ErrProtocol)
				}
				mapped.Changes = append(mapped.Changes, filecontext.Change{Path: change.Path, Kind: label, Diff: change.Diff})
			}
			all = append(all, mapped)
		}
		if out.NextCursor == nil || *out.NextCursor == "" {
			return all, nil
		}
		cursor = *out.NextCursor
		if seen[cursor] {
			return nil, &filecontext.Error{Code: filecontext.CodeOverloaded}
		}
		seen[cursor] = true
	}
	return nil, &filecontext.Error{Code: filecontext.CodeOverloaded}
}

func fileSourceFailure(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, ErrCapacity), errors.Is(err, ErrResponseTooLarge):
		return &filecontext.Error{Code: filecontext.CodeOverloaded}
	case errors.Is(err, ErrMethodUnavailable), errors.Is(err, ErrProtocol):
		return &filecontext.Error{Code: filecontext.CodeProtocolUnsupported}
	default:
		return err
	}
}
