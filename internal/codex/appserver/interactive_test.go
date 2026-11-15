package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestBrainAppServerArgsKeepLocalToolsAndDynamicHost(t *testing.T) {
	args, err := brainAppServerArgs([]byte("shell_tool stable true\nunified_exec stable true\ncode_mode_host stable true\nview_image removed false\nsleep_tool removed false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "--disable") || slices.Contains(args, "shell_tool") || slices.Contains(args, "code_mode_host") || slices.Contains(args, "view_image") {
		t.Fatalf("brain process must keep Codex tools enabled: %v", args)
	}
	if !slices.Contains(args, "app-server") {
		t.Fatalf("args=%v", args)
	}
}

func TestBrainAppServerArgsFailClosedWithoutCodeModeHost(t *testing.T) {
	if _, err := brainAppServerArgs([]byte("shell_tool stable true\n")); err == nil {
		t.Fatal("brainAppServerArgs() accepted a build without code_mode_host")
	}
}

func TestInteractiveSessionCorrelatesResponsesAndDeliversIncomingFrames(t *testing.T) {
	client, server := net.Pipe()
	session := NewInteractiveSession(client, 4096)
	defer session.Close()
	defer server.Close()

	go func() {
		decoder := json.NewDecoder(server)
		var request map[string]any
		if err := decoder.Decode(&request); err != nil {
			return
		}
		_, _ = server.Write([]byte("{\"method\":\"turn/started\",\"params\":{\"threadId\":\"thread-a\"}}\n"))
		_, _ = server.Write([]byte("{\"id\":77,\"method\":\"item/commandExecution/requestApproval\",\"params\":{\"threadId\":\"thread-a\"}}\n"))
		_, _ = server.Write([]byte("{\"id\":1,\"result\":{\"ok\":true}}\n"))
		_ = decoder.Decode(&request)
	}()

	var result struct {
		OK bool `json:"ok"`
	}
	if err := session.Call(context.Background(), "thread/read", map[string]any{}, &result); err != nil || !result.OK {
		t.Fatalf("Call() = %#v, %v", result, err)
	}
	first := <-session.Incoming()
	second := <-session.Incoming()
	if first.Method != "turn/started" || first.IsRequest() {
		t.Fatalf("first incoming = %#v", first)
	}
	if second.Method != "item/commandExecution/requestApproval" || !second.IsRequest() || string(second.ID) != "77" {
		t.Fatalf("second incoming = %#v", second)
	}
	if err := session.Reply(context.Background(), second.ID, map[string]any{"decision": "decline"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestInteractiveSessionPreservesBurstWhileConsumerIsSlow(t *testing.T) {
	client, server := net.Pipe()
	session := NewInteractiveSession(client, 4096)
	defer session.Close()
	defer server.Close()
	callResult := make(chan error, 1)
	go func() {
		var result struct {
			OK bool `json:"ok"`
		}
		callResult <- session.Call(context.Background(), "thread/read", nil, &result)
	}()
	decoder := json.NewDecoder(server)
	var request map[string]any
	if err := decoder.Decode(&request); err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("x", 2048)
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	const burst = 2048
	for i := 0; i < burst; i++ {
		if _, err := fmt.Fprintf(server, "{\"method\":\"item/updated\",\"params\":{\"index\":%d,\"payload\":%s}}\n", i, encoded); err != nil {
			t.Fatalf("write notification %d after transport error %v: %v", i, session.Err(), err)
		}
		if i == burst/2 {
			if _, err := server.Write([]byte("{\"id\":77,\"method\":\"item/tool/call\",\"params\":{}}\n")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := server.Write([]byte("{\"id\":1,\"result\":{\"ok\":true}}\n")); err != nil {
		t.Fatal(err)
	}
	secondCallResult := make(chan error, 1)
	go func() {
		secondCallResult <- session.Call(context.Background(), "thread/read", nil, new(map[string]any))
	}()
	if err := decoder.Decode(&request); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Write([]byte("{\"id\":2,\"result\":{\"ok\":true}}\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-callResult:
		if err != nil {
			t.Fatalf("response behind burst failed: %v", err)
		}
	case <-session.Done():
		t.Fatalf("burst closed session: %v", session.Err())
	case <-time.After(time.Second):
		t.Fatal("response behind burst was not dispatched")
	}
	select {
	case err := <-secondCallResult:
		if err != nil {
			t.Fatalf("second response behind burst failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second response behind burst was not dispatched")
	}
	for i := 0; i < burst+1; i++ {
		frame := <-session.Incoming()
		if i == burst/2+1 {
			if frame.Method != "item/tool/call" || !frame.IsRequest() || string(frame.ID) != "77" {
				t.Fatalf("tool request out of order at %d: %#v", i, frame)
			}
			replyResult := make(chan error, 1)
			requestID := append(json.RawMessage(nil), frame.ID...)
			go func() {
				replyResult <- session.Reply(context.Background(), requestID, map[string]any{"success": true}, nil)
			}()
			if err := decoder.Decode(&request); err != nil || request["id"] != float64(77) {
				t.Fatalf("tool reply: %#v, %v", request, err)
			}
			if err := <-replyResult; err != nil {
				t.Fatal(err)
			}
			continue
		}
		expected := i
		if i > burst/2+1 {
			expected--
		}
		if frame.Method != "item/updated" {
			t.Fatalf("notification out of order at %d: %#v", i, frame)
		}
		var params struct {
			Index   int    `json:"index"`
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal(frame.Params, &params); err != nil || params.Index != expected || params.Payload != payload {
			t.Fatalf("frame %d index=%d, err=%v", i, params.Index, err)
		}
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("close did not stop session")
	}
}

func TestInteractiveSessionIncomingQueueReclaimsFIFOStorage(t *testing.T) {
	s := &InteractiveSession{done: make(chan struct{}), incoming: make(chan IncomingFrame), incomingReady: make(chan struct{}, 1)}
	const backlog = 4
	for i := 0; i < backlog; i++ {
		if err := s.enqueueIncoming(IncomingFrame{Method: "item/updated", Params: json.RawMessage(fmt.Sprintf(`{"index":%d}`, i))}); err != nil {
			t.Fatal(err)
		}
	}
	deliveryDone := make(chan struct{})
	go func() {
		defer close(deliveryDone)
		s.deliverIncoming()
	}()
	defer func() {
		close(s.done)
		<-deliveryDone
	}()
	for i := 0; i < 2048; i++ {
		if err := s.enqueueIncoming(IncomingFrame{Method: "item/updated", Params: json.RawMessage(fmt.Sprintf(`{"index":%d}`, i+backlog))}); err != nil {
			t.Fatal(err)
		}
		s.incomingMu.Lock()
		retained := cap(s.incomingQueue)
		s.incomingMu.Unlock()
		if retained > 2*backlog {
			t.Fatalf("cycle %d retained backing storage for %d frames with backlog %d", i, retained, backlog)
		}
		frame := <-s.Incoming()
		if string(frame.Params) != fmt.Sprintf(`{"index":%d}`, i) {
			t.Fatalf("frame %d = %s", i, frame.Params)
		}
	}
}

func TestInteractiveSessionIncomingQueueAccountsFrameMetadata(t *testing.T) {
	overhead := int(unsafe.Sizeof(IncomingFrame{}))
	for _, frame := range []IncomingFrame{
		{},
		{Params: json.RawMessage{}},
		{Method: "m", Params: json.RawMessage("{}")},
	} {
		session := &InteractiveSession{done: make(chan struct{}), incomingReady: make(chan struct{}, 1)}
		if err := session.enqueueIncoming(frame); err != nil {
			t.Fatal(err)
		}
		want := overhead + len(frame.ID) + len(frame.Method) + len(frame.Params)
		if session.incomingBytes != want {
			t.Fatalf("frame charge = %d, want %d", session.incomingBytes, want)
		}
	}

	atLimit := &InteractiveSession{done: make(chan struct{}), incomingReady: make(chan struct{}, 1), incomingBytes: interactiveIncomingByteLimit - overhead}
	if err := atLimit.enqueueIncoming(IncomingFrame{}); err != nil {
		t.Fatalf("exact-budget empty frame: %v", err)
	}
	if err := atLimit.enqueueIncoming(IncomingFrame{}); err != ErrCapacity {
		t.Fatalf("empty frame beyond budget = %v", err)
	}
	tinyOverflow := &InteractiveSession{done: make(chan struct{}), incomingReady: make(chan struct{}, 1), incomingBytes: interactiveIncomingByteLimit - overhead - 1}
	if err := tinyOverflow.enqueueIncoming(IncomingFrame{Params: json.RawMessage("{}")}); err != ErrCapacity {
		t.Fatalf("tiny frame beyond budget = %v", err)
	}
}

func TestInteractiveSessionIncomingQueueHasBoundedAdmission(t *testing.T) {
	session := &InteractiveSession{done: make(chan struct{}), incomingReady: make(chan struct{}, 1)}
	overhead := int(unsafe.Sizeof(IncomingFrame{}))
	if err := session.enqueueIncoming(IncomingFrame{Method: "item/updated", Params: make(json.RawMessage, interactiveIncomingByteLimit-overhead-len("item/updated"))}); err != nil {
		t.Fatal(err)
	}
	if err := session.enqueueIncoming(IncomingFrame{Method: "item/updated", Params: json.RawMessage("{}")}); err != ErrCapacity {
		t.Fatalf("overflow error = %v", err)
	}
	close(session.done)
	session.err = ErrClosed
	if err := session.enqueueIncoming(IncomingFrame{Method: "item/updated"}); err != ErrClosed {
		t.Fatalf("enqueue after cancellation = %v", err)
	}
}

func TestInteractiveSessionRejectsOversizeUnsolicitedFrame(t *testing.T) {
	client, server := net.Pipe()
	session := NewInteractiveSession(client, 64)
	defer server.Close()
	_, _ = server.Write([]byte("{\"method\":\"item/updated\",\"params\":{\"text\":\"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\"}}\n"))
	select {
	case <-session.Done():
		if session.Err() != ErrProtocol {
			t.Fatalf("Err() = %v", session.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("oversize frame did not close session")
	}
}

func TestInteractiveProcessExitDiagnosticSeparatesSignalTransportAndOwnership(t *testing.T) {
	command := exec.Command("sh", "-c", "kill -TERM $$")
	if err := command.Run(); err == nil {
		t.Fatal("signal fixture unexpectedly succeeded")
	}
	diagnostic := newInteractiveProcessExitDiagnostic(context.Background(), command.ProcessState, nil, true)
	if diagnostic.Kind != "signal" || diagnostic.Signal != "terminated" || diagnostic.Transport != "none" || diagnostic.ContextCancelled {
		t.Fatalf("signal diagnostic=%+v", diagnostic)
	}
	if got := newInteractiveProcessExitDiagnostic(context.Background(), command.ProcessState, ErrProtocol, true); got.Transport != "protocol" {
		t.Fatalf("transport diagnostic=%+v", got)
	}
	if got := newInteractiveProcessExitDiagnostic(context.Background(), nil, nil, false); got.Kind != "none" || got.Transport != "closed" {
		t.Fatalf("pipe-close diagnostic=%+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := newInteractiveProcessExitDiagnostic(ctx, nil, nil, false); !got.ContextCancelled {
		t.Fatalf("cancelled owner was not identified: %+v", got)
	}
}

func TestInteractiveProcessExitDiagnosticSeparatesExitCode(t *testing.T) {
	command := exec.Command("sh", "-c", "exit 17")
	if err := command.Run(); err == nil {
		t.Fatal("exit fixture unexpectedly succeeded")
	}
	if got := newInteractiveProcessExitDiagnostic(context.Background(), command.ProcessState, nil, true); got.Kind != "exit" || got.ExitCode != 17 || got.Transport != "none" {
		t.Fatalf("exit diagnostic=%+v", got)
	}
}

func TestInteractiveProcessExitDiagnosticIdentifiesFullIncomingQueueAfterExit(t *testing.T) {
	process := &InteractiveProcess{
		InteractiveSession: &InteractiveSession{err: ErrCapacity},
		cmd:                &exec.Cmd{},
		ctx:                context.Background(),
		exited:             make(chan struct{}),
	}
	close(process.exited)
	if diagnostic := process.ExitDiagnostic(); diagnostic.Transport != "capacity" {
		t.Fatalf("transport diagnostic=%+v", diagnostic)
	}
}

func TestInteractiveProcessExitDiagnosticBoundsSessionDoneWithoutProcessExit(t *testing.T) {
	done := make(chan struct{})
	close(done)
	process := &InteractiveProcess{
		InteractiveSession: &InteractiveSession{done: done},
		cmd:                &exec.Cmd{},
		ctx:                context.Background(),
		exited:             make(chan struct{}),
	}
	diagnosticCh := make(chan InteractiveProcessExitDiagnostic, 1)
	go func() { diagnosticCh <- process.ExitDiagnostic() }()
	select {
	case diagnostic := <-diagnosticCh:
		if diagnostic.Kind != "none" || diagnostic.Transport != "closed" {
			t.Fatalf("transport-only diagnostic=%+v", diagnostic)
		}
	case <-time.After(2 * time.Second):
		close(process.exited)
		t.Fatal("session done with live process blocked exit diagnostic")
	}

	command := exec.Command("sh", "-c", "kill -TERM $$")
	if err := command.Run(); err == nil {
		t.Fatal("signal fixture unexpectedly succeeded")
	}
	process.cmd.ProcessState = command.ProcessState
	close(process.exited)
	if diagnostic := process.ExitDiagnostic(); diagnostic.Kind != "signal" || diagnostic.Signal != "terminated" {
		t.Fatalf("completed process diagnostic=%+v", diagnostic)
	}
}
