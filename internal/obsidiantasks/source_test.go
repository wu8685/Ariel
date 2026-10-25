package obsidiantasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wu8685/Ariel/protocol"
)

func writeTaskFixture(t *testing.T, root, name, frontmatter, body string) string {
	t.Helper()
	dir := filepath.Join(root, "tasks", "list")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".md")
	content := "---\n" + frontmatter + "---\n\n## 任务描述\n\n" + body + "\n\n## 成功条件（选填）\n\n- [ ] 验证 POC\n\n## 评论\n"
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func newSourceFixture(t *testing.T, root string) *Source {
	t.Helper()
	source, err := New(Options{
		VaultRoot: root,
		Now:       func() time.Time { return time.Date(2026, 10, 18, 11, 12, 13, 0, time.FixedZone("CST", 8*60*60)) },
		NewID: func() string {
			return "00000000-0000-4000-8000-000000000095"
		},
		DefaultHostID: "wuke-mac-mini",
	})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func callTracking(t *testing.T, source *Source) []protocol.TaskCommand {
	t.Helper()
	var out struct {
		Commands []protocol.TaskCommand `json:"commands"`
	}
	if err := source.Call(context.Background(), "task.command.tracking", map[string]any{}, &out); err != nil {
		t.Fatal(err)
	}
	return out.Commands
}

func callCommands(t *testing.T, source *Source) []protocol.TaskCommand {
	t.Helper()
	var out struct {
		Commands []protocol.TaskCommand `json:"commands"`
	}
	if err := source.Call(context.Background(), "task.command.claim", map[string]any{}, &out); err != nil {
		t.Fatal(err)
	}
	return out.Commands
}

func TestRandomUUIDHasStableSafeShape(t *testing.T) {
	id := randomUUID()
	if matched, _ := regexp.MatchString(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`, id); !matched {
		t.Fatalf("invalid UUID: %q", id)
	}
}

func TestSourceDiscoversOnlyValidTodoAndFillsStableTaskID(t *testing.T) {
	root := t.TempDir()
	selected := writeTaskFixture(t, root, "selected", "name: Selected\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: TODO\ntask_id:\nagent_host: mac-studio-primary\nagent_id:\nclaimed_at:\n", "Run the harmless fixture.")
	writeTaskFixture(t, root, "done", "name: Done\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: DONE\ntask_id: done-1\nagent_host: mac-studio-primary\nagent_id: thread-done\nclaimed_at: 2026-10-18T10:00:00+08:00\n", "Already done.")
	writeTaskFixture(t, root, "unassigned", "name: Unassigned\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: TODO\ntask_id: task-unassigned\nagent_host:\nagent_id:\nclaimed_at:\n", "No host.")
	writeTaskFixture(t, root, "bad-status", "name: Bad\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: WAITING\ntask_id: task-bad\nagent_host: mac-studio-primary\nagent_id:\nclaimed_at:\n", "Malformed independently.")

	source := newSourceFixture(t, root)
	commands := callCommands(t, source)
	if len(commands) != 1 {
		t.Fatalf("commands=%+v", commands)
	}
	var command protocol.TaskCommand
	for _, candidate := range commands {
		if candidate.ExecutionHostID == "mac-studio-primary" {
			command = candidate
		}
	}
	if command.Kind != "claim" || command.ExecutionHostID != "mac-studio-primary" || command.TaskID != "task-00000000-0000-4000-8000-000000000095" || command.TaskRevision == "" {
		t.Fatalf("unexpected command: %+v", command)
	}
	first, err := os.ReadFile(selected)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "task_id: task-00000000-0000-4000-8000-000000000095") {
		t.Fatalf("stable task id was not persisted:\n%s", first)
	}
	// The in-memory lease prevents an unchanged poll from redispatching.
	if again := callCommands(t, source); len(again) != 0 {
		t.Fatalf("unchanged task was redispatched: %+v", again)
	}
}

func TestSourceNormalizesNativeBasesCard(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tasks", "list")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "面板验收任务.md")
	minimal := "---\nname:\ndate_created:\ndate_modified:\ntags:\nagent_host:\nagent_id:\nstatus: TODO\n---\n"
	if err := os.WriteFile(path, []byte(minimal), 0o640); err != nil {
		t.Fatal(err)
	}

	source := newSourceFixture(t, root)
	commands := callCommands(t, source)
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if len(commands) != 1 || commands[0].ExecutionHostID != "wuke-mac-mini" || commands[0].CWD != resolvedRoot {
		t.Fatalf("normalized command=%+v", commands)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"name: 面板验收任务", "date_created: 2026-10-18", "date_modified: 2026-10-18",
		"tags: [task]", "task_id: task-00000000-0000-4000-8000-000000000095",
		"agent_host: wuke-mac-mini", "agent_id:", "claimed_at:",
		"## 任务描述", "## 成功条件（选填）", "## 评论",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("normalized card missing %q:\n%s", want, text)
		}
	}
}

func TestSourceTracksBasesRenameBeforeClaim(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tasks", "list")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "未命名.md")
	if err := os.WriteFile(oldPath, []byte("---\nname:\nstatus: TODO\n---\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	source := newSourceFixture(t, root)
	if _, err := source.scan(); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(dir, "产品验收.md")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if _, err := source.scan(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: 产品验收") {
		t.Fatalf("Bases rename did not update generated name:\n%s", data)
	}
	if _, err := New(Options{VaultRoot: root, DefaultHostID: "wuke-mac-mini"}); err != nil {
		t.Fatalf("rename left an unrecoverable journal entry: %v", err)
	}
}

func TestAcceptedReceiptSurvivesSourceRestartAsTrackingOnly(t *testing.T) {
	root := t.TempDir()
	writeTaskFixture(t, root, "recover", "name: Recover\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: TODO\ntask_id: task-recover\nagent_host: executor-a\nagent_id:\nclaimed_at:\n", "Recover the accepted claim.")
	source := newSourceFixture(t, root)
	claim := callCommands(t, source)[0]
	receipt := protocol.TaskReceipt{CommandID: claim.CommandID, LeaseToken: claim.LeaseToken, Outcome: "accepted", NativeThreadID: "thread-recover", NativeTurnID: "turn-recover", ClientMessageID: claim.ClientMessageID}
	if err := source.Call(context.Background(), "task.command.record", receipt, &map[string]any{}); err != nil {
		t.Fatal(err)
	}

	restarted := newSourceFixture(t, root)
	if commands := callCommands(t, restarted); len(commands) != 0 {
		t.Fatalf("restart attempted a second native start: %+v", commands)
	}
	tracking := callTracking(t, restarted)
	if len(tracking) != 1 || tracking[0].CommandID != claim.CommandID || tracking[0].NativeThreadID != "thread-recover" || tracking[0].NativeTurnID != "turn-recover" || tracking[0].State != "accepted" {
		t.Fatalf("tracking recovery=%+v", tracking)
	}
	var validation struct {
		Valid   bool                  `json:"valid"`
		Command *protocol.TaskCommand `json:"command"`
	}
	if err := restarted.Call(context.Background(), "task.command.validate", map[string]any{"commandId": claim.CommandID, "leaseToken": claim.LeaseToken, "readOnly": true}, &validation); err != nil || !validation.Valid {
		t.Fatalf("tracking validation=%+v, %v", validation, err)
	}
	if validation.Command == nil || validation.Command.NativeThreadID != "thread-recover" || validation.Command.NativeTurnID != "turn-recover" || validation.Command.State != "accepted" {
		t.Fatalf("tracking validation did not preserve native identity: %+v", validation.Command)
	}
}

func TestChangedRevisionSupersedesAcceptedDelivery(t *testing.T) {
	root := t.TempDir()
	path := writeTaskFixture(t, root, "supersede", "name: Supersede\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: TODO\ntask_id: task-supersede\nagent_host: executor-a\nagent_id:\nclaimed_at:\n", "Initial request.")
	source := newSourceFixture(t, root)
	old := callCommands(t, source)[0]
	receipt := protocol.TaskReceipt{CommandID: old.CommandID, LeaseToken: old.LeaseToken, Outcome: "accepted", NativeThreadID: "thread-old", NativeTurnID: "turn-old", ClientMessageID: old.ClientMessageID}
	if err := source.Call(context.Background(), "task.command.record", receipt, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("\n### 2026-10-18 11:20｜昊天\n\nPlease use the latest revision.\n")
	_ = file.Close()

	commands := callCommands(t, source)
	if len(commands) != 1 || commands[0].CommandID == old.CommandID || commands[0].TaskRevision == old.TaskRevision {
		t.Fatalf("new revision did not supersede accepted delivery: old=%+v new=%+v", old, commands)
	}
	if _, ok := source.issued[old.CommandID]; ok {
		t.Fatal("superseded accepted command remained in memory")
	}
	if _, ok := source.receipts[old.CommandID]; ok {
		t.Fatal("superseded accepted receipt remained in memory")
	}
	restarted := newSourceFixture(t, root)
	if tracking := callTracking(t, restarted); len(tracking) != 0 {
		t.Fatalf("superseded accepted command remained in journal: %+v", tracking)
	}
}

func TestFailedNativeFactClearsAcceptedCommandWithoutTaskIntent(t *testing.T) {
	root := t.TempDir()
	writeTaskFixture(t, root, "retry", "name: Retry\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: TODO\ntask_id: task-retry\nagent_host: executor-a\nagent_id:\nclaimed_at:\n", "Retry after interruption.")
	source := newSourceFixture(t, root)
	claim := callCommands(t, source)[0]
	receipt := protocol.TaskReceipt{CommandID: claim.CommandID, LeaseToken: claim.LeaseToken, Outcome: "accepted", NativeThreadID: "thread-retry", NativeTurnID: "turn-retry", ClientMessageID: claim.ClientMessageID}
	if err := source.Call(context.Background(), "task.command.record", receipt, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	failed := protocol.TaskFact{EventID: "fact-failed", CommandID: claim.CommandID, TaskID: claim.TaskID, AttemptID: claim.AttemptID, ExecutionHostID: claim.ExecutionHostID, BindingGeneration: claim.BindingGeneration, NativeThreadID: "thread-retry", NativeTurnID: "turn-retry", Status: "failed"}
	if err := source.Call(context.Background(), "task.fact.record", failed, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if tracking := callTracking(t, source); len(tracking) != 0 {
		t.Fatalf("failed turn remained in recovery journal: %+v", tracking)
	}
	if commands := callCommands(t, source); len(commands) != 1 || commands[0].TaskID != claim.TaskID {
		t.Fatalf("failed claim was not retryable: %+v", commands)
	}
}

func TestBlockedWriteDoesNotLoopAndHumanEditContinuesSameThread(t *testing.T) {
	root := t.TempDir()
	path := writeTaskFixture(t, root, "blocked", "name: Blocked\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: WIP\ntask_id: task-blocked\nagent_host: executor-a\nagent_id: thread-blocked\nclaimed_at: 2026-10-18T10:00:00+08:00\n", "Wait for input.")
	source := newSourceFixture(t, root)
	work := callCommands(t, source)[0]
	receipt := protocol.TaskReceipt{CommandID: work.CommandID, LeaseToken: work.LeaseToken, Outcome: "accepted", NativeThreadID: "thread-blocked", NativeTurnID: "turn-blocked", ClientMessageID: work.ClientMessageID}
	if err := source.Call(context.Background(), "task.command.record", receipt, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	fact := protocol.TaskFact{EventID: "fact-blocked", CommandID: work.CommandID, TaskID: work.TaskID, AttemptID: work.AttemptID, ExecutionHostID: work.ExecutionHostID, BindingGeneration: work.BindingGeneration, NativeThreadID: "thread-blocked", NativeTurnID: "turn-blocked", Status: "completed", TaskAction: "blocked", TaskRevision: work.TaskRevision}
	if err := source.Call(context.Background(), "task.fact.record", fact, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if commands := callCommands(t, source); len(commands) != 0 {
		t.Fatalf("Ariel's own blocked comment caused a loop: %+v", commands)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("\n### 2026-10-18 11:20｜昊天\n\n依赖已经恢复，请继续。\n")
	_ = file.Close()
	commands := callCommands(t, source)
	if len(commands) != 1 || commands[0].NativeThreadID != "thread-blocked" || !strings.Contains(commands[0].Text, "依赖已经恢复") {
		t.Fatalf("human continuation=%+v", commands)
	}
}

func TestCorruptJournalFailsClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tasks", "list"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, ".ariel", "tasks")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{VaultRoot: root, DefaultHostID: "wuke-mac-mini"}); err == nil {
		t.Fatal("corrupt journal was accepted")
	}
}

func TestJournalRefusesSymlinkedStateDirectory(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tasks", "list"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".ariel")); err != nil {
		t.Fatal(err)
	}
	writeTaskFixture(t, root, "unsafe-state", "name: Unsafe State\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: TODO\ntask_id: task-unsafe-state\nagent_host: executor-a\nagent_id:\nclaimed_at:\n", "Do not escape.")
	source := newSourceFixture(t, root)
	var out map[string]any
	if err := source.Call(context.Background(), "task.command.claim", map[string]any{}, &out); err == nil {
		t.Fatal("symlinked journal directory was accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatalf("journal escaped vault: %+v", entries)
	}
}

func TestClaimFactCASWritesWIPAndOnlyThenCreatesWork(t *testing.T) {
	root := t.TempDir()
	path := writeTaskFixture(t, root, "poc", "name: POC\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: TODO\ntask_id: task-poc\nagent_host: executor-a\nagent_id:\nclaimed_at:\n", "Append one harmless result.")
	source := newSourceFixture(t, root)
	claim := callCommands(t, source)[0]

	// Native acceptance alone must not unlock the real task.
	receipt := protocol.TaskReceipt{CommandID: claim.CommandID, LeaseToken: claim.LeaseToken, Outcome: "accepted", NativeThreadID: "thread-poc", NativeTurnID: "turn-claim", ClientMessageID: claim.ClientMessageID}
	if err := source.Call(context.Background(), "task.command.record", receipt, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if commands := callCommands(t, source); len(commands) != 0 {
		t.Fatalf("work escaped before a valid claim intent: %+v", commands)
	}

	fact := protocol.TaskFact{EventID: "fact-claim", CommandID: claim.CommandID, TaskID: claim.TaskID, AttemptID: claim.AttemptID, ExecutionHostID: claim.ExecutionHostID, BindingGeneration: claim.BindingGeneration, NativeThreadID: "thread-poc", NativeTurnID: "turn-claim", Status: "completed", TaskAction: "claim", TaskRevision: claim.TaskRevision}
	if err := source.Call(context.Background(), "task.fact.record", fact, &map[string]any{}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"status: WIP", "agent_id: thread-poc", "claimed_at: 2026-10-18T11:12:13+08:00", "2026-10-18 11:12｜Codex thread-poc", "<!-- task-comment-id: claim-task-poc -->"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q after claim:\n%s", want, text)
		}
	}
	commands := callCommands(t, source)
	if len(commands) != 1 || commands[0].Kind != "work" || commands[0].NativeThreadID != "thread-poc" || !strings.Contains(commands[0].Text, "Append one harmless result.") {
		t.Fatalf("work command=%+v", commands)
	}
	work := commands[0]
	if work.TaskRevision == claim.TaskRevision {
		t.Fatal("claim mutation did not advance the task revision")
	}

	// A duplicate claim fact is idempotent and cannot append the claim twice.
	if err := source.Call(context.Background(), "task.fact.record", fact, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if strings.Count(string(after), "<!-- task-comment-id: claim-task-poc -->") != 1 {
		t.Fatalf("duplicate claim comment:\n%s", after)
	}

	workReceipt := protocol.TaskReceipt{CommandID: work.CommandID, LeaseToken: work.LeaseToken, Outcome: "accepted", NativeThreadID: "thread-poc", NativeTurnID: "turn-work", ClientMessageID: work.ClientMessageID}
	if err := source.Call(context.Background(), "task.command.record", workReceipt, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	workFact := protocol.TaskFact{EventID: "fact-work", CommandID: work.CommandID, TaskID: work.TaskID, AttemptID: work.AttemptID, ExecutionHostID: work.ExecutionHostID, BindingGeneration: work.BindingGeneration, NativeThreadID: "thread-poc", NativeTurnID: "turn-work", Status: "completed", TaskAction: "done", TaskRevision: work.TaskRevision}
	if err := source.Call(context.Background(), "task.fact.record", workFact, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	completed, _ := os.ReadFile(path)
	if !strings.Contains(string(completed), "status: DONE") || !strings.Contains(string(completed), "成功条件满足") {
		t.Fatalf("work completion was not recorded:\n%s", completed)
	}
	if commands := callCommands(t, source); len(commands) != 0 {
		t.Fatalf("completed task was dispatched again: %+v", commands)
	}
}

func TestValidateRejectsChangedRevisionAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	path := writeTaskFixture(t, root, "mutable", "name: Mutable\ndate_created: 2026-10-18\ndate_modified: 2026-10-18\ntags: [task]\nstatus: TODO\ntask_id: task-mutable\nagent_host: executor-a\nagent_id:\nclaimed_at:\n", "Original.")
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "tasks", "list", "escape.md")); err != nil {
		t.Fatal(err)
	}
	source := newSourceFixture(t, root)
	claim := callCommands(t, source)[0]
	file, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = file.WriteString("\nHuman changed the task.\n")
	_ = file.Close()
	var validation struct {
		Valid bool `json:"valid"`
	}
	params := map[string]any{"commandId": claim.CommandID, "leaseToken": claim.LeaseToken}
	if err := source.Call(context.Background(), "task.command.validate", params, &validation); err != nil {
		t.Fatal(err)
	}
	if validation.Valid {
		t.Fatal("changed revision remained authorized")
	}
	entries, err := source.scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.path, "escape.md") {
			t.Fatal("symlink escape entered the task collection")
		}
	}
}

func TestProtocolRoundTripKeepsOnlyBoundedTaskIntent(t *testing.T) {
	fact := protocol.TaskFact{EventID: "event", CommandID: "command", TaskID: "task", AttemptID: "attempt", ExecutionHostID: "executor", BindingGeneration: 1, NativeThreadID: "thread", NativeTurnID: "turn", Status: "completed", TaskAction: "claim", TaskRevision: strings.Repeat("a", 64)}
	raw, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "assistant") || !strings.Contains(string(raw), `"taskAction":"claim"`) {
		t.Fatalf("unsafe or missing task intent: %s", raw)
	}
}
