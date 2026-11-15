package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestHarnessShutdownKillsStateDirChildren(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("sleep", "60")
	cmd.Dir = root
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	started := time.Now()
	killHarnessProcesses(os.Getpid(), root)
	if time.Since(started) > 8*time.Second {
		t.Fatal("shutdown kill was not bounded")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child was still running")
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); err == nil {
		t.Fatal("process still exists")
	}
}

func TestProcessCWDFollowsChildDirectory(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("sleep", "30")
	cmd.Dir = root
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if got := processCWD(cmd.Process.Pid); canonicalPath(got) != canonicalPath(root) {
		t.Fatalf("cwd=%q want %s", got, root)
	}
	if got := lsofCWD(cmd.Process.Pid); canonicalPath(got) != canonicalPath(root) {
		t.Fatalf("lsof cwd=%q want %s", got, root)
	}
}

func TestPathUnderResolvesMacOSPrivateVar(t *testing.T) {
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == filepath.Clean(root) {
		t.Skip("temp dir is not behind a symlink")
	}
	if !pathUnder(resolved, root) {
		t.Fatalf("resolved cwd %q should be under temp root %q", resolved, root)
	}
	if !pathUnder(filepath.Join(resolved, "child"), root) {
		t.Fatalf("resolved child should be under temp root")
	}
	if pathUnder("/tmp", root) {
		t.Fatal("unrelated path was treated as under the temp root")
	}
}

func TestParseLsofCWD(t *testing.T) {
	body := "p42\nfcwd\nn/tmp/state with space\n"
	if got := parseLsofCWD(body); got != "/tmp/state with space" {
		t.Fatalf("cwd=%q", got)
	}
	if got := parseLsofCWD("p1\n"); got != "" {
		t.Fatalf("empty=%q", got)
	}
}

func TestReportIsWrittenBeforeCleanup(t *testing.T) {
	root := t.TempDir()
	extra := filepath.Join(root, "outside", "report.json")
	writeReportFiles(root, extra, Report{OK: true})
	if _, err := os.Stat(filepath.Join(root, "report.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(extra); err != nil {
		t.Fatal(err)
	}
}
