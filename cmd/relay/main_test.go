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
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<html><meta name="ariel-auth-mode" content="__ARIEL_WEB_AUTH__"><meta name="ariel-chilling-file-context" content="__ARIEL_CHILLING_FILE_CONTEXT__">Ariel</html>`), 0644); err != nil {
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
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "Ariel") || !strings.Contains(r.Body.String(), `content="pin"`) || !strings.Contains(r.Body.String(), `name="ariel-chilling-file-context" content="false"`) || strings.Contains(r.Body.String(), "__ARIEL_WEB_AUTH__") || strings.Contains(r.Body.String(), "secret") {
		t.Fatalf("response: %d %s", r.Code, r.Body.String())
	}
}

func TestHTMLIsNotCachedAcrossWebBuilds(t *testing.T) {
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
	for _, path := range []string{"/", "/index.html"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if got := r.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want no-store", path, got)
		}
	}
}

func TestStaticAssetsUseBoundedPublicCaching(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>Ariel</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "brand"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index-abc123.js"), []byte("app"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brand", "logo.webp"), []byte("logo"), 0644); err != nil {
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
	tests := []struct{ path, want string }{
		{"/assets/index-abc123.js", "public, max-age=31536000, immutable"},
		{"/brand/logo.webp", "public, max-age=604800"},
	}
	for _, test := range tests {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", test.path, nil))
		if r.Code != http.StatusOK || r.Header().Get("Cache-Control") != test.want {
			t.Errorf("%s: status=%d Cache-Control=%q, want %q", test.path, r.Code, r.Header().Get("Cache-Control"), test.want)
		}
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

func TestPasskeyConfigurationIsMutuallyExclusiveAndComplete(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	valid := configInput{
		token: "agent-secret", webAuth: "passkey", origins: "https://ariel.example.com", dist: dir, listen: "127.0.0.1:8080",
		publicOrigin: "https://ariel.example.com", rpID: "ariel.example.com", credentialsFile: filepath.Join(t.TempDir(), "auth.json"),
		sessionKey: strings.Repeat("00", 32), setupToken: strings.Repeat("s", 32),
	}
	cfg, err := configFromInput(valid)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.webAuth != "passkey" || len(cfg.sessionKey) != 32 {
		t.Fatalf("passkey config = %+v", cfg)
	}

	for name, mutate := range map[string]func(*configInput){
		"pin-fallback": func(in *configInput) { in.webPIN = "012345" },
		"http-origin":  func(in *configInput) { in.publicOrigin = "http://ariel.example.com" },
		"origin-list":  func(in *configInput) { in.origins = "https://other.example.com" },
		"rp-id":        func(in *configInput) { in.rpID = "evil.example" },
		"key":          func(in *configInput) { in.sessionKey = "abcd" },
		"setup":        func(in *configInput) { in.setupToken = "short" },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := configFromInput(input); err == nil {
				t.Fatal("invalid passkey configuration accepted")
			}
		})
	}
}
