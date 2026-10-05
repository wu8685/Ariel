package protocol

import "testing"

func TestV1EnvelopeSchemaAcceptsKnownMessages(t *testing.T) {
	valid := []string{
		`{"type":"hello","v":1,"role":"web","token":"secret"}`,
		`{"type":"hello","v":1,"role":"agent","token":"secret","deviceId":"device-a","deviceName":"Mac","agentEpoch":"epoch-a","adapterVersion":"mock-1","capabilities":{"autoLoad":false,"codexReady":false}}`,
		`{"type":"hello","v":1,"role":"agent","token":"secret","deviceId":"desktop-local","deviceName":"Desktop 本机","agentEpoch":"epoch-a","adapterVersion":"desktop-ipc-0.160.0","capabilities":{"autoLoad":true,"codexReady":true,"history":true,"send":true,"interrupt":true,"interaction":true}}`,
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a"}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.list","params":{"limit":20}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000002","deviceId":"device-a","method":"thread.history","params":{"threadId":"thread-a","limit":10,"cursor":"opaque-turn"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000003","deviceId":"device-a","method":"thread.history.items","params":{"threadId":"thread-a","turnId":"turn-a","limit":100,"cursor":"opaque-item"}}`,
		`{"type":"response","v":1,"requestId":"00000000-0000-4000-8000-000000000001","outcome":"accepted","data":{"threads":[],"nextCursor":null}}`,
		`{"type":"event","v":1,"event":"device.status","deviceId":"device-a","agentOnline":true,"codexReady":false}`,
		`{"type":"event","v":1,"event":"thread.error","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","code":"HISTORY_TOO_LARGE"}`,
		`{"type":"event","v":1,"event":"thread.error","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","code":"RESYNC_REQUIRED"}`,
		`{"type":"event","v":1,"event":"thread.error","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","code":"NATIVE_STATE_UNCERTAIN"}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[],"pendingInteractions":[],"permissions":{"sandbox":"full_access","approval":"on_request"},"historyComplete":false,"recentComplete":true}}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[{"turnId":"turn-a","status":"completed","items":[],"itemsComplete":false,"nextItemCursor":"older-items"}],"pendingInteractions":[],"historyComplete":false,"recentComplete":true}}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[{"turnId":"turn-a","status":"completed","items":[{"itemId":"tool-a","role":"system","text":"工具调用","activity":{"kind":"mcpToolCall","label":"fixture/lookup","status":"completed","details":"参数: {}","truncated":false}}]}],"pendingInteractions":[]}}`,
	}
	for _, raw := range valid {
		if err := Validate([]byte(raw)); err != nil {
			t.Errorf("valid message rejected: %s: %v", raw, err)
		}
	}
}

func TestV1EnvelopeSchemaRejectsWrongVersionShapeAndUnexpectedFields(t *testing.T) {
	invalid := []string{
		`{"type":"hello","v":2,"role":"web","token":"secret"}`,
		`{"type":"hello","v":1,"role":"agent","token":"secret"}`,
		`{"type":"hello","v":1,"role":"web","token":"secret","deviceId":"claimed"}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"turn.start","params":{"text":"hi"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"unknown","params":{}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000002","deviceId":"device-a","method":"thread.history","params":{"threadId":"thread-a","limit":11}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000003","deviceId":"device-a","method":"thread.history.items","params":{"threadId":"thread-a","limit":100}}`,
		`{"type":"response","v":1,"requestId":"00000000-0000-4000-8000-000000000001","outcome":"rejected"}`,
		`{"type":"event","v":1,"event":"thread.update","deviceId":"device-a","threadId":"mock-a","subscriptionId":"sub-a","streamId":"stream-a","seq":2,"items":[]}`,
		`{"type":"event","v":1,"event":"thread.error","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","code":"UNKNOWN"}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[{"turnId":"turn-a","status":"completed","items":[{"itemId":"tool-a","role":"system","text":"工具调用","activity":{"kind":"mcpToolCall","label":"fixture/lookup","status":"completed","details":"x","truncated":false,"unsafe":true}}]}],"pendingInteractions":[]}}`,
		`not-json`,
	}
	for _, raw := range invalid {
		if err := Validate([]byte(raw)); err == nil {
			t.Errorf("invalid message accepted: %s", raw)
		}
	}
}
