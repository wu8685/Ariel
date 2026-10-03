package appserver

import (
	"context"
	"encoding/json"
)

type RPC interface {
	Call(context.Context, string, any, any) error
}
type HistoryReader struct{ RPC RPC }
type Thread struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	CWD       string          `json:"cwd"`
	UpdatedAt int64           `json:"updatedAt"`
	Status    json.RawMessage `json:"status"`
	Turns     []Turn          `json:"turns"`
}
type Turn struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Items  []json.RawMessage `json:"items"`
}
type Page struct {
	Data       []Thread `json:"data"`
	NextCursor *string  `json:"nextCursor"`
}

func (h HistoryReader) List(ctx context.Context, cursor string, limit int) (Page, error) {
	var out Page
	if limit < 1 || limit > 100 {
		return out, ErrProtocol
	}
	params := map[string]any{"limit": limit, "sortKey": "updated_at", "sourceKinds": []string{"cli", "vscode", "appServer"}, "archived": false}
	if cursor != "" {
		params["cursor"] = cursor
	}
	err := h.RPC.Call(ctx, "thread/list", params, &out)
	return out, err
}
func (h HistoryReader) Read(ctx context.Context, threadID string) (Thread, error) {
	var out struct {
		Thread Thread `json:"thread"`
	}
	if threadID == "" {
		return out.Thread, ErrProtocol
	}
	err := h.RPC.Call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": true}, &out)
	if err == nil && out.Thread.ID != threadID {
		err = ErrProtocol
	}
	return out.Thread, err
}
