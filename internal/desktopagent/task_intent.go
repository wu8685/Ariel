package desktopagent

import (
	"encoding/json"
	"strings"
)

type taskIntent struct {
	Action   string `json:"task_action"`
	TaskID   string `json:"task_id"`
	Revision string `json:"task_revision"`
}

func taskIntentFromTurn(turn map[string]any, taskID, revision, commandKind string) (taskIntent, bool) {
	items, _ := turn["items"].([]any)
	for index := len(items) - 1; index >= 0; index-- {
		item, _ := items[index].(map[string]any)
		text, ok := item["text"].(string)
		if item["role"] != "assistant" || !ok || strings.TrimSpace(text) != text || text == "" {
			continue
		}
		var intent taskIntent
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&intent) != nil || decoder.More() {
			continue
		}
		var trailing any
		if decoder.Decode(&trailing) == nil {
			continue
		}
		if intent.TaskID != taskID || intent.Revision != revision {
			continue
		}
		switch commandKind {
		case "claim":
			if intent.Action != "claim" {
				continue
			}
		case "work":
			if intent.Action != "done" && intent.Action != "blocked" {
				continue
			}
		default:
			continue
		}
		return intent, true
	}
	return taskIntent{}, false
}
