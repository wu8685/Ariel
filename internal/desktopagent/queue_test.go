package desktopagent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

type fakeFollowUpQueue struct {
	mu    sync.Mutex
	items []appserver.QueuedSubmission
	next  int
}

func (q *fakeFollowUpQueue) List(context.Context, string) ([]appserver.QueuedSubmission, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]appserver.QueuedSubmission(nil), q.items...), nil
}

func (q *fakeFollowUpQueue) Add(_ context.Context, _ string, clientID, text string, images []string) (appserver.QueuedSubmission, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.next++
	input := []appserver.QueueInput{{Type: "text", Text: text}}
	if text == "" {
		input = nil
	}
	for _, image := range images {
		input = append(input, appserver.QueueInput{Type: "image", URL: image})
	}
	item := appserver.QueuedSubmission{ID: fmt.Sprintf("q%d", q.next), ClientUserMessageID: clientID, Input: input}
	q.items = append(q.items, item)
	return item, nil
}

func (q *fakeFollowUpQueue) Update(_ context.Context, _ string, queueID, text string, images []string) (appserver.QueuedSubmission, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for index := range q.items {
		if q.items[index].ID == queueID {
			input := []appserver.QueueInput{{Type: "text", Text: text}}
			if text == "" {
				input = nil
			}
			for _, image := range images {
				input = append(input, appserver.QueueInput{Type: "image", URL: image})
			}
			q.items[index].Input = input
			return q.items[index], nil
		}
	}
	return appserver.QueuedSubmission{}, nil
}

func (q *fakeFollowUpQueue) Delete(_ context.Context, _ string, queueID string) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for index := range q.items {
		if q.items[index].ID == queueID {
			q.items = append(q.items[:index], q.items[index+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (q *fakeFollowUpQueue) Reorder(_ context.Context, _ string, ids []string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	byID := map[string]appserver.QueuedSubmission{}
	for _, item := range q.items {
		byID[item.ID] = item
	}
	ordered := make([]appserver.QueuedSubmission, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, byID[id])
	}
	q.items = ordered
	return nil
}

type steerLive struct {
	*staticLive
	turnID, queueID, clientID, text string
}

func (l *steerLive) Steer(_ context.Context, turnID, queueID, clientID, text string, _ []string) error {
	l.turnID, l.queueID, l.clientID, l.text = turnID, queueID, clientID, text
	return nil
}

func TestServiceQueueLifecycleAndSnapshotUseCodexOrder(t *testing.T) {
	queue := &fakeFollowUpQueue{}
	live := &fakeLive{updates: make(chan struct{}, 1)}
	service := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil }, queue)
	defer service.Close()
	items, err := service.QueueAdd(context.Background(), "thread", "client-1", "first", nil)
	if err != nil || len(items) != 1 || items[0]["queueId"] != "q1" {
		t.Fatalf("first add: %+v %v", items, err)
	}
	items, err = service.QueueAdd(context.Background(), "thread", "client-2", "second", nil)
	if err != nil || len(items) != 2 {
		t.Fatalf("second add: %+v %v", items, err)
	}
	items, err = service.QueueUpdate(context.Background(), "thread", "q1", "first edited", nil)
	if err != nil || items[0]["text"] != "first edited" {
		t.Fatalf("update: %+v %v", items, err)
	}
	items, err = service.QueueReorder(context.Background(), "thread", []string{"q2", "q1"})
	if err != nil || !reflect.DeepEqual([]any{items[0]["queueId"], items[1]["queueId"]}, []any{"q2", "q1"}) {
		t.Fatalf("reorder: %+v %v", items, err)
	}
	_, _, snapshot, _, err := service.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil || !reflect.DeepEqual(snapshot["queuedMessages"], items) {
		t.Fatalf("snapshot queue: %+v %v", snapshot["queuedMessages"], err)
	}
	items, err = service.QueueDelete(context.Background(), "thread", "q2")
	if err != nil || len(items) != 1 || items[0]["queueId"] != "q1" {
		t.Fatalf("delete: %+v %v", items, err)
	}
}

func TestServiceQueueReorderRejectsStaleOrDuplicateSet(t *testing.T) {
	queue := &fakeFollowUpQueue{items: []appserver.QueuedSubmission{
		{ID: "q1", ClientUserMessageID: "c1", Input: []appserver.QueueInput{{Type: "text", Text: "one"}}},
		{ID: "q2", ClientUserMessageID: "c2", Input: []appserver.QueueInput{{Type: "text", Text: "two"}}},
	}}
	service := NewService(fakeHistory{}, nil, queue)
	for _, ids := range [][]string{{"q1"}, {"q1", "q1"}, {"q1", "other"}} {
		if _, err := service.QueueReorder(context.Background(), "thread", ids); err == nil || err.Error() != "INVALID_ARGUMENT" {
			t.Fatalf("stale reorder %v: %v", ids, err)
		}
	}
}

func TestServiceQueueSteerConfirmsOwnerBeforeDeletingDurableItem(t *testing.T) {
	queue := &fakeFollowUpQueue{items: []appserver.QueuedSubmission{{ID: "q1", ClientUserMessageID: "client-1", Input: []appserver.QueueInput{{Type: "text", Text: "guide now"}}}}}
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"inProgress"},"requests":[],"turns":[{"turnId":"active","status":"inProgress","items":[]}]}`)
	live := &steerLive{staticLive: &staticLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}, state: state}}
	service := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil }, queue)
	defer service.Close()
	items, err := service.QueueSteer(context.Background(), "thread", "q1", "active")
	if err != nil || len(items) != 0 {
		t.Fatalf("steer: %+v %v", items, err)
	}
	if live.turnID != "active" || live.queueID != "q1" || live.clientID != "client-1" || live.text != "guide now" {
		t.Fatalf("owner steer identity lost: %+v", live)
	}
}

func TestNativeQueueAttachmentIsVisibleButNotEditable(t *testing.T) {
	item, err := normalizeQueuedSubmission(appserver.QueuedSubmission{ID: "q1", ClientUserMessageID: "c1", Input: []appserver.QueueInput{{Type: "localImage", Path: "/tmp/a.png"}}})
	if err != nil || item["editable"] != false || item["text"] == "" {
		t.Fatalf("native queue item: %+v %v", item, err)
	}
}
