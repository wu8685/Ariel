package chilling

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func testServer(t *testing.T, contract string) string {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "ariel-chilling-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	path := filepath.Join(base, "fc.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/file-context/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"contract": contract, "enabled": true, "ready": true, "messageReferences": true, "workspaceBrowser": true, "limits": map[string]int{"maxTurnBatch": 10, "maxFileBytes": 1048576, "maxSelections": 5, "maxSelectionBytes": 8192, "maxTotalBytes": 24576}})
	})
	mux.HandleFunc("POST /v1/file-context/changes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"turns": []any{map[string]any{"turnId": "turn-a", "fileChanges": []any{map[string]any{"itemId": "item-a", "path": "a.go", "kind": "update"}}}}})
	})
	mux.HandleFunc("POST /v1/file-context/read", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"path": "a.go", "content": "hello", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "modifiedAtMs": 1, "lineCount": 1, "language": "go", "version": "current-workspace"})
	})
	mux.HandleFunc("POST /v1/file-context/reference/read", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"path": "ops.md", "content": "weekly", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "modifiedAtMs": 2, "lineCount": 1, "language": "md", "version": "current-workspace"})
	})
	mux.HandleFunc("POST /v1/file-context/workspace/files/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"path": "", "entries": []any{map[string]any{"name": "docs", "path": "docs", "kind": "directory", "sizeBytes": 64, "modifiedAtMs": 2, "readable": true}}, "revision": "abc"})
	})
	mux.HandleFunc("POST /v1/file-context/workspace/file/read", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"path": "README.md", "content": "hello", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "modifiedAtMs": 1, "lineCount": 1, "language": "md", "version": "current-workspace"})
	})
	go http.Serve(listener, mux)
	return "unix://" + path
}

func TestClientRequiresFlagAndCompatibleHandshake(t *testing.T) {
	if _, err := New(context.Background(), Config{Enabled: false, Endpoint: "unix:///tmp/missing"}); CodeOf(err) != "FEATURE_DISABLED" {
		t.Fatalf("disabled: %v", err)
	}
	if _, err := New(context.Background(), Config{Enabled: true, Endpoint: testServer(t, "file-context/v2")}); CodeOf(err) != "CONTRACT_UNSUPPORTED" {
		t.Fatalf("contract: %v", err)
	}
	client, err := New(context.Background(), Config{Enabled: true, Endpoint: testServer(t, "file-context/v1")})
	if err != nil || !client.Ready() {
		t.Fatalf("ready: %v", err)
	}
	if !client.MessageReferences() {
		t.Fatal("message reference capability missing")
	}
	if !client.WorkspaceBrowser() {
		t.Fatal("workspace browser capability missing")
	}
}

func TestClientWorkspaceBrowserIsBoundedAndTyped(t *testing.T) {
	client, err := New(context.Background(), Config{Enabled: true, Endpoint: testServer(t, "file-context/v1")})
	if err != nil {
		t.Fatal(err)
	}
	list, err := client.WorkspaceList(context.Background(), WorkspaceListRequest{ThreadID: "thread-a", Limit: 100})
	if err != nil || len(list.Entries) != 1 || list.Entries[0].Kind != "directory" {
		t.Fatalf("list: %#v %v", list, err)
	}
	read, err := client.WorkspaceRead(context.Background(), WorkspaceReadRequest{ThreadID: "thread-a", Path: "README.md"})
	if err != nil || read.Content != "hello" {
		t.Fatalf("read: %#v %v", read, err)
	}
}

func TestClientOnlyExposesChangesReadAndPrepare(t *testing.T) {
	client, err := New(context.Background(), Config{Enabled: true, Endpoint: testServer(t, "file-context/v1")})
	if err != nil {
		t.Fatal(err)
	}
	changes, err := client.Changes(context.Background(), ChangesRequest{ThreadID: "thread-a", TurnIDs: []string{"turn-a"}})
	if err != nil || len(changes.Turns) != 1 || changes.Turns[0].FileChanges[0].Path != "a.go" {
		t.Fatalf("changes: %+v %v", changes, err)
	}
	read, err := client.Read(context.Background(), ReadRequest{ThreadID: "thread-a", TurnID: "turn-a", ItemID: "item-a", Path: "a.go"})
	if err != nil || read.Content != "hello" {
		t.Fatalf("read: %+v %v", read, err)
	}
	reference, err := client.ReferenceRead(context.Background(), ReferenceReadRequest{ThreadID: "thread-a", TurnID: "turn-a", ItemID: "reply", ReferenceKind: "plain_path", RawReference: "ops.md"})
	if err != nil || reference.Content != "weekly" {
		t.Fatalf("reference: %+v %v", reference, err)
	}
}
