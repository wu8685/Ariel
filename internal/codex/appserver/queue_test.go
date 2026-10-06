package appserver

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type queueFakeRPC struct {
	methods []string
	params  []any
	call    func(string, any, any) error
}

func (f *queueFakeRPC) Call(_ context.Context, method string, params any, result any) error {
	f.methods = append(f.methods, method)
	f.params = append(f.params, params)
	if f.call != nil {
		return f.call(method, params, result)
	}
	return nil
}

func TestQueueReaderUsesNativeSchemasAndNeverStartsTurn(t *testing.T) {
	f := &queueFakeRPC{call: func(method string, params any, result any) error {
		switch method {
		case "thread/queue/add":
			out := result.(*struct {
				QueuedSubmission QueuedSubmission `json:"queuedSubmission"`
			})
			out.QueuedSubmission = QueuedSubmission{ID: "q1", ClientUserMessageID: "c1", Input: queueInput("hello", []string{"data:image/png;base64,AAAA"})}
		case "thread/queue/update":
			out := result.(*struct {
				QueuedSubmission QueuedSubmission `json:"queuedSubmission"`
			})
			out.QueuedSubmission = QueuedSubmission{ID: "q1", ClientUserMessageID: "c1", Input: queueInput("edited", nil)}
		case "thread/queue/delete":
			result.(*struct {
				Deleted bool `json:"deleted"`
			}).Deleted = true
		}
		return nil
	}}
	q := QueueReader{RPC: f}
	if got, err := q.Add(context.Background(), "t1", "c1", "hello", []string{"data:image/png;base64,AAAA"}); err != nil || got.ID != "q1" {
		t.Fatalf("add: %+v %v", got, err)
	}
	if got, err := q.Update(context.Background(), "t1", "q1", "edited", nil); err != nil || got.ClientUserMessageID != "c1" {
		t.Fatalf("update: %+v %v", got, err)
	}
	if deleted, err := q.Delete(context.Background(), "t1", "q1"); err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	if err := q.Reorder(context.Background(), "t1", []string{"q2", "q1"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"thread/queue/add", "thread/queue/update", "thread/queue/delete", "thread/queue/reorder"}; !reflect.DeepEqual(f.methods, want) {
		t.Fatalf("methods: %v", f.methods)
	}
	add := f.params[0].(map[string]any)
	if add["threadId"] != "t1" || add["clientUserMessageId"] != "c1" || len(add["input"].([]QueueInput)) != 2 {
		t.Fatalf("add params: %#v", add)
	}
	reorder := f.params[3].(map[string]any)
	if !reflect.DeepEqual(reorder["queuedSubmissionIds"], []string{"q2", "q1"}) {
		t.Fatalf("reorder params: %#v", reorder)
	}
}

func TestQueueReaderBoundsPaginationAndRejectsDuplicateIdentity(t *testing.T) {
	f := &queueFakeRPC{call: func(_ string, params any, result any) error {
		cursor, _ := params.(map[string]any)["cursor"].(string)
		page := result.(*queuePage)
		if cursor == "" {
			next := "next"
			page.Data = []QueuedSubmission{{ID: "q1", ClientUserMessageID: "c1"}}
			page.NextCursor = &next
		} else {
			page.Data = []QueuedSubmission{{ID: "q1", ClientUserMessageID: "c2"}}
		}
		return nil
	}}
	if _, err := (QueueReader{RPC: f}).List(context.Background(), "t1"); !errors.Is(err, ErrProtocol) {
		t.Fatalf("duplicate queue id: %v", err)
	}
}
