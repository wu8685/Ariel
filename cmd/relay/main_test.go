package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurationRequiresTokenOriginAndBuiltWeb(t *testing.T) {
	if _, err := configFrom("", "012345", "http://localhost:8080", "web/dist", "127.0.0.1:8080"); err == nil {
		t.Fatal("empty token accepted")
	}
	if _, err := configFrom("secret", "012345", "", "web/dist", "127.0.0.1:8080"); err == nil {
		t.Fatal("empty origin accepted")
	}
	if _, err := configFrom("secret", "012345", "http://localhost:8080", "/not/existing", "127.0.0.1:8080"); err == nil {
		t.Fatal("missing Web build accepted")
	}
}

func TestHandlerServesWebWithoutExposingToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>Ariel</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := configFrom("secret", "012345", "http://localhost:8080", dir, "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := buildHandler(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "Ariel") || strings.Contains(r.Body.String(), "secret") {
		t.Fatalf("response: %d %s", r.Code, r.Body.String())
	}
}

func TestConfigurationRequiresSixASCIIDigitWebPIN(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, pin := range []string{"", "12345", "1234567", "12a456", "１２３４５６"} {
		if _, err := configFrom("agent-secret", pin, "http://localhost:8080", dir, "127.0.0.1:8080"); err == nil || !strings.Contains(err.Error(), "ARIEL_WEB_PIN") || (pin != "" && strings.Contains(err.Error(), pin)) {
			t.Fatalf("invalid PIN %q: %v", pin, err)
		}
	}
	cfg, err := configFrom("agent-secret", "012345", "http://localhost:8080", dir, "127.0.0.1:8080")
	if err != nil || cfg.webPIN != "012345" {
		t.Fatalf("leading zero PIN: %+v, %v", cfg, err)
	}
	if _, err := configFrom("012345", "012345", "http://localhost:8080", dir, "127.0.0.1:8080"); err == nil {
		t.Fatal("Agent token and Web PIN may not be identical")
	}
}
