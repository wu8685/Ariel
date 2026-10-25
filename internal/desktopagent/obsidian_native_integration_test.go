package desktopagent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"net/http/httptest"

	"github.com/wu8685/Ariel/internal/obsidiantasks"
	"github.com/wu8685/Ariel/internal/probe"
	"github.com/wu8685/Ariel/internal/relay"
)

func TestObsidianTaskSourceRealNativePOC(t *testing.T) {
	if os.Getenv("ARIEL_OBSIDIAN_NATIVE_POC") != "1" {
		t.Skip("set ARIEL_OBSIDIAN_NATIVE_POC=1 for the opt-in real native POC")
	}
	defaults, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	desktopVersion, err := probe.DesktopVersion(context.Background(), defaults.AppPath)
	if err != nil {
		t.Fatal(err)
	}
	binary := probe.BundledBinary(defaults.AppPath)
	cliVersion, err := probe.BinaryVersion(context.Background(), binary)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := probe.CheckIPCProfile(desktopVersion, cliVersion)
	if err != nil || profile != probe.IPCProfileVerified {
		t.Fatalf("native profile is not verified: desktop=%s cli=%s profile=%s err=%v", desktopVersion, cliVersion, profile, err)
	}

	vault := t.TempDir()
	taskDir := filepath.Join(vault, "tasks", "list")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(taskDir, "Ariel Obsidian POC.md")
	task := `---
name: "Ariel Obsidian POC"
date_created: 2026-10-18
date_modified: 2026-10-18
tags: [task, poc]
status: TODO
task_id: task-ariel-obsidian-native-poc
agent_host: poc-device
agent_id:
claimed_at:
---

## 任务描述

这是无副作用 POC。不要修改文件、不要访问网络、不要调用工具；只需确认本指令本身已满足。

## 成功条件（选填）

- [ ] Codex claim-only turn 先完成
- [ ] 同一 thread 接收 work turn

## 评论
`
	if err := os.WriteFile(taskPath, []byte(task), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := obsidiantasks.New(obsidiantasks.Options{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	credential := "poc-device-token-00000000000000000000000000000001"
	router, err := relay.New(relay.Config{Token: "legacy-token", WebPIN: "012345", AllowedOrigins: []string{"http://localhost"}, TasksEnabled: true, TaskDevices: map[string]relay.TaskDeviceCredential{
		"poc-device": {Token: credential, TaskStoreIDs: []string{source.TaskStoreID()}, ExecutionTargets: []string{"poc-device"}, Execute: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router.Handler())
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errors := make(chan error, 1)
	go func() {
		errors <- Run(ctx, Config{
			URL: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws", Token: credential,
			DeviceID: "poc-device", DeviceName: "POC Device", Binary: binary, Socket: defaults.Socket,
			TasksEnabled: true, TaskBackend: source, TaskAllowedManagers: []string{"poc-device"}, TaskAllowedRoots: []string{vault}, TaskPollInterval: 50 * time.Millisecond,
		})
	}()

	deadline := time.Now().Add(4 * time.Minute)
	var completed string
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(taskPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		completed = string(data)
		if strings.Contains(completed, "status: DONE") {
			break
		}
		select {
		case runErr := <-errors:
			t.Fatalf("Desktop Agent stopped before completion: %v\n%s", runErr, completed)
		case <-time.After(250 * time.Millisecond):
		}
	}
	if !strings.Contains(completed, "status: DONE") {
		t.Fatalf("POC did not complete before timeout:\n%s", completed)
	}
	match := regexp.MustCompile(`(?m)^agent_id: ([A-Za-z0-9_.:-]+)$`).FindStringSubmatch(completed)
	if len(match) != 2 || strings.Count(completed, "<!-- task-comment-id: claim-task-ariel-obsidian-native-poc -->") != 1 {
		t.Fatalf("POC lacks an exact native owner or idempotent claim:\n%s", completed)
	}
	t.Logf("native_thread_id=%s", match[1])
	cancel()
	select {
	case <-errors:
	case <-time.After(5 * time.Second):
		t.Fatal("Desktop Agent did not stop after the POC")
	}
}
