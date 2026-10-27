package desktopagent

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/wu8685/Ariel/internal/relay"
)

func TestBuiltinFilesAndImagesThroughRealAgentRelayWithoutChilling(t *testing.T) {
	root := t.TempDir()
	content := "hello\n\n![pixel](pixel.png)"
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	var imageBody bytes.Buffer
	if err := png.Encode(&imageBody, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pixel.png"), imageBody.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	router, _ := relay.New(relay.Config{Token: "fixture-token", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	server := httptest.NewServer(router.Handler())
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	cfg := Config{URL: url, Token: "fixture-token", DeviceID: "fixture-mac", DeviceName: "Fixture", AgentEpoch: "fixture-epoch", FileContextEnabled: true, WorkspaceBrowserEnabled: true, DocumentAssetsEnabled: true, ChillingEndpoint: "unix:///no-chilling.sock"}
	configureFileContext(&cfg, &builtinProof{root: root})
	service := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) {
		return nil, fmt.Errorf("file reads must not create a follower")
	})
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan struct{})
	cfg.OnReady = func() error { close(ready); return nil }
	stopped := make(chan error, 1)
	go func() { stopped <- RunWithService(ctx, cfg, service) }()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("fixture Agent not ready")
	}
	web, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer web.CloseNow()
	if err = wsjson.Write(ctx, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}); err != nil {
		t.Fatal(err)
	}
	var reply map[string]any
	if err = wsjson.Read(ctx, web, &reply); err != nil {
		t.Fatal(err)
	}
	count := 0
	request := func(method string, params map[string]any) map[string]any {
		t.Helper()
		count++
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", count)
		if err := wsjson.Write(ctx, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "fixture-mac", "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		for {
			var response map[string]any
			if err := wsjson.Read(ctx, web, &response); err != nil {
				t.Fatal(err)
			}
			if response["requestId"] != id {
				continue
			}
			if response["outcome"] != "accepted" {
				t.Fatalf("%s rejected: %v", method, response["error"])
			}
			return response["data"].(map[string]any)
		}
	}
	list := request("device.list", map[string]any{})
	if list["documentAssetsVersion"] != float64(1) {
		t.Fatal("extension missing")
	}
	changed := request("file.context.changes", map[string]any{"threadId": "t", "turnIds": []string{"turn"}})
	if len(changed["turns"].([]any)) != 1 {
		t.Fatal("changes missing")
	}
	doc := request("workspace.file.read", map[string]any{"threadId": "t", "path": "a.md"})
	if doc["content"] != content {
		t.Fatal("read mismatch")
	}
	resource := request("file.context.asset.read", map[string]any{"threadId": "t", "source": "workspace_browser", "path": "a.md", "agentEpoch": cfg.AgentEpoch, "documentSha256": doc["sha256"], "imageReference": "pixel.png"})
	if resource["mime"] != "image/png" || resource["width"] != float64(2) || resource["height"] != float64(3) {
		t.Fatal("resource mismatch")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("fixture Agent did not stop")
	}
}
