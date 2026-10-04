package desktopagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

type historyStub struct {
	fakeHistory
	turnCalls []int
	turns     func(int, string, string) (appserver.TurnPage, error)
	items     func(string, string, int) (appserver.ItemPage, error)
}

func (h *historyStub) Turns(_ context.Context, id, cursor string, limit int, view string) (appserver.TurnPage, error) {
	h.turnCalls = append(h.turnCalls, limit)
	if id != "thread" || h.turns == nil {
		return appserver.TurnPage{}, fmt.Errorf("unexpected turn request")
	}
	return h.turns(limit, cursor, view)
}

func (h *historyStub) Items(_ context.Context, id, turnID, cursor string, limit int) (appserver.ItemPage, error) {
	if id != "thread" || h.items == nil {
		return appserver.ItemPage{}, fmt.Errorf("unexpected item request")
	}
	return h.items(turnID, cursor, limit)
}

func TestServiceHistoryReturnsFullTurnsWithNativeCursor(t *testing.T) {
	next := "opaque-older"
	h := &historyStub{turns: func(limit int, cursor, view string) (appserver.TurnPage, error) {
		if limit != 2 || cursor != "opaque-now" || view != appserver.TurnItemsFull {
			return appserver.TurnPage{}, fmt.Errorf("wrong page request")
		}
		return appserver.TurnPage{Data: []appserver.Turn{
			{ID: "new", Status: "completed", Items: []json.RawMessage{json.RawMessage(`{"id":"a","type":"agentMessage","text":"latest"}`)}},
			{ID: "old", Status: "completed", Items: []json.RawMessage{json.RawMessage(`{"id":"f","type":"fileChange","changes":[{"path":"safe.txt","kind":{"type":"update"}}]}`)}},
		}, NextCursor: &next}, nil
	}}
	s := NewService(h, nil)
	defer s.Close()
	page, err := s.History(context.Background(), "thread", "opaque-now", 2)
	if err != nil {
		t.Fatal(err)
	}
	turns := page["turns"].([]any)
	if len(turns) != 2 || turns[0].(map[string]any)["turnId"] != "new" || turns[1].(map[string]any)["turnId"] != "old" || page["nextCursor"] != next {
		t.Fatalf("turn order or cursor lost: %+v", page)
	}
	items := turns[1].(map[string]any)["items"].([]any)
	if len(items) != 1 || !strings.Contains(items[0].(map[string]any)["text"].(string), "safe.txt") {
		t.Fatalf("full history omitted file change: %+v", items)
	}
}

func TestServiceHistoryShrinksOversizedTurnPageWithoutSkippingTurns(t *testing.T) {
	bigText := strings.Repeat("x", 800<<10)
	all := make([]appserver.Turn, 10)
	for i := range all {
		item := json.RawMessage(fmt.Sprintf(`{"id":"item-%d","type":"agentMessage","text":%q}`, i, bigText))
		all[i] = appserver.Turn{ID: fmt.Sprintf("turn-%d", 10-i), Status: "completed", Items: []json.RawMessage{item}}
	}
	h := &historyStub{turns: func(limit int, cursor, view string) (appserver.TurnPage, error) {
		if cursor != "" || view != appserver.TurnItemsFull {
			return appserver.TurnPage{}, fmt.Errorf("wrong page request")
		}
		next := fmt.Sprintf("after-%d", limit)
		return appserver.TurnPage{Data: all[:limit], NextCursor: &next}, nil
	}}
	s := NewService(h, nil)
	defer s.Close()
	page, err := s.History(context.Background(), "thread", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	turns := page["turns"].([]any)
	if len(turns) != 5 || turns[0].(map[string]any)["turnId"] != "turn-10" || turns[4].(map[string]any)["turnId"] != "turn-6" || page["nextCursor"] != "after-5" || len(h.turnCalls) != 2 || h.turnCalls[0] != 10 || h.turnCalls[1] != 5 {
		t.Fatalf("page shrink skipped or duplicated turns: calls=%v page=%+v", h.turnCalls, page)
	}
}

func TestServiceHistorySplitsGiantTurnIntoNewestItemPage(t *testing.T) {
	fullItems := make([]json.RawMessage, 10)
	for i := range fullItems {
		fullItems[i] = json.RawMessage(fmt.Sprintf(`{"id":"item-%02d","type":"agentMessage","text":%q}`, i, strings.Repeat("x", 800<<10)))
	}
	views := []string{}
	itemCursor := "older-items"
	turnCursor := "older-turns"
	h := &historyStub{
		turns: func(limit int, cursor, view string) (appserver.TurnPage, error) {
			views = append(views, view)
			if limit != 1 || cursor != "" {
				return appserver.TurnPage{}, fmt.Errorf("wrong turn page")
			}
			items := fullItems
			if view == appserver.TurnItemsNotLoaded {
				items = nil
			}
			return appserver.TurnPage{Data: []appserver.Turn{{ID: "giant", Status: "completed", Items: items}}, NextCursor: &turnCursor}, nil
		},
		items: func(turnID, cursor string, limit int) (appserver.ItemPage, error) {
			if turnID != "giant" || cursor != "" || limit != 100 {
				return appserver.ItemPage{}, fmt.Errorf("wrong item page")
			}
			return appserver.ItemPage{Data: []json.RawMessage{
				json.RawMessage(`{"id":"item-09","type":"agentMessage","text":"latest"}`),
				json.RawMessage(`{"id":"item-08","type":"agentMessage","text":"earlier"}`),
			}, NextCursor: &itemCursor}, nil
		},
	}
	s := NewService(h, nil)
	defer s.Close()
	page, err := s.History(context.Background(), "thread", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0] != appserver.TurnItemsFull || views[1] != appserver.TurnItemsNotLoaded || page["nextCursor"] != turnCursor {
		t.Fatalf("giant turn metadata/cursor: views=%v page=%+v", views, page)
	}
	turn := page["turns"].([]any)[0].(map[string]any)
	items := turn["items"].([]any)
	if turn["itemsComplete"] != false || turn["nextItemCursor"] != itemCursor || len(items) != 2 || items[0].(map[string]any)["itemId"] != "item-08" || items[1].(map[string]any)["itemId"] != "item-09" {
		t.Fatalf("giant turn latest items: %+v", turn)
	}
}

func TestServiceHistoryItemsContinuesGiantTurnInChronologicalOrder(t *testing.T) {
	h := &historyStub{items: func(turnID, cursor string, limit int) (appserver.ItemPage, error) {
		if turnID != "giant" || cursor != "older-items" || limit != 100 {
			return appserver.ItemPage{}, fmt.Errorf("wrong item cursor")
		}
		return appserver.ItemPage{Data: []json.RawMessage{
			json.RawMessage(`{"id":"item-07","type":"agentMessage","text":"seven"}`),
			json.RawMessage(`{"id":"item-06","type":"agentMessage","text":"six"}`),
		}}, nil
	}}
	s := NewService(h, nil)
	defer s.Close()
	page, err := s.HistoryItems(context.Background(), "thread", "giant", "older-items", 100)
	if err != nil {
		t.Fatal(err)
	}
	items := page["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["itemId"] != "item-06" || items[1].(map[string]any)["itemId"] != "item-07" || page["itemsComplete"] != true || page["nextItemCursor"] != "" {
		t.Fatalf("item continuation: %+v", page)
	}
}

func TestServiceHistoryRejectsNonAdvancingOrDuplicateNativePage(t *testing.T) {
	for name, page := range map[string]appserver.TurnPage{
		"empty with cursor": {Data: []appserver.Turn{}, NextCursor: strPointer("same")},
		"repeated cursor":   {Data: []appserver.Turn{{ID: "one", Status: "completed"}}, NextCursor: strPointer("same")},
		"duplicate turn":    {Data: []appserver.Turn{{ID: "one", Status: "completed"}, {ID: "one", Status: "completed"}}},
	} {
		t.Run(name, func(t *testing.T) {
			h := &historyStub{turns: func(int, string, string) (appserver.TurnPage, error) { return page, nil }}
			s := NewService(h, nil)
			defer s.Close()
			if _, err := s.History(context.Background(), "thread", "same", 2); err == nil {
				t.Fatal("malformed native page accepted")
			}
		})
	}
}

func TestServiceHistoryItemsRejectsNonAdvancingNativePage(t *testing.T) {
	h := &historyStub{items: func(string, string, int) (appserver.ItemPage, error) {
		return appserver.ItemPage{Data: []json.RawMessage{}, NextCursor: strPointer("same")}, nil
	}}
	s := NewService(h, nil)
	defer s.Close()
	if _, err := s.HistoryItems(context.Background(), "thread", "giant", "same", 10); err == nil {
		t.Fatal("empty item page with repeated cursor accepted")
	}
}

func TestItemPageLeavesRoomForOwnerMetadata(t *testing.T) {
	calls := []int{}
	h := &historyStub{items: func(turnID, cursor string, limit int) (appserver.ItemPage, error) {
		calls = append(calls, limit)
		items := make([]json.RawMessage, limit)
		for i := range items {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":"item-%d","type":"agentMessage","text":%q}`, i, strings.Repeat("x", 650<<10)))
		}
		next := fmt.Sprintf("after-%d", limit)
		return appserver.ItemPage{Data: items, NextCursor: &next}, nil
	}}
	page, err := boundedItems(context.Background(), h, "thread", "giant", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 5 || len(calls) != 2 || calls[0] != 10 || calls[1] != 5 {
		t.Fatalf("item page failed to reserve metadata space: calls=%v count=%d", calls, len(page.Data))
	}
}

func strPointer(value string) *string { return &value }
