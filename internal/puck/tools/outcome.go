package tools

import "strings"

// Outcome says whether a snapshot is a new result the user should hear about.
// Progress reports and repeated states stay quiet so a long-running thread can
// finish turns without posting a chat message each time.
type Outcome struct {
	Notify bool
	Status string
	Reason string
}

func DecideOutcome(view ThreadView, until, toldStatus, toldSummary string) Outcome {
	if view.Freshness != "" && view.Freshness != "fresh" || view.Runtime == "unknown" {
		return Outcome{Reason: "unknown"}
	}
	summary := normalizeOutcome(view.Summary)
	previous := normalizeOutcome(toldSummary)
	switch view.Status {
	case "waiting_user":
		if toldStatus == "waiting_user" {
			return Outcome{Reason: "repeat"}
		}
		return Outcome{Notify: true, Status: "waiting_user", Reason: "blocked"}
	case "attention":
		if toldStatus == "attention" {
			return Outcome{Reason: "repeat"}
		}
		return Outcome{Notify: true, Status: "attention", Reason: "failed"}
	case "stopped":
		if toldStatus == "stopped" {
			return Outcome{Reason: "repeat"}
		}
		return Outcome{Notify: true, Status: "stopped", Reason: "stopped"}
	case "review":
		if summary != "" && summary == previous {
			return Outcome{Reason: "repeat"}
		}
		if isProgressReport(view.Summary) && !conditionMet(until, view.Summary, view.Status) {
			return Outcome{Reason: "in_progress"}
		}
		if conditionMet(until, view.Summary, view.Status) {
			return Outcome{Notify: true, Status: "review", Reason: "condition"}
		}
		if isProgressReport(view.Summary) {
			return Outcome{Reason: "in_progress"}
		}
		return Outcome{Notify: true, Status: "review", Reason: "completed"}
	default:
		return Outcome{Reason: "in_progress"}
	}
}

func isProgressReport(summary string) bool {
	text := normalizeOutcome(summary)
	if text == "" {
		return true
	}
	for _, hint := range []string{
		"还没完成", "尚未完成", "没有完成", "未完成", "还在", "正在", "继续", "检查中", "运行中", "处理中",
		"没有变化", "暂无变化", "无变化", "进行中", "稍后再看",
		"still working", "still running", "in progress", "no change", "not yet", "not done", "working on",
	} {
		if strings.Contains(text, hint) {
			return true
		}
	}
	return false
}

func conditionMet(until, summary, status string) bool {
	want := normalizeOutcome(until)
	if want == "" {
		return false
	}
	if (strings.Contains(want, "卡住") || strings.Contains(want, "审批") || strings.Contains(want, "blocked")) && status == "waiting_user" {
		return true
	}
	if (strings.Contains(want, "失败") || strings.Contains(want, "fail")) && status == "attention" {
		return true
	}
	if (strings.Contains(want, "停止") || strings.Contains(want, "stopped")) && status == "stopped" {
		return true
	}
	got := strings.NewReplacer("已经", "", "了", "", "。", "", " ", "").Replace(normalizeOutcome(summary))
	needle := strings.NewReplacer("已经", "", "了", "", "。", "", " ", "").Replace(want)
	return needle != "" && strings.Contains(got, needle) && !isProgressReport(summary)
}

func normalizeOutcome(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}
