package desktopagent

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAsyncQuestionsAreInternalAtomicSidecarNotLegacyThreadFields(t *testing.T) {
	raw := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"inProgress"},"requests":[],"turns":[{"turnId":"turn","status":"inProgress","items":[{"type":"agentMessage","id":"message","text":"Continuing","questions":[{"title":"Which color?","options":["Blue","Green"]}]}]}]}`)
	thread, err := normalizeLiveQuestions("thread", "Fixture", "/fixture", raw)
	if err != nil || thread["_asyncQuestions"] == nil {
		t.Fatalf("native question lost: %v", err)
	}
	wire := withoutAsyncQuestions(thread)
	if wire["_asyncQuestions"] != nil || wire["pendingInteractions"] == nil || thread["_asyncQuestions"] == nil {
		t.Fatal("leaked sidecar or mutated baseline")
	}
	changed := map[string]any{}
	for k, v := range thread {
		changed[k] = v
	}
	changed["_asyncQuestions"] = "changed"
	if cacheStructure(thread) == cacheStructure(changed) {
		t.Fatal("async change delayed behind ordinary text cadence")
	}
	if !reflect.DeepEqual(thread["turns"], wire["turns"]) {
		t.Fatal("question stripped text")
	}
}
