package desktopagent

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/probe"
)

// Opt-in, read-only check against an installed Codex binary. The target ID is
// supplied at runtime and never written to a fixture, log, or repository file.
func TestReadOnlyNativeSearchAgainstInstalledCodex(t *testing.T) {
	query := os.Getenv("ARIEL_READONLY_SEARCH_QUERY")
	if query == "" {
		t.Skip("set ARIEL_READONLY_SEARCH_QUERY for a read-only native search check")
	}
	defaults, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	process, err := appserver.Start(ctx, probe.BundledBinary(defaults.AppPath), os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	s := NewService(appserver.HistoryReader{RPC: process.Session}, nil)
	defer s.Close()
	results, _, err := s.Search(ctx, query, 50, "")
	if err != nil || len(results) == 0 {
		t.Fatalf("native search returned no fixture results: count=%d err=%v", len(results), err)
	}
	for _, result := range results {
		if result["threadId"] == "" || result["searchSnippet"] == nil || len(result["turns"].([]any)) != 0 {
			t.Fatal("native search result shape invalid or full history leaked")
		}
	}
	t.Logf("read-only native search verified: results=%d", len(results))
}

func TestReadOnlyPinnedThreadsAgainstInstalledCodex(t *testing.T) {
	if os.Getenv("ARIEL_READONLY_PINNED") != "1" {
		t.Skip("set ARIEL_READONLY_PINNED=1 for a read-only native pinned-thread check")
	}
	defaults, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	process, err := appserver.Start(ctx, probe.BundledBinary(defaults.AppPath), os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	page, err := (appserver.HistoryReader{RPC: process.Session}).ListPinned(ctx, 100)
	if errors.Is(err, appserver.ErrInvalidArgument) || errors.Is(err, appserver.ErrMethodUnavailable) {
		t.Log("installed Codex does not expose public isPinned metadata")
		return
	}
	if err != nil {
		t.Fatalf("native pinned list failed: %v", err)
	}
	for _, thread := range page.Data {
		if thread.ID == "" || thread.IsPinned == nil || !*thread.IsPinned {
			t.Fatal("native pinned result missing identity or pin marker")
		}
	}
	t.Logf("read-only native pinned list verified: results=%d", len(page.Data))
}

func TestReadOnlyPagedHistoryAgainstInstalledCodex(t *testing.T) {
	id := os.Getenv("ARIEL_READONLY_THREAD_ID")
	if id == "" {
		t.Skip("set ARIEL_READONLY_THREAD_ID for a read-only compatibility check")
	}
	defaults, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	process, err := appserver.Start(ctx, probe.BundledBinary(defaults.AppPath), os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	reader := appserver.HistoryReader{RPC: process.Session}
	s := NewService(reader, nil)
	defer s.Close()
	recent, err := s.Read(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent["turns"].([]any)) > recentTurnLimit {
		t.Fatal("recent window exceeds ten turns")
	}
	full, err := reader.ReadFull(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	count, itemCount, partialCount := 0, 0, 0
	expected := make(map[string][]any, len(full.Turns))
	for _, native := range full.Turns {
		normalized, err := normalizeTurn(native.ID, native.Status, native.Items)
		if err != nil {
			t.Fatal(err)
		}
		expected[native.ID] = normalized["items"].([]any)
	}
	seen := map[string]bool{}
	cursor := ""
	for pageNumber := 0; pageNumber < len(full.Turns)+1; pageNumber++ {
		page, err := s.History(ctx, id, cursor, recentTurnLimit)
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range page["turns"].([]any) {
			turn := raw.(map[string]any)
			turnID := turn["turnId"].(string)
			if seen[turnID] {
				t.Fatal("duplicate turn across native pages")
			}
			seen[turnID] = true
			count++
			allItems := turn["items"].([]any)
			if turn["itemsComplete"] == false {
				partialCount++
				itemCursor := turn["nextItemCursor"].(string)
				if itemCursor == "" {
					t.Fatal("partial turn without item cursor")
				}
				for itemPage := 0; itemPage < 1000 && itemCursor != ""; itemPage++ {
					more, err := s.HistoryItems(ctx, id, turnID, itemCursor, 100)
					if err != nil {
						t.Fatal(err)
					}
					allItems = append(more["items"].([]any), allItems...)
					itemCursor = more["nextItemCursor"].(string)
				}
				if itemCursor != "" {
					t.Fatal("item cursor did not exhaust")
				}
			}
			if !reflect.DeepEqual(allItems, expected[turnID]) {
				t.Fatal("paged items differ from original native turn")
			}
			itemCount += len(allItems)
		}
		next := page["nextCursor"].(string)
		if next == "" {
			break
		}
		if next == cursor {
			t.Fatal("turn cursor did not advance")
		}
		cursor = next
	}
	wantItems := 0
	for _, items := range expected {
		wantItems += len(items)
	}
	if count != len(full.Turns) || itemCount != wantItems {
		t.Fatalf("paged structure mismatch: turns=%d/%d items=%d/%d partial=%d", count, len(full.Turns), itemCount, wantItems, partialCount)
	}
	t.Logf("read-only pagination verified: turns=%d items=%d splitTurns=%d", count, itemCount, partialCount)
}

func TestReadOnlyLargeOwnerFollowerAgainstInstalledDesktop(t *testing.T) {
	id := os.Getenv("ARIEL_READONLY_THREAD_ID")
	if id == "" {
		t.Skip("set ARIEL_READONLY_THREAD_ID for a read-only compatibility check")
	}
	defaults, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	process, err := appserver.Start(ctx, probe.BundledBinary(defaults.AppPath), os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	meta, err := (appserver.HistoryReader{RPC: process.Session}).Read(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	live, err := OpenFollower(ctx, defaults.Socket, id, meta.CWD)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	raw, err := live.Current()
	if err != nil {
		t.Fatal(err)
	}
	thread, err := NormalizeLive(id, meta.Name, meta.CWD, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread["turns"].([]any)) > recentTurnLimit {
		t.Fatal("owner window exceeds ten turns")
	}
	if err := checkThreadSize(thread); err != nil {
		t.Fatal(err)
	}
	t.Logf("read-only owner observed: rawBytes=%d recentTurns=%d", len(raw), len(thread["turns"].([]any)))
}
