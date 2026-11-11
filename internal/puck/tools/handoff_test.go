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
			ThreadID: threadID, Status: "completed", Watch: "closed", Outcome: "review",
			Conclusion: "最终结论：可以按方案 B 发布。\n" + strings.Repeat("结", MaxHandoffConclusionRunes) + "\nCONCLUSION-MARKER: use approach B",
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
	for _, absent := range []string{"USER_TAIL_OMITTED", "最终结论：可以按方案 B 发布。"} {
		if strings.Contains(once, absent) {
			t.Fatalf("pack copied %s", absent)
		}
	}
	if !strings.Contains(once, "CONCLUSION-MARKER: use approach B") || !strings.Contains(once, "已省略开头") {
		t.Fatalf("conclusion tail was dropped: %s", once)
	}
	suffix := once[strings.Index(once, HandoffSentinel):]
	if len([]rune(suffix)) > MaxHandoffRunes {
		t.Fatalf("pack runes=%d", len([]rune(suffix)))
	}
	if !strings.Contains(once, "codex://threads/"+threadID) || !strings.Contains(once, "已省略") {
		t.Fatalf("reference or truncation missing: %s", once)
	}
}

func TestExtractSessionFactsUsesWorkspaceTailAndTurnStatus(t *testing.T) {
	facts := ExtractSessionFacts(map[string]any{
		"freshness": "fresh",
		"thread": map[string]any{
			"title": "方案讨论", "cwd": "/Users/owner/.ariel/puck/assistant",
			"session_meta":        map[string]any{"payload": map[string]any{"cwd": "/work/watched"}},
			"pendingInteractions": []any{map[string]any{"prompt": "SUPER_SECRET_PROMPT"}},
			"turns": []any{
				map[string]any{"turnId": "turn-1", "status": "completed", "cwd": "/work/old-turn", "items": []any{map[string]any{"role": "assistant", "text": "EARLY_TURN_NOT_FOR_HANDOFF"}}},
				map[string]any{"turnId": "turn-2", "status": "completed", "items": []any{map[string]any{"role": "user", "text": "SUPER_SECRET_PROMPT"}}},
				map[string]any{"turnId": "turn-3", "status": "completed", "cwd": "/work/from-turn", "items": []any{map[string]any{"role": "assistant", "text": "HEAD_SHOULD_DROP\nCONCLUSION-MARKER: use approach B"}}},
			},
		},
	})
	cwd := ChooseWorkspace(facts.SessionCWD, "/work/recorded", facts.TurnCWD, facts.ThreadCWD)
	if facts.Title != "方案讨论" || cwd != "/work/watched" || facts.Status != "completed" || facts.Conclusion != "HEAD_SHOULD_DROP\nCONCLUSION-MARKER: use approach B" {
		t.Fatalf("facts=%+v cwd=%s", facts, cwd)
	}
	if strings.Contains(facts.Conclusion, "EARLY_TURN_NOT_FOR_HANDOFF") || strings.Contains(facts.Conclusion, "SUPER_SECRET_PROMPT") {
		t.Fatalf("conclusion copied extra material: %s", facts.Conclusion)
	}
	if got := ChooseWorkspace("", "/work/recorded", "/work/from-turn", "/Users/owner/.ariel/puck/assistant"); got != "/work/recorded" {
		t.Fatalf("recorded cwd lost: %s", got)
	}
	if got := ChooseWorkspace("", "", "/work/from-turn", "/Users/owner/.ariel/puck/assistant"); got != "/work/from-turn" {
		t.Fatalf("turn cwd lost: %s", got)
	}
	if got := ChooseWorkspace("", "", "", "/Users/owner/.ariel/puck/assistant"); got != "" {
		t.Fatalf("agent state dir used as workspace: %s", got)
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
