package appserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	// Path is the rollout file Codex returns on thread/read. The original
	// workspace lives in that file's session_meta when the thread object cwd
	// is only the app-server process directory.
	Path string `json:"path,omitempty"`
	// SessionCWD is the workspace from session_meta. Top-level cwd can be the
	// app-server process directory instead.
	SessionCWD string `json:"-"`
}
type Turn struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	CWD    string            `json:"cwd,omitempty"`
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
	if threadID == "" {
		return Thread{}, ErrProtocol
	}
	var raw json.RawMessage
	if err := h.RPC.Call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": includeTurns}, &raw); err != nil {
		return Thread{}, err
	}
	var out struct {
		Thread Thread `json:"thread"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Thread.ID != threadID {
		return Thread{}, ErrProtocol
	}
	if out.Thread.SessionCWD == "" {
		out.Thread.SessionCWD = SessionMetaCWD(raw)
	}
	if out.Thread.SessionCWD == "" {
		out.Thread.SessionCWD = SessionMetaFromRollout(out.Thread.Path)
	}
	return out.Thread, nil
}

func (t *Thread) UnmarshalJSON(raw []byte) error {
	type alias Thread
	var decoded alias
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*t = Thread(decoded)
	t.SessionCWD = SessionMetaCWD(raw)
	return nil
}

var rolloutCWD = regexp.MustCompile(`"cwd"\s*:\s*"((?:[^"\\]|\\.)*)"`)

// SessionMetaCWD reads the workspace recorded on the Codex session. thread/read
// may put the app-server process directory in the top-level cwd, and may put
// session_meta on the thread or beside it.
func SessionMetaCWD(raw json.RawMessage) string {
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	if cwd := metaCWD(body); cwd != "" {
		return cwd
	}
	thread, _ := body["thread"].(map[string]any)
	return metaCWD(thread)
}

func metaCWD(body map[string]any) string {
	if body == nil {
		return ""
	}
	for _, key := range []string{"sessionMeta", "session_meta", "sessionMetadata", "session_metadata"} {
		if cwd := nestedCWD(body[key]); cwd != "" {
			return cwd
		}
	}
	return ""
}

func nestedCWD(value any) string {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if text == "" || (text[0] != '{' && text[0] != '[') {
			return ""
		}
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) != nil {
			return ""
		}
		return nestedCWD(decoded)
	case []any:
		for _, item := range typed {
			if cwd := nestedCWD(item); cwd != "" {
				return cwd
			}
		}
	case map[string]any:
		if cwd, _ := typed["cwd"].(string); strings.TrimSpace(cwd) != "" {
			return strings.TrimSpace(cwd)
		}
		for _, key := range []string{"payload", "session", "sessionMeta", "session_meta"} {
			if cwd := nestedCWD(typed[key]); cwd != "" {
				return cwd
			}
		}
	}
	return ""
}

// SessionMetaFromRollout reads the workspace from the first session_meta line
// of a rollout file returned as thread.path. The line can be larger than the
// prefix we read; cwd is recorded before the long instruction fields.
func SessionMetaFromRollout(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	reader := bufio.NewReader(io.LimitReader(file, 256*1024))
	for index := 0; index < 8; index++ {
		line, err := reader.ReadBytes('\n')
		if cwd := sessionMetaLineCWD(line); cwd != "" {
			return cwd
		}
		if err != nil {
			return ""
		}
	}
	return ""
}

func sessionMetaLineCWD(line []byte) string {
	if !bytes.Contains(line, []byte("session_meta")) {
		return ""
	}
	var row struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(line, &row) == nil {
		if row.Type != "session_meta" {
			return ""
		}
		if len(row.Payload) > 0 {
			var payload any
			if json.Unmarshal(row.Payload, &payload) == nil {
				if cwd := nestedCWD(payload); cwd != "" {
					return cwd
				}
			}
		}
	}
	match := rolloutCWD.FindSubmatch(line)
	if match == nil {
		return ""
	}
	var cwd string
	if json.Unmarshal([]byte(`"`+string(match[1])+`"`), &cwd) != nil {
		return ""
	}
	return strings.TrimSpace(cwd)
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
	if err := h.RPC.Call(ctx, "thread/items/list", params, &out); err != nil {
		return out, err
	}
	// Codex 0.160 may return either the legacy item itself or a timeline
	// entry carrying the item under `item`. Keep both shapes compatible so
	// giant turns can fall back to item pagination without reading all turns.
	for index, raw := range out.Data {
		var entry struct {
			ID   string          `json:"id"`
			Type string          `json:"type"`
			Item json.RawMessage `json:"item"`
		}
		if json.Unmarshal(raw, &entry) != nil {
			return ItemPage{}, ErrProtocol
		}
		if entry.ID != "" || entry.Type != "" {
			continue
		}
		if len(entry.Item) == 0 || string(entry.Item) == "null" {
			return ItemPage{}, ErrProtocol
		}
		out.Data[index] = entry.Item
	}
	return out, nil
}
