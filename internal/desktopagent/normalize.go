package desktopagent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

var ErrNativeShape = errors.New("Desktop history or live state has unsupported structure")

const recentTurnLimit = 10

func NormalizeStored(source appserver.Thread) (map[string]any, error) {
	if source.ID == "" || source.CWD == "" {
		return nil, ErrNativeShape
	}
	var status struct {
		Type string `json:"type"`
	}
	if len(source.Status) > 0 && json.Unmarshal(source.Status, &status) != nil {
		return nil, ErrNativeShape
	}
	runtime := normalizeRuntime(status.Type)
	turns := make([]any, 0, len(source.Turns))
	for _, native := range source.Turns {
		turn, err := normalizeTurn(native.ID, native.Status, native.Items)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	updated := time.Unix(source.UpdatedAt, 0).UTC()
	if source.UpdatedAt == 0 {
		updated = time.Now().UTC()
	}
	return map[string]any{"threadId": source.ID, "title": source.Name, "cwd": source.CWD, "updatedAt": updated.Format(time.RFC3339Nano), "runtime": runtime, "turns": turns, "pendingInteractions": []any{}, "permissions": map[string]any{"sandbox": "unknown", "approval": "unknown"}}, nil
}

func NormalizeLive(threadID, title, cwd string, state json.RawMessage) (map[string]any, error) {
	if threadID == "" || cwd == "" || len(state) == 0 {
		return nil, ErrNativeShape
	}
	var native struct {
		CWD     string `json:"cwd"`
		Runtime struct {
			Type string `json:"type"`
		} `json:"threadRuntimeStatus"`
		Requests             []json.RawMessage `json:"requests"`
		LatestThreadSettings struct {
			ApprovalPolicy string `json:"approvalPolicy"`
			SandboxPolicy  struct {
				Type string `json:"type"`
			} `json:"sandboxPolicy"`
		} `json:"latestThreadSettings"`
		Turns []struct {
			TurnID string            `json:"turnId"`
			Status string            `json:"status"`
			Items  []json.RawMessage `json:"items"`
		} `json:"turns"`
		TurnHistory struct {
			Kind    string `json:"kind"`
			History struct {
				Islands []struct {
					Entries []struct {
						Value string `json:"value"`
					} `json:"entries"`
				} `json:"islands"`
				Entities map[string]struct {
					TurnID string            `json:"turnId"`
					Status string            `json:"status"`
					Items  []json.RawMessage `json:"items"`
				} `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(state, &native) != nil || native.CWD != cwd || native.Requests == nil || native.Runtime.Type == "" {
		return nil, ErrNativeShape
	}
	type nativeTurn struct {
		id, status string
		items      []json.RawMessage
	}
	allTurns := make([]nativeTurn, 0)
	if native.TurnHistory.Kind == "canonical" {
		for _, island := range native.TurnHistory.History.Islands {
			for _, entry := range island.Entries {
				t, ok := native.TurnHistory.History.Entities[entry.Value]
				if !ok || t.TurnID == "" {
					return nil, ErrNativeShape
				}
				allTurns = append(allTurns, nativeTurn{t.TurnID, t.Status, t.Items})
			}
		}
	} else if native.TurnHistory.Kind == "" {
		for _, t := range native.Turns {
			if t.TurnID == "" {
				return nil, ErrNativeShape
			}
			allTurns = append(allTurns, nativeTurn{t.TurnID, t.Status, t.Items})
		}
	} else {
		return nil, ErrNativeShape
	}
	start := max(0, len(allTurns)-recentTurnLimit)
	turns := make([]any, 0, len(allTurns)-start)
	for _, t := range allTurns[start:] {
		turn, err := normalizeTurn(t.id, t.status, t.items)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	interactions := make([]any, 0, len(native.Requests))
	for _, raw := range native.Requests {
		card, err := normalizeInteraction(threadID, cwd, state, raw)
		if err != nil {
			return nil, err
		}
		interactions = append(interactions, card)
	}
	return map[string]any{"threadId": threadID, "title": title, "cwd": cwd, "updatedAt": time.Now().UTC().Format(time.RFC3339Nano), "runtime": normalizeRuntime(native.Runtime.Type), "turns": turns, "pendingInteractions": interactions, "permissions": normalizePermissions(native.LatestThreadSettings.SandboxPolicy.Type, native.LatestThreadSettings.ApprovalPolicy), "historyComplete": start == 0, "recentComplete": true}, nil
}

func normalizePermissions(sandbox, approval string) map[string]any {
	switch sandbox {
	case "readOnly", "read-only":
		sandbox = "read_only"
	case "workspaceWrite", "workspace-write":
		sandbox = "workspace_write"
	case "dangerFullAccess", "danger-full-access":
		sandbox = "full_access"
	default:
		sandbox = "unknown"
	}
	switch approval {
	case "on-request":
		approval = "on_request"
	case "never":
	default:
		approval = "unknown"
	}
	return map[string]any{"sandbox": sandbox, "approval": approval}
}

func normalizeRuntime(native string) string {
	switch native {
	case "idle":
		return "idle"
	case "active", "inProgress":
		return "inProgress"
	case "notLoaded":
		return "notLoaded"
	default:
		return "unknown"
	}
}

func normalizeTurn(id, status string, rawItems []json.RawMessage) (map[string]any, error) {
	if id == "" {
		return nil, ErrNativeShape
	}
	switch status {
	case "inProgress", "completed", "failed", "interrupted":
	default:
		return nil, ErrNativeShape
	}
	items := make([]any, 0, len(rawItems))
	for _, raw := range rawItems {
		var item struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Text    string `json:"text"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Command   string `json:"command"`
			Status    string `json:"status"`
			Completed *bool  `json:"completed"`
			Changes   []struct {
				Path string `json:"path"`
				Kind struct {
					Type string `json:"type"`
				} `json:"kind"`
			} `json:"changes"`
		}
		if json.Unmarshal(raw, &item) != nil || item.ID == "" || item.Type == "" {
			return nil, ErrNativeShape
		}
		role, text := "system", ""
		switch item.Type {
		case "userMessage":
			role = "user"
			parts := make([]string, 0, len(item.Content))
			for _, c := range item.Content {
				if c.Type == "text" {
					parts = append(parts, c.Text)
				} else {
					parts = append(parts, "[非文本输入: "+c.Type+"]")
				}
			}
			text = strings.Join(parts, "\n")
		case "agentMessage":
			role = "assistant"
			text = item.Text
		case "plan":
			text = item.Text
		case "reasoning":
			continue
		case "commandExecution":
			text = "命令: " + item.Command
			if item.Status != "" {
				text += "\n状态: " + item.Status
			}
		case "fileChange":
			paths := make([]string, 0, len(item.Changes))
			for _, change := range item.Changes {
				if change.Path != "" {
					paths = append(paths, change.Kind.Type+": "+change.Path)
				}
			}
			text = "文件变更: " + strings.Join(paths, "\n")
			if item.Status != "" {
				text += "\n状态: " + item.Status
			}
		case "userInputResponse":
			switch {
			case item.Completed == nil:
				text = "补充回答状态未知"
			case *item.Completed:
				text = "已回答补充问题"
			default:
				text = "等待补充回答"
			}
		default:
			text = "[" + item.Type + " 项目]"
		}
		items = append(items, map[string]any{"itemId": item.ID, "role": role, "text": text})
	}
	return map[string]any{"turnId": id, "status": status, "items": items}, nil
}

func normalizeInteraction(threadID, cwd string, state, raw json.RawMessage) (map[string]any, error) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ThreadID  string            `json:"threadId"`
			TurnID    string            `json:"turnId"`
			CWD       string            `json:"cwd"`
			Command   string            `json:"command"`
			ItemID    string            `json:"itemId"`
			GrantRoot *string           `json:"grantRoot"`
			Reason    *string           `json:"reason"`
			Available []json.RawMessage `json:"availableDecisions"`
			Questions []struct {
				ID       string `json:"id"`
				Question string `json:"question"`
				IsSecret bool   `json:"isSecret"`
				Options  []struct {
					Label string `json:"label"`
				} `json:"options"`
			} `json:"questions"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &req) != nil || req.Params.ThreadID != threadID || req.Params.TurnID == "" {
		return nil, ErrNativeShape
	}
	interactionID, err := desktopipc.InteractionID(raw)
	if err != nil {
		return nil, ErrNativeShape
	}
	card := map[string]any{"interactionId": interactionID, "kind": "unsupported", "prompt": "此交互类型暂未支持，请在电脑端处理。", "availableDecisions": []string{}}
	switch req.Method {
	case "item/tool/requestUserInput":
		if len(req.Params.Questions) == 0 {
			return nil, ErrNativeShape
		}
		for _, q := range req.Params.Questions {
			if q.IsSecret {
				card["prompt"] = "此问题需要敏感输入，请在电脑端处理。"
				return card, nil
			}
		}
		questions := make([]any, 0, len(req.Params.Questions))
		for _, q := range req.Params.Questions {
			if q.ID == "" || q.Question == "" {
				return nil, ErrNativeShape
			}
			options := make([]string, 0, len(q.Options))
			for _, o := range q.Options {
				options = append(options, o.Label)
			}
			questions = append(questions, map[string]any{"id": q.ID, "question": q.Question, "options": options})
		}
		card["kind"] = "user_input"
		card["prompt"] = "请回答 Codex 的补充问题"
		card["availableDecisions"] = []string{"answer"}
		card["questions"] = questions
	case "item/commandExecution/requestApproval":
		if req.Params.CWD != cwd || req.Params.Command == "" {
			return nil, ErrNativeShape
		}
		available := make([]string, 0)
		for _, d := range req.Params.Available {
			var decision string
			if json.Unmarshal(d, &decision) == nil {
				switch decision {
				case "accept":
					available = append(available, "accept_once")
				case "decline":
					available = append(available, "deny")
				case "cancel":
					available = append(available, "deny_and_stop")
				}
			}
		}
		card["kind"] = "command_approval"
		card["prompt"] = fmt.Sprintf("目录: %s\n命令: %s", cwd, req.Params.Command)
		card["availableDecisions"] = available
	case "item/fileChange/requestApproval":
		if req.Params.ItemID == "" {
			return nil, ErrNativeShape
		}
		itemRaw, err := desktopipc.FileChangeItem(state, req.Params.TurnID, req.Params.ItemID)
		if err != nil {
			return nil, ErrNativeShape
		}
		var item struct {
			Changes []struct {
				Path string `json:"path"`
				Kind struct {
					Type string `json:"type"`
				} `json:"kind"`
				Diff string `json:"diff"`
			} `json:"changes"`
		}
		if json.Unmarshal(itemRaw, &item) != nil || len(item.Changes) == 0 {
			return nil, ErrNativeShape
		}
		var preview strings.Builder
		if req.Params.Reason != nil && *req.Params.Reason != "" {
			preview.WriteString(*req.Params.Reason + "\n")
		}
		for _, change := range item.Changes {
			if change.Path == "" || change.Kind.Type == "" || change.Diff == "" {
				return nil, ErrNativeShape
			}
			preview.WriteString(change.Kind.Type + ": " + change.Path + "\n" + change.Diff + "\n")
		}
		boundID, err := desktopipc.InteractionIDWithContext(raw, itemRaw)
		if err != nil {
			return nil, ErrNativeShape
		}
		card["interactionId"] = boundID
		card["kind"] = "file_approval"
		card["prompt"] = preview.String()
		decisions := []string{"deny", "deny_and_stop"}
		if req.Params.GrantRoot == nil {
			decisions = append([]string{"accept_once"}, decisions...)
		}
		card["availableDecisions"] = decisions
	case "item/permissions/requestApproval":
		permission, err := desktopipc.ParsePermissionRequest(raw, threadID, cwd)
		if err != nil {
			return nil, ErrNativeShape
		}
		var pretty bytes.Buffer
		if json.Indent(&pretty, permission.Profile, "", "  ") != nil {
			pretty.Write(permission.Profile)
		}
		card["kind"] = "permission_request"
		card["prompt"] = fmt.Sprintf("目录: %s\n原因: %s\n请求权限:\n%s", cwd, permission.Reason, pretty.String())
		card["availableDecisions"] = []string{"deny"}
		if permission.Grantable {
			card["availableDecisions"] = []string{"accept_once", "deny"}
		}
	}
	return card, nil
}
