package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsolationRejectsRealState(t *testing.T) {
	root := t.TempDir()
	iso := layout(root, filepath.Join(root, "codex"), filepath.Join(root, "ipc.sock"))
	if err := validateIsolation(iso); err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(home, ".codex", "config.toml"),
		filepath.Join(home, "Library", "Application Support", "ArielLocal"),
		filepath.Join(home, ".ariel", "puck"),
	} {
		bad := iso
		bad.IPCSocket = path
		if err := validateIsolation(bad); err == nil {
			t.Fatalf("accepted forbidden path %s", path)
		}
	}
	escaped := iso
	escaped.DBPath = filepath.Join(root, "..", "puck.db")
	if err := validateIsolation(escaped); err == nil {
		t.Fatal("accepted a database outside the temp root")
	}
}

func TestOwnerSessionIsBootstrappedWithoutPasskey(t *testing.T) {
	root := t.TempDir()
	iso := layout(root, "", "")
	if err := prepareTree(iso); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapOwner(iso.HarnessDB); err != nil {
		t.Fatal(err)
	}
	ok, err := ownerBootstrapped(iso.HarnessDB)
	if err != nil || !ok {
		t.Fatalf("owner=%v err=%v", ok, err)
	}
}

func TestInspectPackPrefersWorkspaceAndOmitsAgentState(t *testing.T) {
	const threadID = "22222222-2222-4222-8222-222222222222"
	const deviceID = "e2e-device"
	const workspace = "/tmp/workspace-a"
	const agentDir = "/tmp/agent-state"
	pack := "[puck-handoff]\n【Referenced sessions】\n- deviceId: " + deviceID + "\n  threadId: " + threadID + "\n  cwd: " + workspace + "\n  link: codex://threads/" + threadID + "\n"
	view := inspectPack(pack, threadID, deviceID, workspace, agentDir)
	if !view.HasLink || !view.HasDeviceID || !view.HasThreadID || !view.CWDTrusted || view.Duplicate || view.HasSummary || view.HasTruncation {
		t.Fatalf("view=%+v", view)
	}
	wrong := inspectPack("[puck-handoff]\n【Referenced sessions】\n  cwd: "+agentDir+"\n[puck-handoff]\n结论\n已省略\n", threadID, deviceID, workspace, agentDir)
	if wrong.CWDTrusted || !wrong.Duplicate || !wrong.HasSummary || !wrong.HasTruncation {
		t.Fatalf("wrong=%+v", wrong)
	}
	omitted := inspectPack("[puck-handoff]\n- deviceId: "+deviceID+"\n  threadId: "+threadID+"\n  link: codex://threads/"+threadID+"\n", threadID, deviceID, workspace, agentDir)
	if !omitted.CWDTrusted || omitted.CWD != "" {
		t.Fatalf("omitted=%+v", omitted)
	}
}

func TestCwdTrustedResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(real, "workspace-a")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	viaLink := filepath.Join(link, "workspace-a")
	agentDir := filepath.Join(root, "agent-state")
	if !cwdTrusted(viaLink, workspace, agentDir) {
		t.Fatalf("symlink cwd was rejected: %s vs %s", viaLink, workspace)
	}
	if cwdTrusted(agentDir, workspace, agentDir) {
		t.Fatal("agent state dir was accepted")
	}
}

func TestSystemContextLineCount(t *testing.T) {
	text := "【系统上下文】\nintro\n- 交接时 Puck 会附上被引用会话的链接；需要时自己去读，并写下你的目标和判断。\n- 接住的事用 save_bot_task 记成任务。\n- 新请求可能是旧任务的延续时，先 list_bot_tasks，是的话用 merge_bot_tasks。\n【机主自定义上下文】\n无\n"
	lines, observed, handoff, save, merge := systemContextLines(text)
	if !observed || !handoff || !save || !merge || lines != 5 {
		t.Fatalf("lines=%d observed=%v handoff=%v save=%v merge=%v", lines, observed, handoff, save, merge)
	}
}

func TestReportRedactsMessageBodies(t *testing.T) {
	report := Report{OK: true}
	report.add(passResult("A", "handoff", []Check{{Name: "pack", Pass: true, Detail: "[puck-handoff]\n【Referenced sessions】\n用户原话很长"}}))
	if report.HardFailed || !report.OK {
		t.Fatalf("report=%+v", report)
	}
	body, err := report.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "[puck-handoff]") || strings.Contains(string(body), "【Referenced") {
		t.Fatalf("body leaked a pack: %s", body)
	}
	soft := Report{OK: true}
	soft.add(softResult("D", "merge", 2, []Check{{Name: "joined", Pass: false}}))
	if soft.exitCode() != 3 || soft.HardFailed || !soft.SoftFailed {
		t.Fatalf("soft=%+v", soft)
	}
}

func TestIsolatedRelayBootsOwnerWithoutPasskey(t *testing.T) {
	iso := layout(t.TempDir(), "", "")
	stack, err := newStack(iso)
	if err != nil {
		t.Fatal(err)
	}
	defer stack.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := stack.startRelay(ctx); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get("http://" + stack.listen + "/api/auth/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var status map[string]any
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status["mode"] != "pin" || status["authenticated"] != false {
		t.Fatalf("status=%v", status)
	}
	owner, err := dialOwner(ctx, stack.wsURL(), stack.origin, stack.pin)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	listed, err := owner.call(ctx, "puck.list", "relay", map[string]any{}, 5*time.Second)
	if err != nil || outcomeCode(listed) != "accepted" {
		t.Fatalf("list=%v err=%v", outcomeCode(listed), err)
	}
	if _, err := os.Stat(iso.DBPath); err != nil {
		t.Fatal(err)
	}
}

func TestObservedScenariosDoNotFailTheRun(t *testing.T) {
	var report Report
	report.add(ScenarioResult{ID: "B", Status: "observed", Checks: []Check{{Name: "picked-up", Pass: false, Detail: "attached=0"}}})
	report.add(ScenarioResult{ID: "D", Status: "observed", Checks: []Check{{Name: "continuation-joined-x", Pass: false}}})
	if report.HardFailed || report.SoftFailed || report.exitCode() != 0 || !report.OK {
		t.Fatalf("observed checks failed the run: %+v", report)
	}
}

func TestRedactKeepsErrorText(t *testing.T) {
	raw := "request-failed: context deadline exceeded\n[puck-handoff]\nthread 11111111-1111-4111-8111-111111111111\n\nnext line\ngoroutine 1 [running]\nat main.boom"
	got := redact(raw)
	if !strings.Contains(got, "request-failed: context deadline exceeded") || !strings.Contains(got, "next line") {
		t.Fatalf("error text dropped: %s", got)
	}
	if strings.Contains(got, "11111111") || strings.Contains(got, "[puck-handoff]") || strings.Contains(got, "goroutine") {
		t.Fatalf("secret or body kept: %s", got)
	}
}
