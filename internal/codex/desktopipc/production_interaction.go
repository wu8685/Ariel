package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrStaleInteraction = errors.New("Desktop interaction is no longer current")

type InteractionReceipt struct {
	Kind          string
	TurnID        string
	Decision      string
	input         inputRequest
	command       commandRequest
	fileID        string
	fileReq       json.RawMessage
	permissionReq json.RawMessage
	answers       map[string][]string
}

// SubmitProductionInteraction checks the complete pending request immediately
// before submission. Its receipt is not acceptance; Confirmed requires a new
// owner state and, for user input, the exact answers echoed by that owner.
func SubmitProductionInteraction(ctx context.Context, c caller, owner, threadID, cwd string, state json.RawMessage, interactionID, decision string, answers map[string][]string) (InteractionReceipt, error) {
	var receipt InteractionReceipt
	var snapshot struct {
		CWD      string            `json:"cwd"`
		Requests []json.RawMessage `json:"requests"`
	}
	if owner == "" || threadID == "" || cwd == "" || json.Unmarshal(state, &snapshot) != nil || snapshot.CWD != cwd || len(snapshot.Requests) != 1 {
		return receipt, ErrStaleInteraction
	}
	var request struct {
		Method string `json:"method"`
		Params struct {
			ThreadID  string  `json:"threadId"`
			TurnID    string  `json:"turnId"`
			ItemID    string  `json:"itemId"`
			GrantRoot *string `json:"grantRoot"`
		} `json:"params"`
	}
	if json.Unmarshal(snapshot.Requests[0], &request) != nil || request.Params.ThreadID != threadID || request.Params.TurnID == "" {
		return receipt, ErrStaleInteraction
	}
	id, err := InteractionID(snapshot.Requests[0])
	var fileItem json.RawMessage
	if request.Method == "item/fileChange/requestApproval" {
		fileItem, err = FileChangeItem(state, request.Params.TurnID, request.Params.ItemID)
		if err == nil {
			id, err = InteractionIDWithContext(snapshot.Requests[0], fileItem)
		}
	}
	if err != nil || id != interactionID {
		return receipt, ErrStaleInteraction
	}
	status, known := nativeTurnStatus(state, request.Params.TurnID)
	if !known || status != "inProgress" {
		return receipt, ErrStaleInteraction
	}
	receipt.TurnID = request.Params.TurnID
	receipt.Decision = decision
	switch request.Method {
	case "item/tool/requestUserInput":
		if decision != "answer" {
			return InteractionReceipt{}, ErrStaleInteraction
		}
		pending, err := pendingUserInput(state, threadID, receipt.TurnID, cwd)
		if err != nil {
			return InteractionReceipt{}, ErrStaleInteraction
		}
		if err := submitUserInput(ctx, c, owner, cwd, state, pending, answers); err != nil {
			if errors.Is(err, errInvalidInput) {
				return InteractionReceipt{}, ErrStaleInteraction
			}
			return InteractionReceipt{}, submittedError(err)
		}
		receipt.Kind, receipt.input, receipt.answers = "user_input", pending, answers
	case "item/commandExecution/requestApproval":
		nativeDecision := ""
		switch decision {
		case "deny":
			nativeDecision = "decline"
		case "deny_and_stop":
			nativeDecision = "cancel"
		case "accept_once":
			nativeDecision = "accept"
		default:
			return InteractionReceipt{}, ErrStaleInteraction
		}
		pending, err := pendingCommandForDecision(state, threadID, receipt.TurnID, cwd, nativeDecision)
		if err != nil {
			return InteractionReceipt{}, ErrStaleInteraction
		}
		var submitErr error
		if nativeDecision == "accept" {
			submitErr = acceptCommandOnce(ctx, c, owner, cwd, state, pending)
		} else {
			submitErr = respondCommandDecision(ctx, c, owner, cwd, state, pending, nativeDecision)
		}
		if err := submitErr; err != nil {
			if errors.Is(err, errInvalidApproval) {
				return InteractionReceipt{}, ErrStaleInteraction
			}
			return InteractionReceipt{}, submittedError(err)
		}
		receipt.Kind, receipt.command = "command_approval", pending
	case "item/fileChange/requestApproval":
		nativeDecision := ""
		switch decision {
		case "accept_once":
			if request.Params.GrantRoot != nil {
				return InteractionReceipt{}, ErrStaleInteraction
			}
			nativeDecision = "accept"
		case "deny":
			nativeDecision = "decline"
		case "deny_and_stop":
			nativeDecision = "cancel"
		default:
			return InteractionReceipt{}, ErrStaleInteraction
		}
		var item struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(fileItem, &item) != nil || item.Status != "inProgress" || request.Params.ItemID == "" {
			return InteractionReceipt{}, ErrStaleInteraction
		}
		var nativeRequest struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(snapshot.Requests[0], &nativeRequest) != nil {
			return InteractionReceipt{}, ErrStaleInteraction
		}
		r, err := c.Call(ctx, Request{Method: "thread-follower-file-approval-decision", Version: 1, TargetClientID: owner, Mutating: true, Params: map[string]any{"conversationId": threadID, "requestId": nativeRequest.ID, "decision": nativeDecision}})
		if err != nil {
			return InteractionReceipt{}, err
		}
		var nativeReceipt struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal(r.Result, &nativeReceipt) != nil || !nativeReceipt.OK {
			return InteractionReceipt{}, &CallError{Cause: ErrProtocol, Outcome: "unknown"}
		}
		receipt.Kind, receipt.fileID, receipt.fileReq = "file_approval", request.Params.ItemID, nativeRequest.ID
	case "item/permissions/requestApproval":
		pending, err := ParsePermissionRequest(snapshot.Requests[0], threadID, cwd)
		if err != nil {
			return InteractionReceipt{}, ErrStaleInteraction
		}
		grant := map[string]json.RawMessage{}
		switch decision {
		case "deny":
		case "accept_once":
			if !pending.Grantable {
				return InteractionReceipt{}, ErrStaleInteraction
			}
			grant = pending.grant
		default:
			return InteractionReceipt{}, ErrStaleInteraction
		}
		r, err := c.Call(ctx, Request{Method: "thread-follower-permissions-request-approval-response", Version: 1, TargetClientID: owner, Mutating: true, Params: map[string]any{"conversationId": threadID, "requestId": pending.ID, "response": map[string]any{"permissions": grant, "scope": "turn"}}})
		if err != nil {
			return InteractionReceipt{}, submittedError(err)
		}
		var nativeReceipt struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal(r.Result, &nativeReceipt) != nil || !nativeReceipt.OK {
			return InteractionReceipt{}, &CallError{Cause: ErrProtocol, Outcome: "unknown"}
		}
		receipt.Kind, receipt.permissionReq = "permission_request", pending.ID
	default:
		return InteractionReceipt{}, errors.New("INTERACTION_UNSUPPORTED")
	}
	return receipt, nil
}

func submittedError(err error) error {
	var call *CallError
	if errors.As(err, &call) {
		return err
	}
	return &CallError{Cause: err, Outcome: "unknown"}
}

func (r InteractionReceipt) Confirmed(state json.RawMessage) bool {
	if r.Kind == "user_input" {
		return userInputEchoMatches(state, r.input, r.answers)
	}
	if r.Kind == "file_approval" {
		var snapshot struct {
			Requests []struct {
				ID json.RawMessage `json:"id"`
			} `json:"requests"`
		}
		if json.Unmarshal(state, &snapshot) != nil || snapshot.Requests == nil {
			return false
		}
		for _, pending := range snapshot.Requests {
			if sameJSON(pending.ID, r.fileReq) {
				return false
			}
		}
		status, known := nativeTurnStatus(state, r.TurnID)
		if !known {
			return false
		}
		if r.Decision == "deny_and_stop" {
			return status == "interrupted"
		}
		if r.Decision == "deny" {
			return status == "completed" || status == "failed" || status == "interrupted"
		}
		item, err := FileChangeItem(state, r.TurnID, r.fileID)
		if err != nil {
			return false
		}
		var applied struct {
			Status string `json:"status"`
		}
		return json.Unmarshal(item, &applied) == nil && applied.Status == "completed"
	}
	if r.Kind == "permission_request" {
		var snapshot struct {
			Requests []struct {
				ID json.RawMessage `json:"id"`
			} `json:"requests"`
		}
		if json.Unmarshal(state, &snapshot) != nil || snapshot.Requests == nil {
			return false
		}
		for _, pending := range snapshot.Requests {
			if sameJSON(pending.ID, r.permissionReq) {
				return false
			}
		}
		status, known := nativeTurnStatus(state, r.TurnID)
		return known && (status == "inProgress" || status == "completed" || status == "failed" || status == "interrupted")
	}
	if r.Kind != "command_approval" {
		return false
	}
	var snapshot struct {
		Requests []struct {
			ID json.RawMessage `json:"id"`
		} `json:"requests"`
	}
	if json.Unmarshal(state, &snapshot) != nil || snapshot.Requests == nil {
		return false
	}
	for _, request := range snapshot.Requests {
		if sameJSON(request.ID, r.command.ID) {
			return false
		}
	}
	status, known := nativeTurnStatus(state, r.TurnID)
	if !known {
		return false
	}
	if r.Decision == "deny_and_stop" {
		return status == "interrupted"
	}
	return status == "inProgress" || status == "completed"
}
