package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryFile(t *testing.T, path string) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository from test source")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(current), "..", "..", path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestContainerImageBuildsRelayAndWebOnly(t *testing.T) {
	dockerfile := repositoryFile(t, "Dockerfile")
	for _, required := range []string{
		"ARG WEB_BUILD_IMAGE=golang:1.26-alpine",
		"FROM ${WEB_BUILD_IMAGE} AS web-build",
		"apk add --cache-dir /var/cache/apk --update-cache nodejs npm",
		"npm ci",
		"COPY protocol/v1.schema.json /src/protocol/v1.schema.json",
		"RUN npm run build",
		"FROM golang:1.26-alpine AS relay-build",
		"go build",
		"./cmd/relay",
		"ARIEL_LISTEN=0.0.0.0:8080",
		"ARIEL_WEB_DIST=/app/web/dist",
		"EXPOSE 8080",
		"USER ariel",
		"HEALTHCHECK",
		`ENTRYPOINT ["/app/ariel-relay"]`,
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile missing %q", required)
		}
	}
	for _, forbidden := range []string{"cmd/desktop-agent", "internal/codex", "Codex.app", "ARIEL_TOKEN=", "ARIEL_WEB_PIN=", "--chown=ariel"} {
		if strings.Contains(dockerfile, forbidden) {
			t.Errorf("Dockerfile must not contain %q", forbidden)
		}
	}
}

func TestDockerBuildContextExcludesLocalAndSecretState(t *testing.T) {
	ignored := repositoryFile(t, ".dockerignore")
	for _, required := range []string{
		".git/",
		".local/",
		".env",
		".env.*",
		"graphify-out/",
		"web/node_modules/",
		"web/dist/",
		"web/test-results/",
		"web/playwright-report/",
	} {
		if !strings.Contains(ignored, required) {
			t.Errorf(".dockerignore missing %q", required)
		}
	}
}

func TestContainerDocumentationKeepsDesktopAgentOnHost(t *testing.T) {
	doc := repositoryFile(t, "docs/operations/container-deployment.md")
	for _, required := range []string{
		"Relay + Web",
		"Desktop Agent",
		"宿主机",
		"docker build",
		"docker run",
		"ARIEL_TOKEN",
		"ARIEL_WEB_PIN",
		"ARIEL_ORIGINS",
		"--allow-insecure-ws",
		"不要直接暴露到公网",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("container deployment guide missing %q", required)
		}
	}
}
