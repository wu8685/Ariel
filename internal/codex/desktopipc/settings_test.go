package desktopipc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wu8685/Ariel/internal/modelsettings"
)

func TestNativeSettingsReadAndWriteDoNotChangePermissionsOrMode(t *testing.T) {
	raw := json.RawMessage(`{"cwd":"/fixture","latestModel":"old","latestReasoningEffort":"low","latestThreadSettings":{"model":"next","effort":"high","serviceTier":"priority"},"latestCollaborationMode":{"mode":"plan","settings":{"model":"old","reasoning_effort":"low","developer_instructions":"keep"}}}`)
	current, err := ExecutionSettings(raw)
	if err != nil || current.Model != "next" || current.Effort != "high" || current.Speed.TierID != "priority" {
		t.Fatalf("settings=%+v err=%v", current, err)
	}
	owner := &recordingOwner{reply: Reply{Result: json.RawMessage(`{"applied":true}`)}}
	desired := modelsettings.Values{Model: "new", Effort: "xhigh", Speed: modelsettings.Speed{Mode: "standard"}}
	if err := UpdateExecutionSettings(context.Background(), owner, "owner", "thread", "/fixture", raw, desired); err != nil {
		t.Fatal(err)
	}
	r := owner.calls[0]
	if r.Method != "thread-follower-update-thread-settings" || r.Version != 2 || !r.Mutating {
		t.Fatalf("request=%+v", r)
	}
	b, _ := json.Marshal(r.Params)
	var p map[string]json.RawMessage
	_ = json.Unmarshal(b, &p)
	if _, ok := p["activeTurnId"]; ok {
		t.Fatal("active turn changed")
	}
	var settings map[string]any
	_ = json.Unmarshal(p["threadSettings"], &settings)
	if len(settings) != 3 || settings["model"] != "new" || settings["effort"] != "xhigh" || settings["serviceTier"] != "default" {
		t.Fatalf("native patch=%s", p["threadSettings"])
	}
}

func TestNativeSettingsMalformedReceiptIsUnknown(t *testing.T) {
	owner := &recordingOwner{reply: Reply{Result: json.RawMessage(`{"ok":true}`)}}
	err := UpdateExecutionSettings(context.Background(), owner, "owner", "thread", "/fixture", json.RawMessage(`{"cwd":"/fixture","latestModel":"m"}`), modelsettings.Values{Model: "m", Speed: modelsettings.Speed{Mode: "inherit"}})
	if err == nil {
		t.Fatal("transport ok falsely confirmed")
	}
}

func TestNativeSettingsConditionPreservesNullEffort(t *testing.T) {
	owner := &recordingOwner{reply: Reply{Result: json.RawMessage(`{"applied":true}`)}}
	if err := UpdateExecutionSettings(context.Background(), owner, "owner", "thread", "/fixture", json.RawMessage(`{"cwd":"/fixture","latestModel":"m","latestReasoningEffort":null}`), modelsettings.Values{Model: "m", Speed: modelsettings.Speed{Mode: "inherit"}}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(owner.calls[0].Params)
	var p struct {
		Condition map[string]any `json:"condition"`
	}
	_ = json.Unmarshal(body, &p)
	if p.Condition["ifEffortEquals"] != nil {
		t.Fatal("null effort changed to empty string, native CAS will always reject")
	}
}
func TestNativeSettingsExplicitNullClearsInheritedEffort(t *testing.T) {
	v, err := ExecutionSettings(json.RawMessage(`{"latestModel":"m","latestReasoningEffort":"high","latestThreadSettings":{"effort":null}}`))
	if err != nil || v.Effort != "" {
		t.Fatalf("null effort was not cleared: %+v %v", v, err)
	}
}

func TestReducerKeepsOnlyBoundedSettingsMetadata(t *testing.T) {
	r := NewReducer(4 << 20)
	state := `{"latestModel":"m","latestReasoningEffort":"high","latestThreadSettings":{"model":"m","effort":"high","serviceTier":null,"sandboxPolicy":{"type":"dangerFullAccess"}},"latestCollaborationMode":{"mode":"plan","settings":{"model":"m","reasoning_effort":"high","developer_instructions":"` + strings.Repeat("x", 1<<20) + `"}},"turns":[{"text":"` + strings.Repeat("y", 1<<20) + `"}]}`
	if err := r.Apply(json.RawMessage(`{"type":"snapshot","revision":1,"conversationState":` + state + `}`)); err != nil {
		t.Fatal(err)
	}
	metadata := r.ExecutionMetadata()
	if len(metadata) > 4096 || strings.Contains(string(metadata), "sandboxPolicy") || strings.Contains(string(metadata), "developer_instructions") || strings.Contains(string(metadata), "turns") {
		t.Fatal("settings projection leaked history or unrelated settings")
	}
	v, err := ExecutionSettings(metadata)
	if err != nil || v.Model != "m" || v.Effort != "high" {
		t.Fatal("metadata missing", err)
	}
}

func TestNativeSettingsProfileOnlyEnablesVerifiedCombination(t *testing.T) {
	if !SettingsProfileSupported("26.930.51102", "0.160.0") {
		t.Fatal("verified profile disabled")
	}
	for _, versions := range [][2]string{{"26.930.31730", "0.160.0"}, {"99.1.1", "0.160.0"}, {"26.930.51102", "99.1.1"}} {
		if SettingsProfileSupported(versions[0], versions[1]) {
			t.Fatal("unverified settings write enabled")
		}
	}
}
