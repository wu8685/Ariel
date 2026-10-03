package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type PendingProbeResult struct {
	Kind    string `json:"kind"`
	Pending bool   `json:"pending"`
	TurnID  string `json:"-"`
}

// This fixture-only probe leaves an isolated interaction pending so that the
// production Web/Agent path can be tested. The CLI validates the saved fixture
// manifest and purpose before exposing this method.
func exercisePendingInteraction(ctx context.Context, h inputExperiment, owner, threadID, cwd, purpose string) (out PendingProbeResult, err error) {
	state, err := h.Snapshot(ctx)
	if err != nil || !idleFixture(state, cwd) {
		return out, ErrTurnBusy
	}
	switch purpose {
	case "command-accept":
		prompt := "Ariel isolated one-time command approval test. Request approval once for exactly /usr/bin/true using exec_command with sandbox_permissions require_escalated and no prefix rule. Do not read or write files, use the network, request other commands, or create tasks. After approval, report only the command exit status."
		out.TurnID, err = startFixtureRequest(ctx, h, owner, threadID, cwd, state, prompt, map[string]any{"approvalPolicy": "on-request", "sandboxPolicy": map[string]string{"type": "readOnly"}})
	case "user-input":
		out.TurnID, err = startInputFixtureTurn(ctx, h, owner, threadID, cwd, state, userInputFixturePrompt("ARIEL_M3_INPUT"))
	case "file-change":
		prompt := "Ariel isolated file approval test. Use apply_patch once to create only fixture-note.txt in this fixture workspace. The file must contain exactly one line: approved followed by a newline (UTF-8 bytes 61 70 70 72 6f 76 65 64 0a). The content is fully specified; do not ask a clarification question. Request approval if needed; do not use another tool, inspect files, change settings, or access the network."
		out.TurnID, err = startFixtureRequest(ctx, h, owner, threadID, cwd, state, prompt, map[string]any{"approvalPolicy": "on-request", "sandboxPolicy": map[string]string{"type": "readOnly"}})
	case "permission-request":
		prompt := "Ariel isolated permission request test. Call the request_permissions tool once, requesting only filesystem write access to the current fixture workspace for this turn; do not perform a write. This is a tool-call test, not a request for a text explanation or clarification. Do not request network access or other permissions, inspect files, run commands, or create tasks."
		approval := map[string]any{"granular": map[string]bool{"sandbox_approval": true, "rules": false, "skill_approval": false, "request_permissions": true, "mcp_elicitations": false}}
		out.TurnID, err = startFixtureRequest(ctx, h, owner, threadID, cwd, state, prompt, map[string]any{"approvalPolicy": approval, "sandboxPolicy": map[string]string{"type": "readOnly"}})
	default:
		return out, ErrProtocol
	}
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil && out.TurnID != "" {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = interruptFixtureTurn(cleanup, h, owner, threadID, out.TurnID)
		}
	}()
	for {
		state, err = h.Snapshot(ctx)
		if err != nil {
			return out, err
		}
		if purpose == "command-accept" {
			var pending commandRequest
			pending, err = pendingCommandForDecision(state, threadID, out.TurnID, cwd, "accept")
			if err == nil && !strings.Contains(pending.Params.Command, "/usr/bin/true") {
				return out, ErrProtocol
			}
			out.Kind = "command_approval"
		} else if purpose == "user-input" {
			_, err = pendingUserInput(state, threadID, out.TurnID, cwd)
			out.Kind = "user_input"
		} else {
			var snapshot struct {
				CWD      string            `json:"cwd"`
				Requests []json.RawMessage `json:"requests"`
			}
			if json.Unmarshal(state, &snapshot) != nil || snapshot.CWD != cwd || snapshot.Requests == nil {
				return out, ErrProtocol
			}
			err = errNoPendingInput
			if len(snapshot.Requests) > 0 {
				var req struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
					Params struct {
						ThreadID string `json:"threadId"`
						TurnID   string `json:"turnId"`
						ItemID   string `json:"itemId"`
					} `json:"params"`
				}
				method := "item/fileChange/requestApproval"
				out.Kind = "file_change"
				if purpose == "permission-request" {
					method = "item/permissions/requestApproval"
					out.Kind = "permissions"
				}
				if len(snapshot.Requests) != 1 || json.Unmarshal(snapshot.Requests[0], &req) != nil || !validRequestID(req.ID) || req.Method != method || req.Params.ThreadID != threadID || req.Params.TurnID != out.TurnID || req.Params.ItemID == "" {
					return out, ErrProtocol
				}
				err = nil
			}
		}
		if err == nil {
			out.Pending = true
			return out, nil
		}
		if !errors.Is(err, errNoPendingInput) {
			return out, err
		}
		if idleFixture(state, cwd) {
			return out, errors.New("fixture ended without requested interaction")
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
}

func (c *Client) ProbePendingInteraction(ctx context.Context, threadID, cwd, purpose string) (PendingProbeResult, error) {
	if purpose != "command-accept" && purpose != "user-input" && purpose != "file-change" && purpose != "permission-request" {
		return PendingProbeResult{}, ErrProtocol
	}
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil || r.HandledByClientID == "" {
		return PendingProbeResult{}, ErrProtocol
	}
	owner := r.HandledByClientID
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": true}, []string{owner}); err != nil {
		return PendingProbeResult{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = c.Broadcast(cleanup, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": false}, []string{owner})
	}()
	h := &fixtureFollower{c: c, o: newObservationState(threadID, owner)}
	return exercisePendingInteraction(ctx, h, owner, threadID, cwd, purpose)
}
