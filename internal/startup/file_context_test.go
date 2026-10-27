package startup

import (
	"strings"
	"testing"
)

func TestFileFeaturesPersistWithoutDependingOnInheritedEnvironment(t *testing.T) {
	opts, err := parseArgs([]string{"up", "local", "--listen", "192.168.1.20:8080", "--device-id", "fixture", "--device-name", "Fixture", "--enable-file-context", "--enable-workspace-browser", "--enable-document-assets"})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := prepareConfig(t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(agentEnvironment(nil, "/fixture", cfg, "fake-token"), "\n")
	for _, key := range []string{"ARIEL_ENABLE_FILE_CONTEXT=true", "ARIEL_ENABLE_WORKSPACE_BROWSER=true", "ARIEL_ENABLE_DOCUMENT_ASSETS=true"} {
		if !strings.Contains(joined, key) {
			t.Fatalf("missing %s", key)
		}
	}
	if strings.Contains(joined, "CHILLING_ENDPOINT") || cfg.TasksEnabled {
		t.Fatal("file setup acquired an external dependency")
	}
	if _, err := parseArgs([]string{"up", "agent", "--enable-file-context", "--disable-chilling-file-context"}); err == nil {
		t.Fatal("conflicting aliases accepted")
	}
}
