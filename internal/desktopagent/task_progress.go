package desktopagent

import (
	"fmt"
	"strings"
)

func taskTurnHasAssistantOutput(turn map[string]any) bool {
	items, _ := turn["items"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		text, ok := item["text"].(string)
		if item["role"] == "assistant" && ok && strings.TrimSpace(text) != "" {
			return true
		}
	}
	return false
}

// taskSafeProgress deliberately does not read native prose, tool labels,
// details or paths. Unknown/private source material stays on the executor.
func taskSafeProgress(status string, turn map[string]any) string {
	label := "状态待同步"
	switch status {
	case "running":
		label = "正在执行"
	case "completed":
		label = "本轮执行结束，待验收"
	case "waiting":
		label = "等待用户处理或依赖恢复"
	case "failed":
		label = "本轮执行未完成，需要检查"
	default:
		return label
	}
	items, _ := turn["items"].([]any)
	completed, total, files := 0, 0, 0
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		activity, ok := item["activity"].(map[string]any)
		if !ok {
			continue
		}
		switch activity["kind"] {
		case "commandExecution", "fileChange", "mcpToolCall", "collabAgentToolCall", "webSearch", "imageView", "tool":
		default:
			continue
		}
		total++
		if activity["status"] == "completed" {
			completed++
		}
		if activity["kind"] == "fileChange" {
			files++
		}
	}
	if total > 0 {
		label += fmt.Sprintf("；已完成 %d/%d 项工具活动", completed, total)
	}
	if files > 0 {
		label += fmt.Sprintf("，含 %d 项文件变更活动", files)
	}
	return label
}
