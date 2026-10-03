package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestSessionRoutesRepliesAndRejectsUnsolicitedRequests(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewSession(a, 4096)
	defer c.Close()
	result := make(chan error, 1)
	go func() {
		var got struct {
			OK bool `json:"ok"`
		}
		err := c.Call(context.Background(), "thread/list", map[string]any{}, &got)
		if err == nil && !got.OK {
			err = errors.New("wrong result")
		}
		result <- err
	}()
	s := bufio.NewScanner(b)
	if !s.Scan() {
		t.Fatal("no request")
	}
	var req map[string]json.RawMessage
	json.Unmarshal(s.Bytes(), &req)
	if _, err := fmt.Fprintln(b, `{"id":99,"method":"item/commandExecution/requestApproval","params":{"command":"secret"}}`); err != nil {
		t.Fatal(err)
	}
	if !s.Scan() {
		t.Fatal("no interactive rejection")
	}
	var rejected map[string]json.RawMessage
	json.Unmarshal(s.Bytes(), &rejected)
	if string(rejected["id"]) != "99" || rejected["error"] == nil || rejected["result"] != nil {
		t.Fatalf("unexpected response: %s", s.Bytes())
	}
	fmt.Fprintf(b, "{\"id\":%s,\"result\":{\"ok\":true}}\n", req["id"])
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestSessionCancellationAndFrameLimit(t *testing.T) {
	t.Run("before send", func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		c := NewSession(a, 64)
		defer c.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.Call(ctx, "thread/list", nil, nil); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("oversize response", func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		c := NewSession(a, 64)
		defer c.Close()
		go fmt.Fprintln(b, `{"notification":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
		select {
		case <-c.Done():
			if c.Err() == nil {
				t.Fatal("missing error")
			}
		case <-time.After(time.Second):
			t.Fatal("oversize frame hangs")
		}
	})
}

func TestHistoryReaderUsesOnlyReadMethods(t *testing.T) {
	f := &fakeRPC{}
	h := HistoryReader{RPC: f}
	if _, err := h.List(context.Background(), "cursor", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Read(context.Background(), "fixture-thread"); err != nil {
		t.Fatal(err)
	}
	if len(f.methods) != 2 || f.methods[0] != "thread/list" || f.methods[1] != "thread/read" {
		t.Fatalf("methods: %v", f.methods)
	}
}

type fakeRPC struct{ methods []string }

func (f *fakeRPC) Call(ctx context.Context, method string, params any, result any) error {
	f.methods = append(f.methods, method)
	var body string
	switch method {
	case "thread/list":
		body = `{"data":[],"nextCursor":null}`
	case "thread/read":
		body = `{"thread":{"id":"fixture-thread","turns":[],"status":{"type":"notLoaded"}}}`
	default:
		return errors.New("unexpected mutation")
	}
	return json.Unmarshal([]byte(body), result)
}
