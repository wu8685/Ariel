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
	cwd := ChooseWorkspace(facts.SessionCWD, "/work/recorded", facts.TurnCWD, facts.ThreadCWD, facts.AgentStateDir)
	if facts.Title != "方案讨论" || cwd != "/work/watched" {
		t.Fatalf("facts=%+v cwd=%s", facts, cwd)
	}
	agentDir := "/Users/owner/Library/Application Support/ArielLocal"
	if got := ChooseWorkspace("", "/work/recorded", "/work/from-turn", agentDir, agentDir); got != "/work/recorded" {
		t.Fatalf("recorded cwd lost: %s", got)
	}
	if got := ChooseWorkspace("", "", "", agentDir, agentDir); got != "" {
		t.Fatalf("agent state dir used as workspace: %s", got)
	}
	if got := ChooseWorkspace("/work/session", "/work/recorded", agentDir, agentDir, agentDir); got != "/work/session" {
		t.Fatalf("session_meta lost: %s", got)
	}
}

func TestRealisticThreadReadPrefersSessionMetaOverAgentStateDir(t *testing.T) {
	const agentDir = "/Users/owner/Library/Application Support/ArielLocal"
	const workspace = "/Users/owner/tmp/real-workspace"
	const threadID = "22222222-2222-4222-8222-222222222222"
	facts := ExtractSessionFacts(map[string]any{
		"thread": map[string]any{
			"threadId": threadID, "title": "fix tests", "cwd": agentDir, "agentStateDir": agentDir,
			"session_meta": map[string]any{
				"type": "session_meta",
				"payload": map[string]any{
					"id": threadID, "cwd": workspace, "originator": "codex_cli_rs", "cli_version": "0.160.0", "source": "vscode",
				},
			},
			"turns": []any{map[string]any{"turnId": "turn-1", "cwd": agentDir, "items": []any{}}},
		},
	})
	cwd := ChooseWorkspace(facts.SessionCWD, "", facts.TurnCWD, facts.ThreadCWD, facts.AgentStateDir)
	if cwd != workspace {
		t.Fatalf("cwd=%q facts=%+v", cwd, facts)
	}
	normalized := ExtractSessionFacts(map[string]any{
		"thread": map[string]any{
			"threadId": threadID, "cwd": agentDir, "sessionCwd": workspace, "agentStateDir": agentDir,
		},
	})
	if got := ChooseWorkspace(normalized.SessionCWD, "/work/recorded", normalized.TurnCWD, normalized.ThreadCWD, normalized.AgentStateDir); got != workspace {
		t.Fatalf("normalized session cwd lost: %s", got)
	}
	omitted := ExtractSessionFacts(map[string]any{
		"thread": map[string]any{"threadId": threadID, "cwd": agentDir, "agentStateDir": agentDir},
	})
	if got := ChooseWorkspace(omitted.SessionCWD, "", omitted.TurnCWD, omitted.ThreadCWD, omitted.AgentStateDir); got != "" {
		t.Fatalf("untrustworthy cwd was kept: %s", got)
	}
}
