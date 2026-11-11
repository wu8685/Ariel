package main

import (
	"path/filepath"
	"strings"

	pucktools "github.com/wu8685/Ariel/internal/puck/tools"
)

// PackView is the structural result of a handoff pack. The source text is not retained.
type PackView struct {
	HasLink       bool   `json:"hasLink"`
	HasDeviceID   bool   `json:"hasDeviceId"`
	HasThreadID   bool   `json:"hasThreadId"`
	CWD           string `json:"cwd,omitempty"`
	CWDTrusted    bool   `json:"cwdTrusted"`
	SentinelCount int    `json:"sentinelCount"`
	Duplicate     bool   `json:"duplicate"`
	HasSummary    bool   `json:"hasSummary"`
	HasTruncation bool   `json:"hasTruncation"`
}

func inspectPack(text, threadID, deviceID, workspace, agentDir string) PackView {
	view := PackView{SentinelCount: strings.Count(text, pucktools.HandoffSentinel)}
	view.Duplicate = view.SentinelCount > 1
	if threadID != "" {
		view.HasLink = strings.Contains(text, "codex://threads/"+threadID)
		view.HasThreadID = strings.Contains(text, threadID)
	}
	if deviceID != "" {
		view.HasDeviceID = strings.Contains(text, "deviceId: "+deviceID) || strings.Contains(text, `"deviceId":"`+deviceID+`"`)
	}
	view.CWD = packCWD(text)
	view.CWDTrusted = cwdTrusted(view.CWD, workspace, agentDir)
	lower := strings.ToLower(text)
	for _, marker := range []string{"结论", "conclusion", "用户原话", "已省略", "omitted", "[truncated]"} {
		if strings.Contains(lower, strings.ToLower(marker)) {
			if marker == "结论" || marker == "conclusion" || marker == "用户原话" {
				view.HasSummary = true
			} else {
				view.HasTruncation = true
			}
		}
	}
	return view
}

func packCWD(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "cwd:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "cwd:"))
		}
	}
	return ""
}

func cwdTrusted(cwd, workspace, agentDir string) bool {
	if strings.TrimSpace(cwd) == "" {
		return true
	}
	cleaned := filepath.Clean(cwd)
	if agentDir != "" && pathWithin(cleaned, filepath.Clean(agentDir)) {
		return false
	}
	return workspace != "" && cleaned == filepath.Clean(workspace)
}

func pathWithin(path, root string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func systemContextLines(payload string) (int, bool, bool, bool, bool) {
	start := strings.Index(payload, "【系统上下文】")
	if start < 0 {
		return 0, false, false, false, false
	}
	body := payload[start:]
	if end := strings.Index(body, "【机主自定义上下文"); end >= 0 {
		body = body[:end]
	}
	lines := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	return lines, true,
		strings.Contains(body, "交接时 Puck 会附上被引用会话的链接"),
		strings.Contains(body, "接住的事用 save_bot_task 记成任务"),
		strings.Contains(body, "先 list_bot_tasks")
}

func userTexts(payload map[string]any) []string {
	var texts []string
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if typed["role"] == "user" {
				if text, _ := typed["text"].(string); text != "" {
					texts = append(texts, text)
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(payload)
	return texts
}

func findSystemContext(payload map[string]any) string {
	var found string
	var walk func(any)
	walk = func(value any) {
		if found != "" {
			return
		}
		switch typed := value.(type) {
		case string:
			if strings.Contains(typed, "【系统上下文】") {
				found = typed
			}
		case map[string]any:
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(payload)
	return found
}
