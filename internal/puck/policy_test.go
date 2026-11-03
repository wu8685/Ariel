package puck

import (
	"path/filepath"
	"testing"
)

func TestPolicyEngineClassifiesP0CriticalActions(t *testing.T) {
	root := t.TempDir()
	base := PolicyInput{Tool: "start_task", DeviceID: "exec-a", CWD: filepath.Join(root, "work"), AllowedScope: map[string][]string{"exec-a": {root}}, ExecutionPolicyEnforceable: true}
	tests := []struct {
		name   string
		mutate func(*PolicyInput)
		want   string
	}{
		{"allowed", func(*PolicyInput) {}, ""},
		{"B1-device", func(in *PolicyInput) { in.DeviceID = "exec-b" }, "B1"},
		{"B1-path", func(in *PolicyInput) { in.CWD = filepath.Join(root, "..", "outside") }, "B1"},
		{"B2", func(in *PolicyInput) { in.Tool = "send_turn"; in.OwnedThread = false }, "B2"},
		{"B3", func(in *PolicyInput) { in.Tool = "interrupt"; in.OwnedThread = true; in.TurnOrigin = "user" }, "B3"},
		{"B5", func(in *PolicyInput) { in.RiskTags = []string{"deploy"} }, "B5"},
		{"B6", func(in *PolicyInput) { in.Prompt = "please git push origin main" }, "B6"},
		{"B10", func(in *PolicyInput) { in.ExecutionPolicyEnforceable = false }, "B10"},
		{"self-workspace", func(in *PolicyInput) {
			in.CWD = filepath.Join(root, ".ariel", "puck", "assistant")
			in.AssistantWorkspaceRoot = filepath.Join(root, ".ariel", "puck")
		}, "PUCK_SELF_TARGET"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			input.RiskTags = nil
			tc.mutate(&input)
			decision := EvaluatePolicy(input)
			if tc.want == "" {
				if decision.RequiresAuthorization || len(decision.Rules) != 0 {
					t.Fatalf("decision=%+v", decision)
				}
				return
			}
			if tc.want == "PUCK_SELF_TARGET" {
				if decision.DeniedCode != tc.want {
					t.Fatalf("decision=%+v", decision)
				}
				return
			}
			if !decision.RequiresAuthorization || !contains(decision.Rules, tc.want) {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func TestNativeInteractionsAlwaysRequireUserDecision(t *testing.T) {
	for _, kind := range []string{"command_approval", "file_approval", "permission_request", "user_input", "unsupported"} {
		if !NativeInteractionRequiresUser(kind) {
			t.Fatalf("%s was made optional", kind)
		}
	}
	if NativeInteractionRequiresUser("completed") {
		t.Fatal("non-interaction classified as user decision")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
