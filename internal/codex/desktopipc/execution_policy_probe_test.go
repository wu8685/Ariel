package desktopipc

import (
	"context"
	"encoding/json"
	"testing"
)

type policyProbeCaller struct {
	result string
	calls  []Request
}

func (c *policyProbeCaller) Call(_ context.Context, request Request) (Reply, error) {
	c.calls = append(c.calls, request)
	return Reply{Result: json.RawMessage(c.result)}, nil
}

func TestStartExecutionPolicyProbeCarriesExplicitPolicy(t *testing.T) {
	h := &policyProbeCaller{result: `{"result":{"turn":{"id":"turn-policy"}}}`}
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"latestThreadSettings":{"approvalPolicy":"never","sandboxPolicy":{"type":"dangerFullAccess"}}}`)
	turnID, err := startExecutionPolicyProbe(context.Background(), h, "owner", "thread", "/fixture", state, "client-policy")
	if err != nil || turnID != "turn-policy" || len(h.calls) != 1 {
		t.Fatalf("start policy probe: id=%q calls=%d err=%v", turnID, len(h.calls), err)
	}
	body, _ := json.Marshal(h.calls[0].Params)
	var params struct {
		TurnStart struct {
			Request struct {
				ApprovalPolicy    string `json:"approvalPolicy"`
				ApprovalsReviewer string `json:"approvalsReviewer"`
				SandboxPolicy     struct {
					Type          string   `json:"type"`
					WritableRoots []string `json:"writableRoots"`
					NetworkAccess bool     `json:"networkAccess"`
				} `json:"sandboxPolicy"`
			} `json:"request"`
			Context struct {
				Inherit bool `json:"inheritThreadSettings"`
			} `json:"context"`
		} `json:"turnStart"`
	}
	if json.Unmarshal(body, &params) != nil {
		t.Fatal("invalid policy wire")
	}
	r := params.TurnStart.Request
	if r.ApprovalPolicy != "on-request" || r.ApprovalsReviewer != "user" || r.SandboxPolicy.Type != "workspaceWrite" || len(r.SandboxPolicy.WritableRoots) != 1 || r.SandboxPolicy.WritableRoots[0] != "/fixture" || r.SandboxPolicy.NetworkAccess || !params.TurnStart.Context.Inherit {
		t.Fatalf("wrong explicit policy: %+v", params)
	}
}

func TestReadExecutionPolicyRequiresExactSettings(t *testing.T) {
	good := json.RawMessage(`{"cwd":"/fixture","latestThreadSettings":{"approvalPolicy":"on-request","sandboxPolicy":{"type":"workspaceWrite","writableRoots":["/fixture"],"networkAccess":false}}}`)
	got, err := readExecutionPolicy(good, "/fixture")
	if err != nil || !got.Enforced {
		t.Fatalf("expected enforced policy: %+v err=%v", got, err)
	}
	bad := json.RawMessage(`{"cwd":"/fixture","latestThreadSettings":{"approvalPolicy":"never","sandboxPolicy":{"type":"dangerFullAccess"}}}`)
	got, err = readExecutionPolicy(bad, "/fixture")
	if err != nil || got.Enforced {
		t.Fatalf("unsafe settings must be reported, not accepted: %+v err=%v", got, err)
	}
}
