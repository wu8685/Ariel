package tools

import (
	"encoding/json"
	"strings"
)

// ThreadView is the bounded runtime reading used to update a task. Summary is
// the last assistant message, collapsed and clipped. Interaction prompts are
// not copied: they can contain commands.
type ThreadView struct {
	Freshness  string
	Runtime    string
	Status     string
	TurnID     string
	TurnStatus string
	Summary    string
	Notable    bool
}

// ClassifySnapshot reads a relay cache snapshot or a thread.read payload.
// A missing or stale snapshot is not notable: unknown must not look finished.
func ClassifySnapshot(data map[string]any) (ThreadView, bool) {
	if data == nil {
		return ThreadView{}, false
	}
	freshness, _ := data["freshness"].(string)
	thread, ok := unwrapThread(data["thread"])
	if !ok {
		if _, hasRuntime := data["runtime"]; hasRuntime {
			thread = data
			ok = true
		}
	}
	if !ok {
		if freshness == "" {
			return ThreadView{}, false
		}
		return ThreadView{Freshness: freshness, Runtime: "unknown"}, true
	}
	if freshness != "" && freshness != "fresh" {
		return ThreadView{Freshness: freshness, Runtime: "unknown"}, true
	}
	runtime, _ := thread["runtime"].(string)
	view := ThreadView{Freshness: freshness, Runtime: runtime}
	if freshness == "" {
		view.Freshness = "fresh"
	}
	view.TurnID, view.TurnStatus, view.Summary = latestTurn(thread)
	if pendingCount(thread["pendingInteractions"]) > 0 {
		view.Status = "waiting_user"
		view.Notable = true
		if view.Summary == "" {
			view.Summary = "需要你处理审批或补充输入"
		}
		return view, true
	}
	switch view.TurnStatus {
	case "failed", "interrupted":
		view.Status = "attention"
		view.Notable = true
		if view.Summary == "" {
			view.Summary = "这一轮失败或被中断"
		}
		return view, true
	}
	if runtime == "inProgress" || view.TurnStatus == "inProgress" {
		view.Status = "running"
		return view, true
	}
	if (runtime == "idle" || runtime == "") && view.TurnStatus == "completed" {
		view.Status = "review"
		view.Notable = true
		view.Runtime = "idle"
		if view.Summary == "" {
			view.Summary = "这一轮已经结束"
		}
		return view, true
	}
	if runtime == "" || runtime == "unknown" || runtime == "notLoaded" {
		view.Runtime = "unknown"
		return view, true
	}
	return view, true
}

func unwrapThread(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case json.RawMessage:
		return decodeThread(typed)
	case []byte:
		return decodeThread(typed)
	case string:
		if typed == "" {
			return nil, false
		}
		return decodeThread([]byte(typed))
	default:
		return nil, false
	}
}

func decodeThread(raw []byte) (map[string]any, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var thread map[string]any
	if json.Unmarshal(raw, &thread) != nil || thread == nil {
		return nil, false
	}
	return thread, true
}

func pendingCount(value any) int {
	items, _ := value.([]any)
	return len(items)
}

func latestTurn(thread map[string]any) (string, string, string) {
	turns, _ := thread["turns"].([]any)
	if len(turns) == 0 {
		return "", "", ""
	}
	last, _ := turns[len(turns)-1].(map[string]any)
	if last == nil {
		return "", "", ""
	}
	turnID, _ := last["turnId"].(string)
	status, _ := last["status"].(string)
	items, _ := last["items"].([]any)
	for index := len(items) - 1; index >= 0; index-- {
		item, _ := items[index].(map[string]any)
		if item == nil {
			continue
		}
		role, _ := item["role"].(string)
		text, _ := item["text"].(string)
		if role == "assistant" && strings.TrimSpace(text) != "" {
			return turnID, status, boundSummary(text)
		}
	}
	return turnID, status, ""
}

func boundSummary(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 240 {
		return string(runes[:240]) + "…"
	}
	return string(runes)
}
