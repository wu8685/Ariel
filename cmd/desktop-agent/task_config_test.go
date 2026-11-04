package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wu8685/Ariel/internal/probe"
)

func TestUnverifiedNativeProfileBlocksOnlyTaskWrites(t *testing.T) {
	for _, profile := range []probe.IPCProfileStatus{probe.IPCProfileUnverified, probe.IPCProfileUnsupported, ""} {
		check := taskExecutionCheckForProfile(true, false, profile)
		if check == nil || check(context.Background(), "", "/fixture") == nil {
			t.Fatalf("unverified native profile %q authorized task execution", profile)
		}
	}
	if taskExecutionCheckForProfile(true, false, probe.IPCProfileVerified) != nil {
		t.Fatal("verified native profile replaced the separate official provider gate")
	}
	if taskExecutionCheckForProfile(false, false, probe.IPCProfileUnverified) != nil {
		t.Fatal("disabled tasks introduced a native gate")
	}
	if taskExecutionCheckForProfile(true, true, probe.IPCProfileUnverified) != nil {
		t.Fatal("manager-only mode acquired a local Codex requirement")
	}
}

func TestPuckBrainAllowsOnlyVerifiedOrIsolatedProbeProfile(t *testing.T) {
	if !puckBrainAllowed(probe.IPCProfileVerified, "26.930.31730", "0.160.0") {
		t.Fatal("verified native profile disabled Puck brain")
	}
	if !puckBrainAllowed(probe.IPCProfileUnverified, "26.930.51102", "0.160.0") {
		t.Fatal("isolated Puck write-probe profile was not admitted")
	}
	for _, tc := range []struct {
		profile      probe.IPCProfileStatus
		desktop, cli string
	}{
		{probe.IPCProfileUnverified, "26.931.1", "0.160.0"},
		{probe.IPCProfileUnverified, "26.930.51102", "0.161.0"},
		{probe.IPCProfileUnsupported, "26.930.51102", "0.160.0"},
	} {
		if puckBrainAllowed(tc.profile, tc.desktop, tc.cli) {
			t.Fatalf("unprobed profile admitted: %+v", tc)
		}
	}
}

func TestTaskManagerAndExecutorConfigurationRemainSeparate(t *testing.T) {
	base := config{url: "ws://localhost:8080/ws", token: "fixture-device-token", deviceID: "manager", deviceName: "Manager"}
	if _, err := configureTasks(base, false, true, "unix:///tmp/tasks.sock", "", "", ""); err == nil {
		t.Fatal("manager mode implicitly enabled tasks")
	}
	if _, err := configureTasks(base, true, true, "", "", "", ""); err == nil {
		t.Fatal("manager without backend endpoint accepted")
	}
	manager, err := configureTasks(base, true, true, "unix:///tmp/tasks.sock", "", "", "")
	if err != nil || !manager.tasksManagerOnly || manager.appPath != "" || manager.socket != "" {
		t.Fatalf("manager-only requires Codex: %+v %v", manager, err)
	}
	if _, err := configureTasks(base, true, false, "", "", "relative-root", "manager"); err == nil {
		t.Fatal("relative task execution root accepted")
	}
	executor, err := configureTasks(base, true, false, "", "", "/fixture/workspace", "manager")
	if err != nil || len(executor.taskRoots) != 1 || len(executor.taskManagers) != 1 {
		t.Fatalf("execution boundary missing: %+v %v", executor, err)
	}
	disabled, err := configureTasks(base, false, false, "bad endpoint", "relative-vault", "relative-root", "")
	if err != nil || len(disabled.taskRoots) != 0 {
		t.Fatal("disabled tasks probed optional configuration", err)
	}
}

func TestObsidianTaskSourceIsExclusiveAndUsesAnAbsoluteVault(t *testing.T) {
	base := config{url: "ws://localhost:8080/ws", token: "fixture-device-token", deviceID: "manager", deviceName: "Manager"}
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "tasks", "list"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager, err := configureTasks(base, true, true, "", vault, "", "")
	resolvedVault, resolveErr := filepath.EvalSymlinks(vault)
	if err != nil || resolveErr != nil || manager.obsidianTaskVault != resolvedVault || !manager.tasksManagerOnly {
		t.Fatalf("obsidian manager config=%+v err=%v", manager, err)
	}
	if _, err := configureTasks(base, true, true, "unix:///tmp/tasks.sock", vault, "", ""); err == nil {
		t.Fatal("Chilling and Obsidian acquired the same task source")
	}
	if _, err := configureTasks(base, true, true, "", "relative-vault", "", ""); err == nil {
		t.Fatal("relative Obsidian vault accepted")
	}
}
