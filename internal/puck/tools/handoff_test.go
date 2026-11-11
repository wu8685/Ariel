package tools

import (
	"strings"
	"testing"
)

func TestAppendHandoffKeepsLinksAndDropsExtractedText(t *testing.T) {
	threadID := "11111111-1111-4111-8111-111111111111"
	handoff := Handoff{Sessions: []HandoffSession{{
		Title: "方案讨论", DeviceID: "exec-a", CWD: "/work/watched", ThreadID: threadID,
	}}}
	instruction := "我理解为要在新目录实现发布步骤。"
	once := AppendHandoff(instruction, handoff)
	twice := AppendHandoff(once, handoff)
	if once != twice || strings.Count(once, HandoffSentinel) != 1 {
		t.Fatal("handoff pack was duplicated")
	}
	if !strings.HasPrefix(once, instruction+"\n") || !strings.Contains(once, "codex://threads/"+threadID) {
		t.Fatalf("pack=%s", once)
	}
	for _, absent := range []string{"用户原话", "conclusion", "CONCLUSION-MARKER", "已省略", "status:"} {
		if strings.Contains(once, absent) {
			t.Fatalf("pack included %s: %s", absent, once)
		}
	}
	if AppendHandoff(instruction, Handoff{}) != instruction {
		t.Fatal("empty reference list changed the instruction")
	}
}

func TestExtractSessionFactsReadsWorkspaceMetadataOnly(t *testing.T) {
	facts := ExtractSessionFacts(map[string]any{
		"thread": map[string]any{
			"title": "方案讨论", "cwd": "/Users/owner/.ariel/puck/assistant",
			"session_meta": map[string]any{"payload": map[string]any{"cwd": "/work/watched"}},
			"turns": []any{
				map[string]any{"turnId": "turn-1", "status": "completed", "cwd": "/work/from-turn", "items": []any{map[string]any{"role": "assistant", "text": "CONCLUSION-MARKER: use approach B"}}},
			},
		},
	})
	cwd := ChooseWorkspace(facts.SessionCWD, "/work/recorded", facts.TurnCWD, facts.ThreadCWD)
	if facts.Title != "方案讨论" || cwd != "/work/watched" {
		t.Fatalf("facts=%+v cwd=%s", facts, cwd)
	}
	if got := ChooseWorkspace("", "/work/recorded", "/work/from-turn", "/Users/owner/.ariel/puck/assistant"); got != "/work/recorded" {
		t.Fatalf("recorded cwd lost: %s", got)
	}
	if got := ChooseWorkspace("", "", "", "/Users/owner/.ariel/puck/assistant"); got != "" {
		t.Fatalf("agent state dir used as workspace: %s", got)
	}
}
