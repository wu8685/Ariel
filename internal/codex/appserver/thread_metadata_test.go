package appserver

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type metadataCall struct {
	method string
	params map[string]any
}

type metadataRPC struct {
	calls       []metadataCall
	thread      Thread
	pinned      bool
	mutationErr error
	readErr     error
	listMissing bool
}

func (f *metadataRPC) Call(_ context.Context, method string, params any, result any) error {
	p := params.(map[string]any)
	f.calls = append(f.calls, metadataCall{method: method, params: p})
	switch method {
	case "thread/name/set":
		if f.mutationErr != nil {
			return f.mutationErr
		}
		f.thread.Name = p["name"].(string)
	case "thread/section/move":
		if f.mutationErr != nil {
			return f.mutationErr
		}
		section, _ := p["sectionId"].(string)
		f.pinned = section == PinnedSectionID
		if f.pinned && !f.listMissing {
			f.thread.Section = &ThreadSection{ID: PinnedSectionID, Name: "Pinned"}
		} else {
			f.thread.Section = nil
		}
	case "thread/read":
		if f.readErr != nil {
			return f.readErr
		}
		result.(*struct {
			Thread Thread `json:"thread"`
		}).Thread = f.thread
	case "thread/list":
		page := result.(*Page)
		if f.pinned {
			marked := true
			thread := f.thread
			thread.IsPinned = &marked
			page.Data = []Thread{thread}
		}
	}
	return nil
}

func TestThreadMetadataWriterRenamesOnceAndVerifiesStoredTitle(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	rpc := &metadataRPC{thread: Thread{ID: id, Name: "Old", CWD: "/fixture"}}
	stored, err := (ThreadMetadataWriter{RPC: rpc}).Rename(context.Background(), id, "新名称")
	if err != nil || stored.Name != "新名称" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	if got, want := methods(rpc.calls), []string{"thread/name/set", "thread/read"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls=%v want=%v", got, want)
	}
	if rpc.calls[0].params["threadId"] != id || rpc.calls[0].params["name"] != "新名称" || len(rpc.calls[0].params) != 2 {
		t.Fatalf("rename params=%#v", rpc.calls[0].params)
	}
}

func TestThreadMetadataWriterPinsAndUnpinsThroughNativeSection(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	rpc := &metadataRPC{thread: Thread{ID: id, Name: "Fixture", CWD: "/fixture"}}
	writer := ThreadMetadataWriter{RPC: rpc}
	if _, err := writer.Pin(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}
	if got, want := methods(rpc.calls), []string{"thread/section/move", "thread/read", "thread/list"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pin calls=%v want=%v", got, want)
	}
	if rpc.calls[0].params["sectionId"] != PinnedSectionID || rpc.calls[0].params["threadId"] != id || len(rpc.calls[0].params) != 2 {
		t.Fatalf("pin params=%#v", rpc.calls[0].params)
	}

	rpc.calls = nil
	if _, err := writer.Pin(context.Background(), id, false); err != nil {
		t.Fatal(err)
	}
	if got, want := methods(rpc.calls), []string{"thread/section/move", "thread/read", "thread/list"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unpin calls=%v want=%v", got, want)
	}
	if value, exists := rpc.calls[0].params["sectionId"]; !exists || value != nil {
		t.Fatalf("unpin section=%#v exists=%v", value, exists)
	}
}

func TestThreadMetadataWriterClassifiesUnverifiedReceiptAsUnknown(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	rpc := &metadataRPC{thread: Thread{ID: id, Name: "Old", CWD: "/fixture"}, readErr: errors.New("lost")}
	writer := ThreadMetadataWriter{RPC: rpc}
	if _, err := writer.Rename(context.Background(), id, "New"); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("rename err=%v", err)
	}
	rpc = &metadataRPC{thread: Thread{ID: id, Name: "Fixture", CWD: "/fixture"}, listMissing: true}
	if _, err := (ThreadMetadataWriter{RPC: rpc}).Pin(context.Background(), id, true); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("pin err=%v", err)
	}
}

func methods(calls []metadataCall) []string {
	out := make([]string, len(calls))
	for i, call := range calls {
		out[i] = call.method
	}
	return out
}
