package tools

import (
	"strings"
	"testing"
)

func TestAppendHandoffKeepsThePackSmallAndIdempotent(t *testing.T) {
	threadID := "11111111-1111-4111-8111-111111111111"
	handoff := Handoff{
		UserText: "原话一字不差。" + strings.Repeat("长", MaxHandoffUserRunes) + "USER_TAIL_OMITTED",
		Sessions: []HandoffSession{{
			Title: "方案讨论", DeviceName: "执行机", DeviceID: "exec-a", CWD: "/work/watched",
			ThreadID: threadID, Status: "closed", Outcome: "review",
			Conclusion: "最终结论：可以按方案 B 发布。" + strings.Repeat("结", MaxHandoffConclusionRunes) + "CONCLUSION_TAIL_OMITTED",
		}},
	}
	once := AppendHandoff("我理解为要在新目录实现发布步骤。", handoff)
	twice := AppendHandoff(once, handoff)
	if once != twice || strings.Count(once, HandoffSentinel) != 1 {
		t.Fatal("handoff pack was duplicated")
	}
	if !strings.HasPrefix(once, "【Bot 对任务的理解】\n我理解为要在新目录实现发布步骤。") || !strings.Contains(once, "【Referenced sessions】") {
		t.Fatalf("order=%s", once)
	}
	for _, absent := range []string{"USER_TAIL_OMITTED", "CONCLUSION_TAIL_OMITTED"} {
		if strings.Contains(once, absent) {
			t.Fatalf("pack copied %s", absent)
		}
	}
	suffix := once[strings.Index(once, HandoffSentinel):]
	if len([]rune(suffix)) > MaxHandoffRunes {
		t.Fatalf("pack runes=%d", len([]rune(suffix)))
	}
	if !strings.Contains(once, "codex://threads/"+threadID) || !strings.Contains(once, "已省略") {
		t.Fatalf("reference or truncation missing: %s", once)
	}
}

func TestExtractSessionFactsUsesOnlyTheLastAssistantMessage(t *testing.T) {
	title, cwd, conclusion := ExtractSessionFacts(map[string]any{
		"freshness": "fresh",
		"thread": map[string]any{
			"title": "方案讨论", "cwd": "/work/watched",
			"pendingInteractions": []any{map[string]any{"prompt": "SUPER_SECRET_PROMPT"}},
			"turns": []any{
				map[string]any{"turnId": "turn-1", "status": "completed", "items": []any{map[string]any{"role": "assistant", "text": "EARLY_TURN_NOT_FOR_HANDOFF"}}},
				map[string]any{"turnId": "turn-2", "status": "completed", "items": []any{map[string]any{"role": "user", "text": "SUPER_SECRET_PROMPT"}}},
				map[string]any{"turnId": "turn-3", "status": "completed", "items": []any{map[string]any{"role": "assistant", "text": "最终结论：可以按方案 B 发布。"}}},
			},
		},
	})
	if title != "方案讨论" || cwd != "/work/watched" || conclusion != "最终结论：可以按方案 B 发布。" {
		t.Fatalf("title=%q cwd=%q conclusion=%q", title, cwd, conclusion)
	}
	if strings.Contains(conclusion, "EARLY_TURN_NOT_FOR_HANDOFF") || strings.Contains(conclusion, "SUPER_SECRET_PROMPT") {
		t.Fatalf("conclusion copied extra material: %s", conclusion)
	}
}

func TestCodexThreadLinkRequiresPersistedUUID(t *testing.T) {
	if CodexThreadLink("brain-thread-a") != "" {
		t.Fatal("non-uuid thread received a Codex deep link")
	}
	if CodexThreadLink("11111111-1111-4111-8111-111111111111") != "codex://threads/11111111-1111-4111-8111-111111111111" {
		t.Fatal("uuid deep link mismatch")
	}
}
