package appserver

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"slices"
	"testing"
	"time"
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

func TestInteractiveSessionFailsClosedWhenIncomingConsumerIsSlow(t *testing.T) {
	client, server := net.Pipe()
	session := NewInteractiveSession(client, 4096)
	defer server.Close()
	for i := 0; i < interactiveIncomingLimit+1; i++ {
		_, _ = server.Write([]byte("{\"method\":\"item/updated\",\"params\":{}}\n"))
	}
	select {
	case <-session.Done():
		if session.Err() != ErrCapacity {
			t.Fatalf("Err() = %v", session.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("slow incoming consumer did not close session")
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
