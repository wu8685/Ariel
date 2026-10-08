package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestECSScriptExposesSafeSingleNodeLifecycle(t *testing.T) {
	script := repositoryFile(t, "scripts/deploy-ecs.sh")
	for _, required := range []string{
		"set -Eeuo pipefail",
		"--mode pin",
		"--mode passkey",
		"disable-bootstrap",
		"--install-docker",
		"io.github.wu8685.ariel.managed=ecs",
		"--env-file",
		"--read-only",
		"--cap-drop",
		"--security-opt",
		"/healthz",
		"ARIEL_WEBAUTHN_CREDENTIALS_FILE=/data/auth.json",
		"docker image inspect \"$caddy_image\"",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("ECS script missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"curl | sh",
		"curl -fsSL https://get.docker.com",
		"rm -rf \"$state_dir\"",
		"-e ARIEL_TOKEN=",
		"-e ARIEL_WEB_PIN=",
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("ECS script contains unsafe pattern %q", forbidden)
		}
	}
}

func TestECSScriptDocumentsPersistentPINAndPasskeyModes(t *testing.T) {
	doc := repositoryFile(t, "docs/operations/ecs-single-node-deployment.md")
	for _, required := range []string{
		"./scripts/deploy-ecs.sh up --mode pin",
		"./scripts/deploy-ecs.sh up --mode passkey",
		"./scripts/deploy-ecs.sh disable-bootstrap",
		"/opt/ariel/agent-token",
		"/opt/ariel/data/auth.json",
		"不要把 PIN 模式直接暴露到公网",
		"80 / 443 TCP",
		"443 UDP",
		"Desktop Agent",
		"不可变 tag 或 digest",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("ECS guide missing %q", required)
		}
	}
}

func TestECSScriptHelpDoesNotRequireDocker(t *testing.T) {
	repo := repositoryRoot(t)
	command := execCommand(t, filepath.Join(repo, "scripts", "deploy-ecs.sh"), "--help")
	command.Dir = repo
	command.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("deploy-ecs.sh --help: %v\n%s", err, output)
	}
	for _, required := range []string{"up", "status", "logs", "show", "disable-bootstrap", "down"} {
		if !strings.Contains(string(output), required) {
			t.Errorf("help output missing %q", required)
		}
	}
}

func TestECSScriptRejectsUnsafeStateDirectoryBeforeDocker(t *testing.T) {
	repo := repositoryRoot(t)
	command := execCommand(t, filepath.Join(repo, "scripts", "deploy-ecs.sh"), "status", "--state-dir", "/")
	command.Dir = repo
	command.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("unsafe state directory unexpectedly accepted: %s", output)
	}
	if !strings.Contains(string(output), "状态目录不能是根目录") {
		t.Fatalf("unexpected unsafe-directory error: %s", output)
	}
}

func execCommand(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	return exec.Command(name, args...)
}
