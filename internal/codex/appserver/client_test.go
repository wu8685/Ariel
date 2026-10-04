package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
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

func TestDefaultSessionAcceptsResponseAboveOldEightMiBLimit(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewSession(a, 0)
	defer c.Close()
	done := make(chan error, 1)
	go func() {
		s := bufio.NewScanner(b)
		if !s.Scan() {
			done <- errors.New("missing request")
			return
		}
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(s.Bytes(), &req); err != nil {
			done <- err
			return
		}
		_, err := fmt.Fprintf(b, "{\"id\":%s,\"result\":{\"text\":%q}}\n", req.ID, strings.Repeat("a", 9<<20))
		done <- err
	}()
	var got struct {
		Text string `json:"text"`
	}
	err := c.Call(context.Background(), "thread/read", map[string]any{}, &got)
	if err != nil || len(got.Text) != 9<<20 {
		t.Fatalf("9 MiB response: len=%d err=%v", len(got.Text), err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestBoundedJSONLLineUsesInclusiveSixtyFourMiBCap(t *testing.T) {
	if DefaultMaxResponseBytes != 64<<20 {
		t.Fatalf("unexpected local response cap: %d", DefaultMaxResponseBytes)
	}
	for _, size := range []int{127, 128, 129} {
		reader := bufio.NewReader(strings.NewReader(strings.Repeat("x", size) + "\n" + "{}\n"))
		body, _, tooLarge, err := readBoundedLine(reader, 128)
		if err != nil || tooLarge != (size > 128) || (size <= 128 && len(body) != size) {
			t.Fatalf("size=%d bytes=%d tooLarge=%v err=%v", size, len(body), tooLarge, err)
		}
		follow, _, overflow, err := readBoundedLine(reader, 128)
		if err != nil || overflow || string(follow) != "{}" {
			t.Fatalf("following frame lost after size=%d: body=%q err=%v", size, follow, err)
		}
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

func TestOversizedCallDoesNotPoisonReadOnlySession(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewSession(a, 256)
	defer c.Close()
	serverDone := make(chan error, 1)
	go func() {
		s := bufio.NewScanner(b)
		for i := 0; i < 2; i++ {
			if !s.Scan() {
				serverDone <- errors.New("missing request")
				return
			}
			var req struct {
				ID json.RawMessage `json:"id"`
			}
			if err := json.Unmarshal(s.Bytes(), &req); err != nil {
				serverDone <- err
				return
			}
			body := `{"ok":true}`
			if i == 0 {
				body = fmt.Sprintf(`{"text":%q}`, strings.Repeat("x", 400))
			}
			if _, err := fmt.Fprintf(b, "{\"id\":%s,\"result\":%s}\n", req.ID, body); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()
	if err := c.Call(context.Background(), "thread/read", map[string]any{}, nil); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("oversize call: %v", err)
	}
	var got struct {
		OK bool `json:"ok"`
	}
	if err := c.Call(context.Background(), "thread/list", map[string]any{}, &got); err != nil || !got.OK {
		t.Fatalf("later call should work: ok=%v err=%v", got.OK, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestInitializeOptsIntoExperimentalReadPagination(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewSession(a, 4096)
	defer c.Close()
	done := make(chan error, 1)
	go func() { done <- c.Initialize(context.Background()) }()
	s := bufio.NewScanner(b)
	if !s.Scan() {
		t.Fatal("missing initialize request")
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Capabilities struct {
				ExperimentalAPI bool `json:"experimentalApi"`
			} `json:"capabilities"`
		} `json:"params"`
	}
	if err := json.Unmarshal(s.Bytes(), &req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "initialize" || !req.Params.Capabilities.ExperimentalAPI {
		t.Fatal("pagination must be explicitly enabled")
	}
	if _, err := fmt.Fprintf(b, "{\"id\":%s,\"result\":{}}\n", req.ID); err != nil {
		t.Fatal(err)
	}
	if !s.Scan() || !strings.Contains(s.Text(), `"method":"initialized"`) {
		t.Fatalf("missing initialized notification: %q", s.Text())
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
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
	readParams, ok := f.params[1].(map[string]any)
	if !ok || readParams["threadId"] != "fixture-thread" || readParams["includeTurns"] != false {
		t.Fatalf("thread/read must fetch identity without full history: %#v", f.params[1])
	}
}

func TestHistoryReaderFullReadIsExplicit(t *testing.T) {
	f := &fakeRPC{}
	h := HistoryReader{RPC: f}
	if _, err := h.ReadFull(context.Background(), "fixture-thread"); err != nil {
		t.Fatal(err)
	}
	params, ok := f.params[0].(map[string]any)
	if !ok || params["includeTurns"] != true {
		t.Fatalf("explicit full read required: %#v", f.params[0])
	}
}

func TestHistoryReaderUsesBoundedFullTurnAndItemPages(t *testing.T) {
	f := &fakeRPC{}
	h := HistoryReader{RPC: f}
	turns, err := h.Turns(context.Background(), "fixture-thread", "turn-cursor", 10, TurnItemsFull)
	if err != nil || len(turns.Data) != 1 || turns.Data[0].ID != "fixture-turn" || turns.NextCursor == nil || *turns.NextCursor != "next-turn" {
		t.Fatalf("turn page: %+v %v", turns, err)
	}
	params, ok := f.params[0].(map[string]any)
	if f.methods[0] != "thread/turns/list" || !ok || params["threadId"] != "fixture-thread" || params["cursor"] != "turn-cursor" || params["limit"] != 10 || params["sortDirection"] != "desc" || params["itemsView"] != "full" {
		t.Fatalf("turn request: %s %#v", f.methods[0], f.params[0])
	}
	items, err := h.Items(context.Background(), "fixture-thread", "fixture-turn", "item-cursor", 100)
	if err != nil || len(items.Data) != 1 || items.NextCursor == nil || *items.NextCursor != "next-item" {
		t.Fatalf("item page: %+v %v", items, err)
	}
	params, ok = f.params[1].(map[string]any)
	if f.methods[1] != "thread/items/list" || !ok || params["threadId"] != "fixture-thread" || params["turnId"] != "fixture-turn" || params["cursor"] != "item-cursor" || params["limit"] != 100 || params["sortDirection"] != "desc" {
		t.Fatalf("item request: %s %#v", f.methods[1], f.params[1])
	}
	_, err = h.Turns(context.Background(), "fixture-thread", "", 1, TurnItemsNotLoaded)
	if err != nil || f.params[2].(map[string]any)["itemsView"] != "notLoaded" {
		t.Fatalf("metadata turn page: %v %#v", err, f.params[2])
	}
}

func TestUnsupportedPaginationReturnsSpecificError(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	s := NewSession(a, 4096)
	defer s.Close()
	done := make(chan error, 1)
	go func() {
		reader := bufio.NewScanner(b)
		if !reader.Scan() {
			done <- errors.New("missing request")
			return
		}
		var request struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(reader.Bytes(), &request); err != nil {
			done <- err
			return
		}
		_, err := fmt.Fprintf(b, `{"id":%s,"error":{"code":-32601,"message":"Method not found"}}`+"\n", request.ID)
		done <- err
	}()
	_, err := (HistoryReader{RPC: s}).Turns(context.Background(), "thread", "", 1, TurnItemsFull)
	if !errors.Is(err, ErrMethodUnavailable) {
		t.Fatalf("unsupported pagination: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHistoryReaderRejectsInvalidPageRequestsBeforeRPC(t *testing.T) {
	f := &fakeRPC{}
	h := HistoryReader{RPC: f}
	if _, err := h.Turns(context.Background(), "", "", 1, TurnItemsFull); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	if _, err := h.Turns(context.Background(), "fixture-thread", "", 11, TurnItemsFull); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	if _, err := h.Turns(context.Background(), "fixture-thread", "", 1, "summary"); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	if _, err := h.Items(context.Background(), "fixture-thread", "", "", 1); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	if _, err := h.Items(context.Background(), "fixture-thread", "fixture-turn", "", 101); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	if len(f.methods) != 0 {
		t.Fatalf("invalid requests reached App Server: %v", f.methods)
	}
}

type fakeRPC struct {
	methods []string
	params  []any
}

func (f *fakeRPC) Call(ctx context.Context, method string, params any, result any) error {
	f.methods = append(f.methods, method)
	f.params = append(f.params, params)
	var body string
	switch method {
	case "thread/list":
		body = `{"data":[],"nextCursor":null}`
	case "thread/read":
		body = `{"thread":{"id":"fixture-thread","turns":[],"status":{"type":"notLoaded"}}}`
	case "thread/turns/list":
		body = `{"data":[{"id":"fixture-turn","status":"completed","items":[]}],"nextCursor":"next-turn"}`
	case "thread/items/list":
		body = `{"data":[{"id":"fixture-item","type":"agentMessage","text":"ok"}],"nextCursor":"next-item"}`
	default:
		return errors.New("unexpected mutation")
	}
	return json.Unmarshal([]byte(body), result)
}
