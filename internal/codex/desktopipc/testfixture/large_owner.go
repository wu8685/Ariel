// Package testfixture provides an isolated native peer for transport regression
// tests. It never discovers real sockets or executes Codex mutations.
package testfixture

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

const ThreadID = "00000000-0000-4000-8000-000000000121"
const CWD = "/tmp/native-wire-regression"
const InitialText = "原生大帧首次打开成功"
const StreamingText = "原生大帧后续增量已到达"
const CompletedText = "原生大帧最终回复已完成"

// UnknownText must survive projection exactly. Together with the tool body the old
// HTML-escaping encoder exceeded the production 64 MiB resident limit.
func UnknownText() string { return strings.Repeat("<>&", 1<<19) }

type Stats struct {
	Connections int `json:"connections"`
	Loads       int `json:"loads"`
	Patches     int `json:"patches"`
	Unexpected  int `json:"unexpected"`
	WireBytes   int `json:"wireBytes"`
	Faults      int `json:"faults"`
}

type peer struct {
	conn     net.Conn
	mu       sync.Mutex // serializes replies, snapshots and patches
	revision int
}

type LargeOwner struct {
	Path                  string
	dir                   string
	listener              net.Listener
	mu                    sync.Mutex
	peers                 map[*peer]bool
	stats                 Stats
	wg                    sync.WaitGroup
	accepted              chan struct{}
	historyBytes          int
	text, status, runtime string
	revision              int
}

func NewLargeOwner() (*LargeOwner, error) {
	return NewLargeOwnerWithHistory(0)
}

func NewLargeOwnerWithHistory(historyBytes int) (*LargeOwner, error) {
	// Keep the Unix path short on macOS, including under a long test tempdir.
	dir, err := os.MkdirTemp("", "aw-")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "ipc.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	o := &LargeOwner{Path: path, dir: dir, listener: listener, peers: map[*peer]bool{}, accepted: make(chan struct{}), historyBytes: historyBytes, text: InitialText, status: "completed", runtime: "idle", revision: 1}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		defer close(o.accepted)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			p := &peer{conn: conn}
			o.mu.Lock()
			o.peers[p] = true
			o.stats.Connections++
			o.mu.Unlock()
			o.wg.Add(1)
			go o.serve(p)
		}
	}()
	return o, nil
}

func (o *LargeOwner) Close() {
	o.listener.Close()
	// Wait for Accept to stop before closing peers added by its final iteration.
	<-o.accepted
	o.mu.Lock()
	for p := range o.peers {
		p.conn.Close()
	}
	o.mu.Unlock()
	o.wg.Wait()
	os.RemoveAll(o.dir)
}

func (o *LargeOwner) Stats() Stats { o.mu.Lock(); defer o.mu.Unlock(); return o.stats }

func literalJSON(value any) json.RawMessage {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		panic(err)
	} // fixture literals only
	return b.Bytes()
}

func event(change any) json.RawMessage {
	return literalJSON(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11,
		"sourceClientId": "isolated-owner", "params": map[string]any{"hostId": "local", "conversationId": ThreadID, "change": change}})
}

func snapshot(historyBytes int, text, status, runtime string, revision int) json.RawMessage {
	// RawMessage fixes type-before-result order as emitted by native tool items.
	tool := append([]byte(`{"type":"mcpToolCall","id":"native-tool","server":"fixture","tool":"large","status":"completed","result":{"type":42,"content":"`), strings.Repeat("<>&", (10<<20)/3)...)
	tool = append(tool, []byte(`"}}`)...)
	image := map[string]any{"type": "image", "url": "data:image/png;base64," + strings.Repeat("A", 1<<20)}
	state := map[string]any{"cwd": CWD, "requests": []any{}, "threadRuntimeStatus": map[string]any{"type": runtime},
		"latestThreadSettings": map[string]any{"approvalPolicy": "on-request", "sandboxPolicy": map[string]any{"type": "dangerFullAccess"}},
		"fixtureUnknown":       UnknownText(), "turns": []any{map[string]any{"turnId": "native-turn", "status": status, "items": []any{
			map[string]any{"type": "userMessage", "id": "native-user", "content": []any{map[string]any{"type": "text", "text": "隔离大帧回归"}, image}},
			map[string]any{"type": "agentMessage", "id": "native-reply", "text": text}, json.RawMessage(tool),
		}}}}
	// Retained JSON schema metadata is not a native discriminated union.
	state["fixtureSchema"] = map[string]any{"type": []any{"string", "null"}}
	if historyBytes > 0 {
		state["turns"] = append([]any{
			map[string]any{"turnId": "retired-turn", "status": "completed", "items": []any{map[string]any{"id": "retired-item", "type": "agentMessage", "text": strings.Repeat("historical-body-", historyBytes/16)}}},
			map[string]any{"turnId": "previous-turn", "status": "completed", "items": []any{}},
		}, state["turns"].([]any)...)
	}
	return event(map[string]any{"type": "snapshot", "revision": revision, "conversationState": state})
}

func (o *LargeOwner) serve(p *peer) {
	defer o.wg.Done()
	defer func() { p.conn.Close(); o.mu.Lock(); delete(o.peers, p); o.mu.Unlock() }()
	for {
		body, err := desktopipc.ReadFrame(p.conn, desktopipc.DefaultMaxFrameBytes)
		if err != nil {
			return
		}
		var req struct {
			Method    string `json:"method"`
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(body, &req) != nil {
			return
		}
		if req.Method == "thread-stream-following-changed" {
			continue
		}
		result, kind := any(map[string]any{}), "success"
		switch req.Method {
		case "initialize":
			result = map[string]any{"clientId": "isolated-follower"}
		case "thread-owner-discovery":
		case "thread-follower-load-complete-history":
		default:
			kind = "error"
			o.mu.Lock()
			o.stats.Unexpected++
			o.mu.Unlock()
		}
		p.mu.Lock()
		err = desktopipc.WriteFrame(p.conn, literalJSON(map[string]any{"type": "response", "requestId": req.RequestID,
			"resultType": kind, "handledByClientId": "isolated-owner", "result": result}), desktopipc.DefaultMaxFrameBytes)
		if err == nil && req.Method == "thread-follower-load-complete-history" {
			o.mu.Lock()
			text, status, runtime, revision := o.text, o.status, o.runtime, o.revision
			o.mu.Unlock()
			snapshot := snapshot(o.historyBytes, text, status, runtime, revision)
			o.mu.Lock()
			o.stats.Loads++
			o.stats.WireBytes = len(snapshot)
			o.mu.Unlock()
			err = desktopipc.WriteFrame(p.conn, snapshot, 256<<20)
			p.revision = revision
		}
		p.mu.Unlock()
		if err != nil {
			return
		} // small-frame fallback closes its first socket
	}
}

// Advance pushes actual ordered native patches, not a prebuilt Web snapshot.
func (o *LargeOwner) Advance(complete bool) error {
	text, status, runtime := StreamingText, "inProgress", "inProgress"
	if complete {
		text, status, runtime = CompletedText, "completed", "idle"
	}
	o.mu.Lock()
	o.text, o.status, o.runtime = text, status, runtime
	o.revision++
	peers := make([]*peer, 0, len(o.peers))
	for p := range o.peers {
		peers = append(peers, p)
	}
	o.mu.Unlock()
	sent := 0
	for _, p := range peers {
		p.mu.Lock()
		if p.revision > 0 {
			turnIndex := 0
			if o.historyBytes > 0 {
				turnIndex = 2
			}
			change := map[string]any{"type": "patches", "baseRevision": p.revision, "revision": p.revision + 1,
				"patches": []any{
					map[string]any{"op": "replace", "path": []any{"turns", turnIndex, "items", 1, "text"}, "value": text},
					map[string]any{"op": "replace", "path": []any{"turns", turnIndex, "status"}, "value": status},
					map[string]any{"op": "replace", "path": []any{"threadRuntimeStatus", "type"}, "value": runtime},
				}}
			if err := desktopipc.WriteFrame(p.conn, event(change), desktopipc.DefaultMaxFrameBytes); err == nil {
				p.revision++
				sent++
			}
		}
		p.mu.Unlock()
	}
	if sent == 0 {
		return errors.New("no loaded native follower")
	}
	o.mu.Lock()
	o.stats.Patches += sent
	o.mu.Unlock()
	return nil
}

// An actual native continuity failure. The next cold snapshot is terminal;
// neither the Agent↔Relay transport nor any Web socket is touched.
func (o *LargeOwner) InvalidateWithCompletedState() error {
	o.mu.Lock()
	o.text, o.status, o.runtime = CompletedText, "completed", "idle"
	o.revision++
	peers := []*peer{}
	for p := range o.peers {
		peers = append(peers, p)
	}
	o.stats.Faults++
	o.mu.Unlock()
	sent := false
	for _, p := range peers {
		p.mu.Lock()
		if p.revision > 0 {
			err := desktopipc.WriteFrame(p.conn, event(map[string]any{"type": "patches", "baseRevision": p.revision + 1, "revision": p.revision + 2, "patches": []any{}}), desktopipc.DefaultMaxFrameBytes)
			if err == nil {
				sent = true
			}
		}
		p.mu.Unlock()
	}
	if !sent {
		return errors.New("no loaded native follower")
	}
	return nil
}
