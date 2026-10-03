package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type busyCaller struct {
	calls   int
	result  json.RawMessage
	err     error
	request Request
}

func (b *busyCaller) Call(_ context.Context, r Request) (Reply, error) {
	b.calls++
	b.request = r
	return Reply{Result: b.result}, b.err
}

func TestBusySecondProbesNativeOnlyAfterFirstIsActive(t *testing.T) {
	idle := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`)
	active := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"running"},"requests":[],"turns":[{"turnId":"first","status":"inProgress"}]}`)
	c := &busyCaller{err: &CallError{Cause: ErrRemoteRejected, Outcome: "rejected"}}
	out, err := attemptBusySecond(context.Background(), c, "owner", "thread", "/fixture", "first", idle, active)
	if err != nil || out.Outcome != "rejected" || c.calls != 1 || c.request.Method != "thread-follower-start-turn" {
		t.Fatalf("outcome %+v calls %d err %v", out, c.calls, err)
	}
	c = &busyCaller{result: json.RawMessage(`{"result":{"turn":{"id":"second"}}}`)}
	out, err = attemptBusySecond(context.Background(), c, "owner", "thread", "/fixture", "first", idle, active)
	if err != nil || out.Outcome != "accepted" || out.TurnID != "second" || out.SameTurnID || c.calls != 1 {
		t.Fatalf("accepted: %+v %v", out, err)
	}
	c = &busyCaller{result: json.RawMessage(`{"result":{"turn":{"id":"first"}}}`)}
	out, err = attemptBusySecond(context.Background(), c, "owner", "thread", "/fixture", "first", idle, active)
	if err != nil || !out.SameTurnID {
		t.Fatalf("same turn not recognized: %+v %v", out, err)
	}
	if _, err = attemptBusySecond(context.Background(), c, "owner", "thread", "/fixture", "first", idle, idle); err == nil || c.calls != 1 {
		t.Fatal("accepted when first turn was not active")
	}
	if _, err = attemptBusySecond(context.Background(), c, "owner", "thread", "/fixture", "other", idle, active); err == nil || c.calls != 1 {
		t.Fatal("wrong active turn")
	}
	c = &busyCaller{err: &CallError{Cause: context.DeadlineExceeded, Outcome: "unknown"}}
	out, err = attemptBusySecond(context.Background(), c, "owner", "thread", "/fixture", "first", idle, active)
	if out.Outcome != "unknown" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout misclassified: %+v %v", out, err)
	}
}
