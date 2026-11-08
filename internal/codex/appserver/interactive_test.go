package appserver

import (
	"context"
	"encoding/json"
	"net"
	"slices"
	"testing"
	"time"
)

func TestBrainAppServerArgsKeepDynamicToolHostAndDisableLocalSurfaces(t *testing.T) {
	args, err := brainAppServerArgs([]byte("shell_tool stable true\nunified_exec stable true\nimage_generation stable true\ngoals stable true\nbrowser_use stable true\nbrowser_use_external stable true\ncomputer_use stable true\nin_app_browser stable true\nmulti_agent stable true\napps stable true\nplugins stable true\ntool_suggest stable true\nskill_search stable true\nview_image removed false\nsleep_tool removed false\ncode_mode_host stable true\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, feature := range []string{"shell_tool", "unified_exec", "browser_use_external", "multi_agent", "plugins"} {
		if !slices.Contains(args, feature) {
			t.Errorf("missing disabled feature %q in %v", feature, args)
		}
	}
	if slices.Contains(args, "code_mode_host") {
		t.Fatalf("code_mode_host must remain enabled for dynamicTools: %v", args)
	}
	if slices.Contains(args, "view_image") || slices.Contains(args, "sleep_tool") {
		t.Fatalf("removed feature must not be passed to Codex: %v", args)
	}
}

func TestBrainAppServerArgsFailClosedWithoutRequiredFeatureInventory(t *testing.T) {
	if _, err := brainAppServerArgs([]byte("shell_tool stable true\n")); err == nil {
		t.Fatal("brainAppServerArgs() accepted incomplete feature inventory")
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
