package relay

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
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

func TestStaticSVGIsAdvertisedAndOnlyForwardedWithTwoSidedOptIn(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	a := dialTest(t, server.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, server.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	count := 0
	request := func(method string, params map[string]any) {
		count++
		sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": fmt.Sprintf("00000000-0000-4000-8000-%012d", count), "deviceId": "mock-mac", "method": method, "params": params})
	}
	request("device.list", map[string]any{})
	if readJSON(t, w)["data"].(map[string]any)["documentSvgVersion"] != float64(1) {
		t.Fatal("SVG extension not advertised")
	}
	params := map[string]any{"source": "workspace_browser", "threadId": "t", "path": "read.md", "agentEpoch": "epoch-a", "documentSha256": strings.Repeat("a", 64), "imageReference": "a.svg", "svgVersion": 1}
	request("file.context.asset.read", params)
	if readJSON(t, w)["outcome"] != "rejected" {
		t.Fatal("unconfirmed SVG forwarded")
	}
	for _, svgVersion := range []int{0, 1} {
		request("workspace.file.read", map[string]any{"threadId": "t", "path": "read.md"})
		forwarded := readJSON(t, a)
		sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"documentAssets": map[string]any{"version": 1, "maxBytes": 4 << 20, "chunkBytes": 256 << 10, "svgVersion": svgVersion}}})
		readJSON(t, w)
		request("file.context.asset.read", params)
		if svgVersion == 0 {
			if readJSON(t, w)["outcome"] != "rejected" {
				t.Fatal("old Agent received SVG opt-in")
			}
			continue
		}
		forwarded = readJSON(t, a)
		body := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`)
		data := map[string]any{"mime": "image/svg+xml", "assetSha256": strings.Repeat("a", 64), "width": 1, "height": 1, "offset": 0, "totalBytes": len(body), "dataBase64": base64.StdEncoding.EncodeToString(body)}
		sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": data})
		if readJSON(t, w)["outcome"] != "accepted" {
			t.Fatal("negotiated SVG rejected")
		}
		delete(params, "svgVersion")
		request("file.context.asset.read", params)
		forwarded = readJSON(t, a)
		sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": data})
		if readJSON(t, w)["outcome"] != "rejected" {
			t.Fatal("legacy Web received SVG MIME")
		}
	}
	// Capabilities belong to a transport, even when the new Agent advertises
	// the same device ID and epoch. Never inherit an old read receipt.
	replacement := dialTest(t, server.URL, "")
	sendJSON(t, replacement, agentHello())
	readJSON(t, replacement)
	if readJSON(t, w)["type"] != "event" {
		t.Fatal("missing replacement status")
	}
	params["svgVersion"] = 1
	request("file.context.asset.read", params)
	if readJSON(t, w)["outcome"] != "rejected" {
		t.Fatal("new Agent inherited retired SVG capability")
	}
}
