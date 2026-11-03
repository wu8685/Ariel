package desktopipc

import (
	"context"
	"encoding/json"
	"time"
)

type ExecutionPolicyProbeResult struct {
	ApprovalPolicy  string `json:"approvalPolicy"`
	SandboxType     string `json:"sandboxType"`
	WritableRootOK  bool   `json:"writableRootOK"`
	NetworkDisabled bool   `json:"networkDisabled"`
	Enforced        bool   `json:"enforced"`
	TurnID          string `json:"-"`
}

// ProbeExecutionPolicy exercises the Desktop owner path only for a caller-guarded
// isolated fixture. It never accepts a user thread directly from the CLI.
func (c *Client) ProbeExecutionPolicy(ctx context.Context, threadID, cwd string) (out ExecutionPolicyProbeResult, err error) {
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil || r.HandledByClientID == "" {
		return out, ErrProtocol
	}
	owner := r.HandledByClientID
	params := map[string]any{"hostId": "local", "conversationId": threadID, "following": true}
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, params, []string{owner}); err != nil {
		return out, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = c.Broadcast(cleanup, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": false}, []string{owner})
	}()
	f := &fixtureFollower{c: c, o: newObservationState(threadID, owner)}
	if err := f.refresh(ctx); err != nil {
		return out, err
	}
	state, _, _ := f.o.reducer.Snapshot()
	out.TurnID, err = startExecutionPolicyProbe(ctx, f, owner, threadID, cwd, state, requestID())
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil && out.TurnID != "" {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = interruptFixtureTurn(cleanup, c, owner, threadID, out.TurnID)
		}
	}()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		if err = f.refresh(ctx); err != nil {
			return out, err
		}
		state, _, _ = f.o.reducer.Snapshot()
		if hasLiteral(state, "ARIEL_POLICY_OK") && idleFixture(state, cwd) {
			settings, readErr := readExecutionPolicy(state, cwd)
			settings.TurnID = out.TurnID
			return settings, readErr
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
}

func executionPolicyRequest(cwd, clientMessageID string) map[string]any {
	return map[string]any{
		"approvalPolicy":    "on-request",
		"approvalsReviewer": "user",
		"sandboxPolicy": map[string]any{
			"type":          "workspaceWrite",
			"writableRoots": []string{cwd},
			"networkAccess": false,
		},
		"clientUserMessageId": clientMessageID,
	}
}

func startExecutionPolicyProbe(ctx context.Context, c caller, owner, threadID, cwd string, state json.RawMessage, clientMessageID string) (string, error) {
	request := executionPolicyRequest(cwd, clientMessageID)
	return startFixtureRequest(ctx, c, owner, threadID, cwd, state, "Authorized isolated execution-policy fixture. Do not use tools, inspect files, access the network, or create tasks. Reply with exactly ARIEL_POLICY_OK.", request)
}

func readExecutionPolicy(state json.RawMessage, cwd string) (ExecutionPolicyProbeResult, error) {
	var value struct {
		CWD      string `json:"cwd"`
		Settings struct {
			ApprovalPolicy string `json:"approvalPolicy"`
			SandboxPolicy  struct {
				Type          string   `json:"type"`
				WritableRoots []string `json:"writableRoots"`
				NetworkAccess bool     `json:"networkAccess"`
			} `json:"sandboxPolicy"`
		} `json:"latestThreadSettings"`
	}
	if json.Unmarshal(state, &value) != nil || value.CWD != cwd {
		return ExecutionPolicyProbeResult{}, ErrProtocol
	}
	out := ExecutionPolicyProbeResult{
		ApprovalPolicy:  value.Settings.ApprovalPolicy,
		SandboxType:     value.Settings.SandboxPolicy.Type,
		NetworkDisabled: !value.Settings.SandboxPolicy.NetworkAccess,
	}
	for _, root := range value.Settings.SandboxPolicy.WritableRoots {
		if root == cwd {
			out.WritableRootOK = true
		}
	}
	out.Enforced = out.ApprovalPolicy == "on-request" && out.SandboxType == "workspaceWrite" && out.WritableRootOK && out.NetworkDisabled
	return out, nil
}

func ExecutionPolicyEnforced(state json.RawMessage, cwd string) bool {
	result, err := readExecutionPolicy(state, cwd)
	return err == nil && result.Enforced
}
