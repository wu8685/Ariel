package relay

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAsyncQuestionNegotiationSurvivesEarlySnapshotAndKeepsLegacyWebClean(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	agent := dialTest(t, server.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	web := dialTest(t, server.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000012", "deviceId": "mock-mac", "method": "thread.cache.watch", "params": map[string]any{"threadId": "thread", "asyncQuestionsVersion": 1}})
	readJSON(t, web)
	touch := readJSON(t, agent)
	if touch["params"].(map[string]any)["asyncQuestionsVersion"] != nil {
		t.Fatal("new fields sent before Agent receipt")
	}
	sendJSON(t, agent, cacheSnapshotEvent("thread", "epoch-a", "stream", 1))
	readJSON(t, web)
	sendJSON(t, agent, map[string]any{"type": "response", "v": 1, "requestId": touch["requestId"], "outcome": "accepted", "data": map[string]any{"asyncQuestionsVersion": 1, "asyncQuestionWrite": false}})
	optIn := readJSON(t, agent)
	if optIn["method"] != "thread.cache.touch" || optIn["params"].(map[string]any)["asyncQuestionsVersion"] != float64(1) {
		t.Fatalf("late correlated receipt failed negotiation: %v", optIn)
	}
	event := cacheSnapshotEvent("thread", "epoch-a", "stream", 2)
	event["asyncQuestions"] = map[string]any{"version": 1, "writable": false, "questions": []any{}, "warnings": []any{}}
	sendJSON(t, agent, event)
	update := readJSON(t, web)
	if update["asyncQuestions"] == nil {
		t.Fatal("opted-in Web lost negotiated sidecar")
	}
	legacy := dialTest(t, server.URL, testOrigin)
	sendJSON(t, legacy, webHello())
	readJSON(t, legacy)
	sendJSON(t, legacy, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000013", "deviceId": "mock-mac", "method": "thread.cache.watch", "params": map[string]any{"threadId": "thread"}})
	response := readJSON(t, legacy)
	if response["data"].(map[string]any)["asyncQuestions"] != nil {
		t.Fatal("sidecar leaked into legacy watch response")
	}
	event["seq"] = 3
	sendJSON(t, agent, event)
	readJSON(t, web)
	if update := readJSON(t, legacy); update["asyncQuestions"] != nil {
		t.Fatal("sidecar leaked into strict legacy event")
	}
}

func TestAsyncQuestionCapabilityReceiptCannotCrossAgentOrRequest(t *testing.T) {
	for _, mode := range []string{"replaced", "unmatched", "legacy-web", "legacy-agent"} {
		t.Run(mode, func(t *testing.T) {
			r, agent, _, key := cacheDeliveryFixture(t)
			watcher := r.cache.entries[key].watchers["watch"]
			watcher.questions = mode != "legacy-web"
			r.cache.entries[key].watchers["watch"] = watcher
			agent.questionTouches = map[string]questionTouch{"request": {key: key, watching: true}}
			if mode == "replaced" {
				r.agents[agent.deviceID] = &peer{deviceID: agent.deviceID, agentEpoch: agent.agentEpoch}
			}
			id := "request"
			if mode == "unmatched" {
				id = "unknown"
			}
			data := map[string]any{}
			if mode != "legacy-agent" {
				data["asyncQuestionsVersion"] = float64(1)
			}
			r.negotiateQuestionResponse(context.Background(), agent, id, map[string]any{"outcome": "accepted", "data": data})
			if agent.asyncQuestions {
				t.Fatal("untrusted or unrequested capability enabled")
			}
		})
	}
}

func TestAsyncQuestionHistoryDoesNotSendOptInToLegacyAgent(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	agent := dialTest(t, server.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	web := dialTest(t, server.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000021", "deviceId": "mock-mac", "method": "thread.history", "params": map[string]any{"threadId": "thread", "limit": 3, "asyncQuestionsVersion": 1}})
	forward := readJSON(t, agent)
	if forward["method"] != "thread.history" || forward["params"].(map[string]any)["asyncQuestionsVersion"] != nil {
		t.Fatal("old strict Agent received new extension")
	}
	sendJSON(t, agent, map[string]any{"type": "response", "v": 1, "requestId": forward["requestId"], "outcome": "accepted", "data": map[string]any{"turns": []any{}, "nextCursor": ""}})
	if reply := readJSON(t, web); reply["outcome"] != "accepted" {
		t.Fatal("legacy history no longer usable")
	}
}

func TestAsyncQuestionSidecarIsAtomicBudgetedAndOptInOnly(t *testing.T) {
	r, agent, web, key := cacheDeliveryFixture(t)
	agent.asyncQuestions = true
	entry := cacheFixture(key.threadID, 1)
	entry.deviceID = agent.deviceID
	entry.agentEpoch = agent.agentEpoch
	entry.questions = json.RawMessage(`{"version":1,"writable":false,"questions":[],"warnings":[]}`)
	if err := r.cache.put(entry, time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.cache.totalBytes != entry.bytes+int64(len(entry.questions))+cacheEntryMetadataBytes {
		t.Fatal("sidecar missing from memory budget")
	}
	for _, optIn := range []bool{false, true} {
		watcher := r.cache.entries[key].watchers["watch"]
		watcher.questions = optIn
		r.cache.entries[key].watchers["watch"] = watcher
		messages := r.cacheUpdateMessagesLocked(key)
		_, found := messages[0].message["asyncQuestions"]
		if found != optIn {
			t.Fatal("sidecar leaked to legacy Web or lost for opted-in Web")
		}
	}
	entry.seq++
	entry.questions = nil
	if err := r.cache.put(entry, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(r.cache.entries[key].questions) != 0 {
		t.Fatal("rotated question sidecar survived replacement")
	}
	_ = web
}

func TestAsyncQuestionInvalidSidecarDoesNotInvalidateConversation(t *testing.T) {
	r, agent, _, key := cacheDeliveryFixture(t)
	agent.asyncQuestions = true
	event := cacheSnapshotEvent(key.threadID, agent.agentEpoch, "stream", 1)
	event["asyncQuestions"] = map[string]any{"version": 1, "questions": "invalid"}
	applyCacheTestEvent(t, r, agent, event)
	if r.cache.entries[key] == nil || r.cache.entries[key].stale || len(r.cache.entries[key].thread) == 0 {
		t.Fatal("bad optional sidecar broke base conversation")
	}
	var fallback struct {
		Warnings  []any
		Questions []any
	}
	if json.Unmarshal(r.cache.entries[key].questions, &fallback) != nil || len(fallback.Questions) != 0 || len(fallback.Warnings) != 1 {
		t.Fatal("invalid sidecar was not replaced by local warning")
	}
}
