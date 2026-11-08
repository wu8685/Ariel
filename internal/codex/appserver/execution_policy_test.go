package appserver

import "testing"

func TestPuckExecutionPolicyParamsSupportPersistentModes(t *testing.T) {
	params := PuckExecutionPolicyParams("ask", "/fixture")
	if params["approvalPolicy"] != "on-request" || params["approvalsReviewer"] != "user" || params["sandbox"] != "workspace-write" {
		t.Fatalf("thread params=%+v", params)
	}
	policy := params["sandboxPolicy"].(map[string]any)
	roots := policy["writableRoots"].([]string)
	if policy["type"] != "workspaceWrite" || policy["networkAccess"] != false || len(roots) != 1 || roots[0] != "/fixture" {
		t.Fatalf("sandbox policy=%+v", policy)
	}
	full := PuckExecutionPolicyParams("full", "/fixture")
	if full["approvalPolicy"] != "never" || full["sandbox"] != "danger-full-access" {
		t.Fatalf("full thread params=%+v", full)
	}
	if _, exists := full["approvalsReviewer"]; exists {
		t.Fatalf("full policy must not select a human reviewer: %+v", full)
	}
	if policy := full["sandboxPolicy"].(map[string]any); policy["type"] != "dangerFullAccess" {
		t.Fatalf("full sandbox policy=%+v", policy)
	}
}
