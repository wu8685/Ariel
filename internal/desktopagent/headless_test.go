package desktopagent

import (
	"context"
	"strings"
	"testing"
)

func TestHeadlessRefusesDesktopIPC(t *testing.T) {
	err := Run(context.Background(), Config{Headless: true, Binary: "codex", Socket: "/tmp/ipc.sock", URL: "ws://127.0.0.1/ws", Token: "token", DeviceID: "device", DeviceName: "Headless", CWD: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "Desktop IPC") {
		t.Fatalf("err=%v", err)
	}
}

func TestHeadlessLiveDoesNotDial(t *testing.T) {
	live := newHeadlessLive("/nonexistent/codex-e2e", "thread", t.TempDir())
	raw, err := live.Current()
	if err != nil || !strings.Contains(string(raw), `"requests":[]`) {
		t.Fatalf("current=%s err=%v", raw, err)
	}
	if _, err := live.Start(context.Background(), "message", "hello"); err == nil {
		t.Fatal("start should fail without a real binary, not dial IPC")
	}
}
