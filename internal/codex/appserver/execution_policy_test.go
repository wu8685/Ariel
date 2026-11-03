package appserver

import "testing"

func TestPuckExecutionPolicyParamsAreExplicitAndWorkspaceBound(t *testing.T) {
	params := PuckExecutionPolicyParams("/fixture")
	if params["approvalPolicy"] != "on-request" || params["approvalsReviewer"] != "user" || params["sandbox"] != "workspace-write" {
		t.Fatalf("thread params=%+v", params)
	}
	policy := params["sandboxPolicy"].(map[string]any)
	roots := policy["writableRoots"].([]string)
	if policy["type"] != "workspaceWrite" || policy["networkAccess"] != false || len(roots) != 1 || roots[0] != "/fixture" {
		t.Fatalf("sandbox policy=%+v", policy)
	}
}
