package appserver

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeCreationEndpoint struct {
	method string
	params map[string]any
	result any
	err    error
	closed int
}

func (f *fakeCreationEndpoint) Call(_ context.Context, method string, params any, result any) error {
	f.method = method
	f.params = params.(map[string]any)
	if f.err != nil {
		return f.err
	}
	value := f.result.(struct {
		Thread Thread `json:"thread"`
	})
	*(result.(*struct {
		Thread Thread `json:"thread"`
	})) = value
	return nil
}
func (f *fakeCreationEndpoint) Close() error { f.closed++; return nil }

func TestThreadCreatorStartsOnePersistentEmptyThreadAndClosesEndpoint(t *testing.T) {
	endpoint := &fakeCreationEndpoint{result: struct {
		Thread Thread `json:"thread"`
	}{Thread: Thread{ID: "00000000-0000-4000-8000-000000000001", CWD: "/fixture"}}}
	creator := ThreadCreator{Open: func(context.Context, string) (CreationEndpoint, error) { return endpoint, nil }}
	thread, err := creator.Create(context.Background(), "/fixture")
	if err != nil || thread.ID == "" || endpoint.closed != 1 || endpoint.method != "thread/start" {
		t.Fatalf("create: thread=%+v closed=%d method=%q err=%v", thread, endpoint.closed, endpoint.method, err)
	}
	want := map[string]any{"cwd": "/fixture", "ephemeral": false, "serviceName": "ariel"}
	if !reflect.DeepEqual(endpoint.params, want) {
		t.Fatalf("params=%#v want=%#v", endpoint.params, want)
	}
}

func TestThreadCreatorDoesNotRetryAndClassifiesUnconfirmedOutcome(t *testing.T) {
	for name, callErr := range map[string]error{"closed": ErrClosed, "timeout": context.DeadlineExceeded} {
		t.Run(name, func(t *testing.T) {
			opened := 0
			endpoint := &fakeCreationEndpoint{err: callErr}
			creator := ThreadCreator{Open: func(context.Context, string) (CreationEndpoint, error) { opened++; return endpoint, nil }}
			if _, err := creator.Create(context.Background(), "/fixture"); !errors.Is(err, ErrOutcomeUnknown) || opened != 1 || endpoint.closed != 1 {
				t.Fatalf("err=%v opened=%d closed=%d", err, opened, endpoint.closed)
			}
		})
	}
}

func TestThreadCreatorPreservesExplicitRejectionAndRejectsMismatchedReceipt(t *testing.T) {
	rejected := &fakeCreationEndpoint{err: ErrInvalidArgument}
	creator := ThreadCreator{Open: func(context.Context, string) (CreationEndpoint, error) { return rejected, nil }}
	if _, err := creator.Create(context.Background(), "/fixture"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("explicit rejection became unknown: %v", err)
	}
	mismatch := &fakeCreationEndpoint{result: struct {
		Thread Thread `json:"thread"`
	}{Thread: Thread{ID: "00000000-0000-4000-8000-000000000001", CWD: "/other"}}}
	creator = ThreadCreator{Open: func(context.Context, string) (CreationEndpoint, error) { return mismatch, nil }}
	if _, err := creator.Create(context.Background(), "/fixture"); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("mismatched accepted receipt: %v", err)
	}
}
