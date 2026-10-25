package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskConfigurationIsExplicitPrivateAndNeverRenderedIntoWeb(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<meta name="ariel-auth-mode" content="__ARIEL_WEB_AUTH__">`), 0600); err != nil {
		t.Fatal(err)
	}
	base := configInput{token: "legacy-token", webPIN: "012345", origins: "http://localhost:8080", dist: dir, taskCredentialsFile: "/missing/private-file"}
	cfg, err := configFromInput(base)
	if err != nil {
		t.Fatal("disabled mode read task credentials", err)
	}
	h, err := buildHandler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(r.Body.String(), "ariel-enable-tasks") || strings.Contains(r.Body.String(), "__ARIEL_ENABLE_TASKS__") {
		t.Fatal("task configuration leaked into disabled web UI")
	}
	base.enableTasks = true
	if _, err := configFromInput(base); err == nil {
		t.Fatal("enabled tasks accepted missing device credentials")
	}
	file := filepath.Join(dir, "task-devices.json")
	secret := "fixture-device-token-0000000000000000001"
	if err := os.WriteFile(file, []byte(`{"fixture":{"token":"`+secret+`","taskStoreIds":["store"],"executionTargets":["fixture"],"execute":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	base.taskCredentialsFile = file
	cfg, err = configFromInput(base)
	if err != nil {
		t.Fatal(err)
	}
	h, err = buildHandler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(r.Body.String(), "ariel-enable-tasks") || strings.Contains(r.Body.String(), "__ARIEL_ENABLE_TASKS__") || strings.Contains(r.Body.String(), secret) {
		t.Fatal("backend task mode leaked into web UI")
	}
}
