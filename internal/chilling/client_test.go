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
		_ = json.NewEncoder(w).Encode(map[string]any{"contract": contract, "enabled": true, "ready": true, "limits": map[string]int{"maxTurnBatch": 10, "maxFileBytes": 1048576, "maxSelections": 5, "maxSelectionBytes": 8192, "maxTotalBytes": 24576}})
	})
	mux.HandleFunc("POST /v1/file-context/changes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"turns": []any{map[string]any{"turnId": "turn-a", "fileChanges": []any{map[string]any{"itemId": "item-a", "path": "a.go", "kind": "update"}}}}})
	})
	mux.HandleFunc("POST /v1/file-context/read", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"path": "a.go", "content": "hello", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "modifiedAtMs": 1, "lineCount": 1, "language": "go", "version": "current-workspace"})
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
}
