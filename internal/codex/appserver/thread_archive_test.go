package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type archiveCall struct {
	method string
	params map[string]any
}

type archiveRPC struct {
	calls       []archiveCall
	active      []Thread
	archived    []Thread
	mutationErr error
}

func (f *archiveRPC) Call(_ context.Context, method string, params any, result any) error {
	f.calls = append(f.calls, archiveCall{method: method, params: params.(map[string]any)})
	if method == "thread/archive" || method == "thread/unarchive" {
		if f.mutationErr != nil {
			return f.mutationErr
		}
		if method == "thread/unarchive" {
			body, _ := json.Marshal(map[string]any{"thread": f.active[0]})
			return json.Unmarshal(body, result)
		}
		return nil
	}
	if method != "thread/list" {
		return errors.New("unexpected method")
	}
	data := f.active
	if f.calls[len(f.calls)-1].params["archived"] == true {
		data = f.archived
	}
	body, _ := json.Marshal(Page{Data: data})
	return json.Unmarshal(body, result)
}

func TestThreadArchiverUsesPublicMethodsAndVerifiesBothIndexes(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	f := &archiveRPC{archived: []Thread{{ID: id}}, active: []Thread{{ID: "other"}}}
	a := ThreadArchiver{RPC: f}
	if err := a.Archive(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	want := []string{"thread/archive", "thread/list", "thread/list"}
	got := make([]string, len(f.calls))
	for i := range f.calls {
		got[i] = f.calls[i].method
	}
	if !reflect.DeepEqual(got, want) || f.calls[0].params["threadId"] != id || f.calls[1].params["archived"] != false || f.calls[2].params["archived"] != true {
		t.Fatalf("unexpected archive calls: %+v", f.calls)
	}
	for _, call := range f.calls {
		if call.method == "thread/delete" {
			t.Fatal("archive path called destructive thread/delete")
		}
	}

	f.calls = nil
	f.active = []Thread{{ID: id}}
	f.archived = []Thread{{ID: "other"}}
	if err := a.Unarchive(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	want = []string{"thread/unarchive", "thread/list", "thread/list"}
	got = got[:0]
	for i := range f.calls {
		got = append(got, f.calls[i].method)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected unarchive calls: %+v", f.calls)
	}
}

func TestThreadArchiverReturnsUnknownForUnverifiedMutationAndNeverRetries(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	f := &archiveRPC{active: []Thread{{ID: id}}, archived: nil}
	a := ThreadArchiver{RPC: f}
	if err := a.Archive(context.Background(), id); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("unverified archive = %v", err)
	}
	if len(f.calls) != 3 {
		t.Fatalf("archive was retried: %+v", f.calls)
	}

	f = &archiveRPC{mutationErr: ErrRemote}
	a = ThreadArchiver{RPC: f}
	if err := a.Archive(context.Background(), id); !errors.Is(err, ErrRemote) || len(f.calls) != 1 {
		t.Fatalf("explicit rejection was not preserved: err=%v calls=%+v", err, f.calls)
	}
}
