package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

type RPC interface {
	Call(context.Context, string, any, any) error
}
type HistoryReader struct{ RPC RPC }
type ThreadSection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Thread struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	CWD       string          `json:"cwd"`
	UpdatedAt int64           `json:"updatedAt"`
	Status    json.RawMessage `json:"status"`
	Turns     []Turn          `json:"turns"`
	IsPinned  *bool           `json:"isPinned,omitempty"`
	Section   *ThreadSection  `json:"section,omitempty"`
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

type SearchResult struct {
	Thread  Thread `json:"thread"`
	Snippet string `json:"snippet"`
}

type SearchPage struct {
	Data       []SearchResult `json:"data"`
	NextCursor *string        `json:"nextCursor"`
}

type TurnPage struct {
	Data       []Turn  `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

type ItemPage struct {
	Data       []json.RawMessage `json:"data"`
	NextCursor *string           `json:"nextCursor"`
}

const (
	TurnItemsFull      = "full"
	TurnItemsNotLoaded = "notLoaded"
	PinnedSectionID    = "01984de2-8f74-7c91-a3b2-5c5e937cf318"
)

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

// ListPinned uses the public persisted pin filter. A response item without a
// positive isPinned marker means an older server ignored the unknown filter;
// fail closed instead of presenting ordinary sessions as pinned.
func (h HistoryReader) ListPinned(ctx context.Context, limit int) (Page, error) {
	var out Page
	if limit < 1 || limit > 100 {
		return out, ErrProtocol
	}
	params := map[string]any{
		"limit":         limit,
		"sortKey":       "updated_at",
		"sortDirection": "desc",
		"isPinned":      true,
		"sourceKinds":   []string{"cli", "vscode", "appServer"},
		"archived":      false,
	}
	err := h.RPC.Call(ctx, "thread/list", params, &out)
	if err == nil {
		modern := true
		for _, thread := range out.Data {
			if thread.IsPinned == nil || !*thread.IsPinned {
				modern = false
				break
			}
		}
		if modern {
			return out, nil
		}
	} else if !errors.Is(err, ErrMethodUnavailable) && !errors.Is(err, ErrInvalidArgument) {
		return out, err
	}

	// Codex 0.160.0 persists pins in the reserved Pinned section rather than
	// exposing isPinned. This remains App Server-owned state and preserves its
	// manual sidebar order.
	out = Page{}
	sectionParams := map[string]any{
		"limit":          limit,
		"sortKey":        "section_position",
		"sectionId":      PinnedSectionID,
		"sourceKinds":    []string{"cli", "vscode", "appServer"},
		"modelProviders": []string{},
		"archived":       false,
		"useStateDbOnly": true,
	}
	if err := h.RPC.Call(ctx, "thread/list", sectionParams, &out); err != nil {
		return out, err
	}
	isPinned := true
	for _, thread := range out.Data {
		if thread.Section == nil || thread.Section.ID != PinnedSectionID {
			return Page{}, ErrInvalidArgument
		}
	}
	for index := range out.Data {
		out.Data[index].IsPinned = &isPinned
	}
	return out, nil
}

func (h HistoryReader) Search(ctx context.Context, query, cursor string, limit int) (SearchPage, error) {
	var out SearchPage
	query = strings.TrimSpace(query)
	if query == "" || utf8.RuneCountInString(query) > 128 || limit < 1 || limit > 100 {
		return out, ErrInvalidArgument
	}
	params := map[string]any{"searchTerm": query, "limit": limit, "sortKey": "updated_at", "sourceKinds": []string{"cli", "vscode", "appServer"}, "archived": false}
	if cursor != "" {
		params["cursor"] = cursor
	}
	err := h.RPC.Call(ctx, "thread/search", params, &out)
	return out, err
}
func (h HistoryReader) Read(ctx context.Context, threadID string) (Thread, error) {
	return h.read(ctx, threadID, false)
}

// ReadFull is reserved for explicit compatibility probes. Production session
// selection uses Read so one oversized history cannot block other sessions.
func (h HistoryReader) ReadFull(ctx context.Context, threadID string) (Thread, error) {
	return h.read(ctx, threadID, true)
}

func (h HistoryReader) read(ctx context.Context, threadID string, includeTurns bool) (Thread, error) {
	var out struct {
		Thread Thread `json:"thread"`
	}
	if threadID == "" {
		return out.Thread, ErrProtocol
	}
	err := h.RPC.Call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": includeTurns}, &out)
	if err == nil && out.Thread.ID != threadID {
		err = ErrProtocol
	}
	return out.Thread, err
}

func (h HistoryReader) Turns(ctx context.Context, threadID, cursor string, limit int, itemsView string) (TurnPage, error) {
	var out TurnPage
	if threadID == "" || limit < 1 || limit > 10 || (itemsView != TurnItemsFull && itemsView != TurnItemsNotLoaded) {
		return out, ErrProtocol
	}
	params := map[string]any{"threadId": threadID, "limit": limit, "sortDirection": "desc", "itemsView": itemsView}
	if cursor != "" {
		params["cursor"] = cursor
	}
	err := h.RPC.Call(ctx, "thread/turns/list", params, &out)
	return out, err
}

func (h HistoryReader) Items(ctx context.Context, threadID, turnID, cursor string, limit int) (ItemPage, error) {
	var out ItemPage
	if threadID == "" || turnID == "" || limit < 1 || limit > 100 {
		return out, ErrProtocol
	}
	params := map[string]any{"threadId": threadID, "turnId": turnID, "limit": limit, "sortDirection": "desc"}
	if cursor != "" {
		params["cursor"] = cursor
	}
	err := h.RPC.Call(ctx, "thread/items/list", params, &out)
	return out, err
}
