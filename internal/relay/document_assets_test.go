package relay

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetReplyValidatesDecodedChunkAndCursor(t *testing.T) {
	for _, sample := range []struct {
		name, cursor string
		chunk, total int
	}{
		{name: "base64 padding must not permit raw chunk overflow", chunk: (256 << 10) + 1, total: (256 << 10) + 1},
		{name: "cursor shape", cursor: "invalid cursor!", chunk: 1, total: 2},
	} {
		t.Run(sample.name, func(t *testing.T) {
			data := map[string]any{"mime": "image/png", "assetSha256": strings.Repeat("a", 64), "width": float64(1), "height": float64(1), "offset": float64(0), "totalBytes": float64(sample.total), "dataBase64": base64.StdEncoding.EncodeToString(make([]byte, sample.chunk))}
			if sample.cursor != "" {
				data["nextCursor"] = sample.cursor
			}
			body, _ := json.Marshal(data)
			if validAssetReply(body, data) {
				t.Fatal("invalid resource was forwarded")
			}
		})
	}
}

func TestAssetExtensionRequiresTrustedReadReceiptAndCurrentEpoch(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	a := dialTest(t, server.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, server.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	request := func(id, method string, params map[string]any) {
		sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-" + id, "deviceId": "mock-mac", "method": method, "params": params})
	}
	request("000000000001", "device.list", map[string]any{})
	if readJSON(t, w)["data"].(map[string]any)["documentAssetsVersion"] != float64(1) {
		t.Fatal("Relay did not advertise extension in optional data")
	}
	params := map[string]any{"source": "workspace_browser", "threadId": "t", "path": "read.md", "agentEpoch": "epoch-a", "documentSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "imageReference": "a.png"}
	request("000000000002", "file.context.asset.read", params)
	if readJSON(t, w)["outcome"] != "rejected" {
		t.Fatal("unconfirmed assets forwarded")
	}
	request("000000000003", "workspace.file.read", map[string]any{"threadId": "t", "path": "read.md"})
	forwarded := readJSON(t, a)
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"documentAssets": map[string]any{"version": 1, "maxBytes": 4 << 20, "chunkBytes": 256 << 10}}})
	readJSON(t, w)
	request("000000000004", "file.context.asset.read", params)
	if readJSON(t, a)["method"] != "file.context.asset.read" {
		t.Fatal("confirmed asset not forwarded")
	}
	params["agentEpoch"] = "old"
	request("000000000005", "file.context.asset.read", params)
	if readJSON(t, w)["outcome"] != "rejected" {
		t.Fatal("stale epoch accepted")
	}
}
