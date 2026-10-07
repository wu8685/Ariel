package desktopagent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="

func TestLargeConversationImageIsDownscaledForPresentation(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 24), G: uint8(y * 48), B: 120, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	data, mime, err := prepareImageForPresentation(encoded.Bytes(), imagePresentationLimits{maxSourcePixels: 64, maxRenderedPixels: 8, maxDimension: 4})
	if err != nil || mime != "image/png" {
		t.Fatalf("prepare preview: mime=%q err=%v", mime, err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || config.Width != 4 || config.Height != 2 {
		t.Fatalf("preview = %dx%d %s, err=%v", config.Width, config.Height, format, err)
	}
}

func TestPresentationDimensionsBoundCurrentFailureShapeAndSourceMemory(t *testing.T) {
	width, height, err := presentationDimensions(7990, 7225, defaultImagePresentationLimits)
	if err != nil || width != 4096 || height != 3704 || int64(width)*int64(height) > defaultImagePresentationLimits.maxRenderedPixels {
		t.Fatalf("current fixture preview = %dx%d, err=%v", width, height, err)
	}
	if _, _, err := presentationDimensions(9000, 8000, defaultImagePresentationLimits); err == nil {
		t.Fatal("accepted a source above the bounded decode budget")
	}
}

func TestScreenshotReferencesStayOutOfSnapshotAndResolveFromNativeItem(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "reply.png")
	bytes, err := base64.StdEncoding.DecodeString(tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"id":"answer","type":"agentMessage","text":"See ![result](reply.png)"}`)
	turn, err := normalizeTurn("turn", "completed", []json.RawMessage{raw})
	if err != nil {
		t.Fatal(err)
	}
	item := turn["items"].([]any)[0].(map[string]any)
	images, ok := item["images"].([]any)
	if !ok || len(images) != 1 || strings.Contains(string(mustJSON(t, item)), tinyPNG) {
		t.Fatalf("image metadata missing or bytes leaked: %v", item)
	}
	got, err := imageFromNativeItem(raw, dir, 0)
	if err != nil || got != "data:image/png;base64,"+tinyPNG {
		t.Fatalf("resolved image: %v %q", err, got)
	}
	if _, err := imageFromNativeItem(raw, dir, 1); err == nil {
		t.Fatal("accepted nonexistent image index")
	}
}

func TestScreenshotEscapedMarkdownPathKeepsBrowserReferenceButReadsDecodedPath(t *testing.T) {
	dir := t.TempDir()
	data, _ := base64.StdEncoding.DecodeString(tinyPNG)
	if err := os.WriteFile(filepath.Join(dir, "shot one.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"id":"answer","type":"agentMessage","text":"![screenshot](shot%20one.png)"}`)
	turn, err := normalizeTurn("turn", "completed", []json.RawMessage{raw})
	if err != nil {
		t.Fatal(err)
	}
	ref := turn["items"].([]any)[0].(map[string]any)["images"].([]any)[0].(map[string]any)
	if ref["source"] != "shot%20one.png" {
		t.Fatalf("browser source changed: %v", ref["source"])
	}
	if got, err := imageFromNativeItem(raw, dir, 0); err != nil || got != "data:image/png;base64,"+tinyPNG {
		t.Fatalf("escaped path failed: %v", err)
	}
}

func TestCodexImageGenerationDataURIIsReadableWithoutSnapshotBytes(t *testing.T) {
	raw := json.RawMessage(`{"id":"generated","type":"imageGeneration","result":"data:image/png;base64,` + tinyPNG + `"}`)
	turn, err := normalizeTurn("turn", "completed", []json.RawMessage{raw})
	if err != nil {
		t.Fatal(err)
	}
	item := turn["items"].([]any)[0].(map[string]any)
	if item["role"] != "assistant" || len(item["images"].([]any)) != 1 || strings.Contains(string(mustJSON(t, item)), tinyPNG) {
		t.Fatalf("generated image projection: %v", item)
	}
	if got, err := imageFromNativeItem(raw, t.TempDir(), 0); err != nil || got != "data:image/png;base64,"+tinyPNG {
		t.Fatalf("generated image retrieval: %v", err)
	}
}

func TestScreenshotRejectsRemoteAndOversizedSources(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"id":"x","type":"agentMessage","text":"![remote](https://example.com/x.png)"}`),
		json.RawMessage(`{"id":"x","type":"userMessage","content":[{"type":"image","url":"https://example.com/x.png"}]}`),
	} {
		turn, err := normalizeTurn("turn", "completed", []json.RawMessage{raw})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := turn["items"].([]any)[0].(map[string]any)["images"]; ok {
			t.Fatalf("remote image advertised: %s", raw)
		}
		if strings.Contains(string(raw), `"type":"userMessage"`) && !strings.Contains(turn["items"].([]any)[0].(map[string]any)["text"].(string), "图片暂不可用") {
			t.Fatal("unsupported user image became an empty bubble")
		}
		if _, err := imageFromNativeItem(raw, t.TempDir(), 0); err == nil {
			t.Fatal("remote image fetched")
		}
	}
	large := "data:image/png;base64," + strings.Repeat("A", 6<<20)
	if err := validateUploadImages([]string{large}); err == nil {
		t.Fatal("oversized image accepted")
	}
	if err := validateUploadImages([]string{"data:image/svg+xml;base64," + tinyPNG}); err == nil {
		t.Fatal("SVG accepted")
	}
	if err := validateUploadImages([]string{"data:image/png;base64," + tinyPNG}); err != nil {
		t.Fatal(err)
	}
}

func TestScreenshotServiceCannotReadBrowserSuppliedPath(t *testing.T) {
	raw := json.RawMessage(`{"id":"user","type":"userMessage","content":[{"type":"localImage","path":"/missing/screenshot.png"}]}`)
	if _, err := imageFromNativeItem(raw, "/fixture", 0); err == nil {
		t.Fatal("missing native file should fail")
	}
	if _, err := imageFromNativeItem(raw, "/fixture", -1); err == nil {
		t.Fatal("invalid index")
	}
	file := filepath.Join(t.TempDir(), "native.png")
	content, _ := base64.StdEncoding.DecodeString(tinyPNG)
	if err := os.WriteFile(file, content, 0600); err != nil {
		t.Fatal(err)
	}
	item := json.RawMessage(`{"id":"native","type":"userMessage","content":[{"type":"localImage","path":"` + file + `"}]}`)
	fixture := singleFixtureHistory{thread: appserver.Thread{ID: "thread", CWD: "/fixture", Turns: []appserver.Turn{{ID: "turn", Status: "completed", Items: []json.RawMessage{item}}}}}
	service := NewService(fixture, nil)
	defer service.Close()
	got, err := service.Image(context.Background(), "thread", "turn", "native", 0)
	if err != nil || got != "data:image/png;base64,"+tinyPNG {
		t.Fatalf("native image read failed: %v", err)
	}
	if _, err := service.Image(context.Background(), "thread", "turn", "someone-else", 0); err == nil {
		t.Fatal("browser-selected item bypassed native lookup")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
