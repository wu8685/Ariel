package appserver

import "github.com/wu8685/Ariel/internal/modelsettings"

// ExecutionParams is deliberately allowlisted: no permissions or mode fields.
func ExecutionParams(v modelsettings.Values) map[string]any {
	var tier any
	switch v.Speed.Mode {
	case "standard":
		tier = "default"
	case "tier":
		tier = v.Speed.TierID
	}
	var effort any
	if v.Effort != "" {
		effort = v.Effort
	}
	return map[string]any{"model": v.Model, "effort": effort, "serviceTier": tier}
}

// PuckExecutionPolicyParams is the single allowlisted policy shape used for
// Puck-created execution threads. Callers select `sandbox` for thread/start
// and `sandboxPolicy` for every turn/start.
func PuckExecutionPolicyParams(mode, cwd string) map[string]any {
	if mode == "full" {
		return map[string]any{
			"approvalPolicy": "never",
			"sandbox":        "danger-full-access",
			"sandboxPolicy":  map[string]any{"type": "dangerFullAccess"},
		}
	}
	return map[string]any{
		"approvalPolicy":    "on-request",
		"approvalsReviewer": "user",
		"sandbox":           "workspace-write",
		"sandboxPolicy": map[string]any{
			"type":          "workspaceWrite",
			"writableRoots": []string{cwd},
			"networkAccess": false,
		},
	}
}
