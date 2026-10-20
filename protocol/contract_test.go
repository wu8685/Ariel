package protocol

import (
	"strings"
	"testing"
)

func TestV1EnvelopeSchemaAcceptsKnownMessages(t *testing.T) {
	valid := []string{
		`{"type":"hello","v":1,"role":"web","token":"secret"}`,
		`{"type":"hello","v":1,"role":"agent","token":"secret","deviceId":"device-a","deviceName":"Mac","agentEpoch":"epoch-a","adapterVersion":"mock-1","capabilities":{"autoLoad":false,"codexReady":false}}`,
		`{"type":"hello","v":1,"role":"agent","token":"secret","deviceId":"desktop-local","deviceName":"Desktop 本机","agentEpoch":"epoch-a","adapterVersion":"desktop-ipc-0.160.0","capabilities":{"autoLoad":true,"codexReady":true,"history":true,"send":true,"interrupt":true,"interaction":true}}`,
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a"}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.list","params":{"limit":20}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.create","params":{"cwd":"/fixture"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.bootstrap","params":{"cwd":"/fixture","clientMessageId":"00000000-0000-4000-8000-000000000002","text":"hello"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.subscribe","params":{"threadId":"thread-a","updateMode":"item-delta-v1"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.archive.update","params":{"threadId":"thread-a","archived":true}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.archive.undo","params":{"threadId":"thread-a","operationId":"archive-operation"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.name.update","params":{"threadId":"thread-a","name":"新名称"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.pin.update","params":{"threadId":"thread-a","pinned":true}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000002","deviceId":"device-a","method":"thread.history","params":{"threadId":"thread-a","limit":10,"cursor":"opaque-turn"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000003","deviceId":"device-a","method":"thread.history.items","params":{"threadId":"thread-a","turnId":"turn-a","limit":100,"cursor":"opaque-item"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000003","deviceId":"device-a","method":"thread.image","params":{"threadId":"thread-a","turnId":"turn-a","itemId":"image-a","imageIndex":0}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000003","deviceId":"device-a","method":"turn.start","params":{"threadId":"thread-a","clientMessageId":"00000000-0000-4000-8000-000000000003","text":"","images":["data:image/png;base64,AAAA"]}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000003","deviceId":"device-a","method":"file.context.changes","params":{"threadId":"thread-a","turnIds":["turn-a"]}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000003","deviceId":"device-a","method":"file.context.read","params":{"threadId":"thread-a","turnId":"turn-a","itemId":"item-a","path":"internal/a.go"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000003","deviceId":"device-a","method":"turn.start","params":{"threadId":"thread-a","clientMessageId":"00000000-0000-4000-8000-000000000003","text":"review","fileContexts":[{"threadId":"thread-a","turnId":"turn-a","itemId":"item-a","path":"internal/a.go","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","modifiedAtMs":42,"startLine":1,"endLine":2,"text":"package a"}]}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000004","deviceId":"device-a","method":"queue.add","params":{"threadId":"thread-a","clientMessageId":"00000000-0000-4000-8000-000000000004","text":"follow up"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"queue.reorder","params":{"threadId":"thread-a","queueIds":["q2","q1"]}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000004","deviceId":"relay","method":"auth.pair.create","params":{}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"relay","method":"auth.pair.cancel","params":{"credential":"p_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`,
		`{"type":"response","v":1,"requestId":"00000000-0000-4000-8000-000000000001","outcome":"accepted","data":{"threads":[],"nextCursor":null}}`,
		`{"type":"event","v":1,"event":"device.status","deviceId":"device-a","agentOnline":true,"codexReady":false}`,
		`{"type":"event","v":1,"event":"auth.pair.consumed"}`,
		`{"type":"event","v":1,"event":"thread.error","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","code":"HISTORY_TOO_LARGE"}`,
		`{"type":"event","v":1,"event":"thread.error","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","code":"RESYNC_REQUIRED"}`,
		`{"type":"event","v":1,"event":"thread.error","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","code":"NATIVE_STATE_UNCERTAIN"}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[],"pendingInteractions":[],"queuedMessages":[{"queueId":"q1","clientMessageId":"c1","text":"later","images":[],"editable":true}],"permissions":{"sandbox":"full_access","approval":"on_request"},"historyComplete":false,"recentComplete":true}}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[{"turnId":"turn-a","status":"completed","items":[],"itemsComplete":false,"nextItemCursor":"older-items"}],"pendingInteractions":[],"historyComplete":false,"recentComplete":true}}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[{"turnId":"turn-a","status":"completed","items":[{"itemId":"user-a","role":"user","text":"hello","clientMessageId":"00000000-0000-4000-8000-000000000001"}]}],"pendingInteractions":[]}}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[{"turnId":"turn-a","status":"completed","items":[{"itemId":"tool-a","role":"system","text":"工具调用","activity":{"kind":"mcpToolCall","label":"fixture/lookup","status":"completed","details":"参数: {}","truncated":false}}]}],"pendingInteractions":[]}}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"inProgress","turns":[],"pendingInteractions":[{"interactionId":"input-a","kind":"user_input","prompt":"请回答 Codex 的补充问题","availableDecisions":["answer"],"questions":[{"id":"choice","header":"Choice","question":"Choose?","options":[{"label":"A","description":"Recommended"},{"label":"B"}]},{"id":"legacy","question":"Legacy?","options":["Yes"]}]}]}}`,
		`{"type":"event","v":1,"event":"thread.update","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","baseSeq":1,"seq":2,"patch":{"threadState":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:01Z","runtime":"inProgress","pendingInteractions":[],"queuedMessages":[]},"turnOrder":["turn-a"],"turnPatches":[{"turnId":"turn-a","status":"inProgress","itemOrder":["user-a","assistant-a"],"upsertItems":[{"itemId":"assistant-a","role":"assistant","text":"working"}],"removedItemIds":[]}],"removedTurnIds":[]}}`,
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
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.bootstrap","params":{"cwd":"relative","clientMessageId":"00000000-0000-4000-8000-000000000002","text":"hello"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"turn.start","params":{"threadId":"thread-a","clientMessageId":"00000000-0000-4000-8000-000000000001","text":""}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.image","params":{"threadId":"thread-a","turnId":"turn-a","itemId":"image-a","imageIndex":24}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"thread.subscribe","params":{"threadId":"thread-a","updateMode":"unknown"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"unknown","params":{}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000002","deviceId":"device-a","method":"thread.history","params":{"threadId":"thread-a","limit":11}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000003","deviceId":"device-a","method":"thread.history.items","params":{"threadId":"thread-a","limit":100}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000004","deviceId":"relay","method":"auth.pair.create","params":{"unexpected":true}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"relay","method":"auth.pair.cancel","params":{}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"thread.archive.update","params":{"threadId":"thread-a","archived":false}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"thread.archive.update","params":{"threadId":"thread-a","archived":true,"delete":true}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"thread.archive.undo","params":{"threadId":"thread-a","operationId":""}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"thread.name.update","params":{"threadId":"thread-a","name":"   "}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"thread.name.update","params":{"threadId":"thread-a","name":"bad\nname"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"thread.pin.update","params":{"threadId":"thread-a","pinned":"yes"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"thread.pin.update","params":{"threadId":"thread-a","pinned":true,"sectionId":"private"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"file.context.changes","params":{"threadId":"thread-a","turnIds":["1","2","3","4","5","6","7","8","9","10","11"]}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"file.context.read","params":{"threadId":"thread-a","turnId":"turn-a","itemId":"item-a","path":"/etc/passwd"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"file.context.proxy","params":{"url":"http://localhost"}}`,
		`{"type":"response","v":1,"requestId":"00000000-0000-4000-8000-000000000001","outcome":"rejected"}`,
		`{"type":"event","v":1,"event":"auth.pair.consumed","credential":"p_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
		`{"type":"event","v":1,"event":"thread.update","deviceId":"device-a","threadId":"mock-a","subscriptionId":"sub-a","streamId":"stream-a","seq":2,"items":[]}`,
		`{"type":"event","v":1,"event":"thread.update","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","baseSeq":1,"seq":2,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[],"pendingInteractions":[]},"patch":{"threadState":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","pendingInteractions":[]},"turnOrder":[],"turnPatches":[],"removedTurnIds":[]}}`,
		`{"type":"event","v":1,"event":"thread.update","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","baseSeq":1,"seq":2,"patch":{"threadState":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","pendingInteractions":[]},"turnOrder":["turn-a","turn-a"],"turnPatches":[],"removedTurnIds":[]}}`,
		`{"type":"event","v":1,"event":"thread.error","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","code":"UNKNOWN"}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[{"turnId":"turn-a","status":"completed","items":[{"itemId":"tool-a","role":"system","text":"工具调用","activity":{"kind":"mcpToolCall","label":"fixture/lookup","status":"completed","details":"x","truncated":false,"unsafe":true}}]}],"pendingInteractions":[]}}`,
		`{"type":"event","v":1,"event":"thread.snapshot","deviceId":"device-a","threadId":"thread-a","subscriptionId":"sub-a","streamId":"stream-a","seq":1,"thread":{"threadId":"thread-a","title":"Fixture","cwd":"/fixture","updatedAt":"2026-10-04T00:00:00Z","runtime":"idle","turns":[{"turnId":"turn-a","status":"completed","items":[{"itemId":"assistant-a","role":"assistant","text":"hello","clientMessageId":"00000000-0000-4000-8000-000000000001"}]}],"pendingInteractions":[]}}`,
		`not-json`,
	}
	invalid = append(invalid, `{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000005","deviceId":"device-a","method":"thread.name.update","params":{"threadId":"thread-a","name":"`+strings.Repeat("界", 201)+`"}}`)
	for _, raw := range invalid {
		if err := Validate([]byte(raw)); err == nil {
			t.Errorf("invalid message accepted: %s", raw)
		}
	}
}
