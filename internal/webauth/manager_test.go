package webauth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lib "github.com/go-webauthn/webauthn/webauthn"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(Config{
		PublicOrigin:    "https://ariel.example.com",
		RPID:            "ariel.example.com",
		CredentialsFile: filepath.Join(t.TempDir(), "auth.json"),
		SessionKey:      []byte(strings.Repeat("a", 32)),
		SetupToken:      strings.Repeat("setup-", 6),
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStatusAndRegistrationOptionsSecurityBoundary(t *testing.T) {
	m := testManager(t)
	status := httptest.NewRecorder()
	m.Handler().ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"mode":"passkey"`) || !strings.Contains(status.Body.String(), `"enrollmentRequired":true`) {
		t.Fatalf("status: %d %s", status.Code, status.Body.String())
	}

	for _, tc := range []struct {
		name, origin, contentType, body string
		want                            int
	}{
		{"origin", "https://evil.example", "application/json", `{"setupToken":"` + strings.Repeat("setup-", 6) + `"}`, http.StatusForbidden},
		{"content-type", "https://ariel.example.com", "text/plain", `{}`, http.StatusUnsupportedMediaType},
		{"setup-token", "https://ariel.example.com", "application/json", `{"setupToken":"wrong"}`, http.StatusUnauthorized},
		{"valid", "https://ariel.example.com", "application/json", `{"setupToken":"` + strings.Repeat("setup-", 6) + `"}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/options", strings.NewReader(tc.body))
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Content-Type", tc.contentType)
			res := httptest.NewRecorder()
			m.Handler().ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status = %d, body=%s", res.Code, res.Body.String())
			}
			if tc.want == http.StatusOK {
				cookie := res.Header().Get("Set-Cookie")
				for _, part := range []string{"__Host-ariel_webauthn=", "Path=/", "HttpOnly", "Secure", "SameSite=Strict"} {
					if !strings.Contains(cookie, part) {
						t.Fatalf("ceremony cookie %q lacks %q", cookie, part)
					}
				}
			}
		})
	}
}

func TestAuthenticatedStatusRenewsOnlyALiveSession(t *testing.T) {
	m := testManager(t)
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.codec.now = m.now

	issued := httptest.NewRecorder()
	if !m.setSessionCookie(issued) {
		t.Fatal("could not issue session cookie")
	}
	original := issued.Result().Cookies()[0]

	now = now.Add(23 * time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.AddCookie(original)
	refreshed := httptest.NewRecorder()
	m.Handler().ServeHTTP(refreshed, req)
	if refreshed.Code != http.StatusOK || !strings.Contains(refreshed.Body.String(), `"authenticated":true`) {
		t.Fatalf("status: %d %s", refreshed.Code, refreshed.Body.String())
	}
	cookies := refreshed.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].Value == original.Value {
		t.Fatalf("live session was not rotated: %#v", cookies)
	}

	now = now.Add(23 * time.Hour)
	second := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	second.AddCookie(cookies[0])
	if !m.Authenticated(second) {
		t.Fatal("renewed session did not survive a second active day")
	}

	expired := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	expired.AddCookie(original)
	rejected := httptest.NewRecorder()
	m.Handler().ServeHTTP(rejected, expired)
	if strings.Contains(rejected.Body.String(), `"authenticated":true`) || rejected.Header().Get("Set-Cookie") != "" {
		t.Fatalf("expired session renewed: %s %#v", rejected.Body.String(), rejected.Header())
	}
}

func TestManagerRejectsInvalidConfiguration(t *testing.T) {
	base := Config{PublicOrigin: "https://ariel.example.com", RPID: "ariel.example.com", CredentialsFile: filepath.Join(t.TempDir(), "auth.json"), SessionKey: []byte(strings.Repeat("a", 32)), SetupToken: strings.Repeat("s", 32)}
	for name, mutate := range map[string]func(*Config){
		"http":        func(c *Config) { c.PublicOrigin = "http://ariel.example.com" },
		"wrong-rp":    func(c *Config) { c.RPID = "evil.example" },
		"short-key":   func(c *Config) { c.SessionKey = []byte("short") },
		"short-setup": func(c *Config) { c.SetupToken = "short" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.CredentialsFile = filepath.Join(t.TempDir(), "auth.json")
			mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestCeremonyCookieIsConsumedAtMostOnce(t *testing.T) {
	m := testManager(t)
	issued := httptest.NewRecorder()
	if !m.setCeremonyCookie(issued, ceremonyClaims{Kind: "login", Session: lib.SessionData{Challenge: "challenge", Expires: time.Now().Add(time.Minute)}}) {
		t.Fatal("could not issue ceremony cookie")
	}
	cookie := issued.Result().Cookies()[0]
	request := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/verify", strings.NewReader(`{}`))
		req.AddCookie(cookie)
		return req
	}
	first := httptest.NewRecorder()
	if _, ok := m.readCeremony(first, request(), "login"); !ok {
		t.Fatalf("first use rejected: %d %s", first.Code, first.Body.String())
	}
	second := httptest.NewRecorder()
	if _, ok := m.readCeremony(second, request(), "login"); ok || second.Code != http.StatusUnauthorized {
		t.Fatalf("ceremony replay accepted: %d %s", second.Code, second.Body.String())
	}
}
