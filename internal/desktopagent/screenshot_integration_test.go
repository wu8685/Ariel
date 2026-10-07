package desktopagent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/probe"
)

// Opt-in and read-only: validates a real high-resolution PNG/JPEG named by the caller.
func TestRealLargeConversationImagePreview(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_LARGE_IMAGE")
	if path == "" {
		t.Skip("requires a real high-resolution image fixture")
	}
	raw, err := json.Marshal(map[string]any{"id": "large-image", "type": "agentMessage", "text": "![large](" + path + ")"})
	if err != nil {
		t.Fatal(err)
	}
	uri, err := imageFromNativeItem(raw, "/", 0)
	if err != nil {
		t.Fatalf("large image preview failed: %v", err)
	}
	prefix := "data:image/png;base64,"
	if strings.HasPrefix(uri, "data:image/jpeg;base64,") {
		prefix = "data:image/jpeg;base64,"
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, prefix))
	if err != nil || len(data) > maxScreenshotBytes {
		t.Fatalf("preview payload: bytes=%d err=%v", len(data), err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width > defaultImagePresentationLimits.maxDimension || config.Height > defaultImagePresentationLimits.maxDimension || int64(config.Width)*int64(config.Height) > defaultImagePresentationLimits.maxRenderedPixels {
		t.Fatalf("preview dimensions = %dx%d err=%v", config.Width, config.Height, err)
	}
}

// Opt-in: this sends exactly one screenshot to a manifest-guarded fixture.
func TestRealDesktopScreenshotTurn(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_SCREENSHOT_MANIFEST")
	if path == "" || os.Getenv("ARIEL_TEST_WRITE_ENABLED") != "1" {
		t.Skip("requires an explicitly authorized isolated screenshot fixture")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil {
		t.Fatalf("fixture manifest unavailable: %v", err)
	}
	if manifest.Purpose != "" {
		t.Fatal("wrong fixture purpose")
	}
	if err := manifest.Authorize(true, manifest.ThreadID, manifest.Workspace); err != nil {
		t.Fatalf("invalid isolated fixture: %v", err)
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	live, err := OpenFollower(ctx, cfg.Socket, manifest.ThreadID, manifest.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	before, err := live.Current()
	if err != nil {
		t.Fatal(err)
	}
	thread, err := NormalizeLive(manifest.ThreadID, "isolated screenshot fixture", manifest.Workspace, before)
	if err != nil || thread["runtime"] != "idle" || len(thread["pendingInteractions"].([]any)) != 0 {
		t.Fatal("fixture not idle")
	}
	messageID := fixtureMessageID(t)
	uri := "data:image/png;base64," + tinyPNG
	turnID, err := live.(interface {
		StartWithImages(context.Context, string, string, []string) (string, error)
	}).StartWithImages(ctx, messageID, "", []string{uri})
	if err != nil {
		t.Fatalf("native screenshot start outcome: %v", err)
	}
	state, err := live.(*desktopipc.Follower).Refresh(ctx)
	if err != nil || !desktopipc.TurnContainsClientMessageImages(state, turnID, messageID, "", 1) {
		t.Fatal("native screenshot not verified in owner turn")
	}
	projected, err := NormalizeLive(manifest.ThreadID, "isolated screenshot fixture", manifest.Workspace, state)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, turn := range projected["turns"].([]any) {
		if turn.(map[string]any)["turnId"] != turnID {
			continue
		}
		for _, projectedItem := range turn.(map[string]any)["items"].([]any) {
			item := projectedItem.(map[string]any)
			if item["role"] != "user" {
				continue
			}
			if len(item["images"].([]any)) != 1 {
				t.Fatal("native screenshot reference absent")
			}
			raw := nativeItemFromState(state, turnID, item["itemId"].(string))
			if raw == nil {
				t.Fatal("native screenshot item not found")
			}
			dataURI, err := imageFromNativeItem(raw, manifest.Workspace, 0)
			if err != nil || dataURI != uri {
				t.Fatal("native screenshot bytes could not be read")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("native screenshot user item absent")
	}
	t.Log("native owner accepted screenshot-only turn and its image is readable")
}

func TestRealPersistedScreenshotRead(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_SCREENSHOT_MANIFEST")
	if path == "" {
		t.Skip("requires isolated screenshot fixture")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil || manifest.Purpose != "" || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
		t.Fatal("invalid isolated fixture")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	rpc, err := appserver.Start(ctx, probe.BundledBinary(cfg.AppPath), manifest.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	thread, err := (appserver.HistoryReader{RPC: rpc}).ReadFull(ctx, manifest.ThreadID)
	if err != nil || thread.CWD != manifest.Workspace {
		t.Fatal("fixture history unavailable")
	}
	found := false
	for _, turn := range thread.Turns {
		for _, raw := range turn.Items {
			var item struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(raw, &item) != nil || item.Type != "userMessage" || len(nativeImageSources(raw)) == 0 {
				continue
			}
			uri, err := imageFromNativeItem(raw, manifest.Workspace, 0)
			if err == nil && uri == "data:image/png;base64,"+tinyPNG {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("persisted original screenshot could not be rendered")
	}
	t.Log("independent App Server history retained a readable screenshot")
}
