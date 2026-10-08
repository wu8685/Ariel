package webauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	lib "github.com/go-webauthn/webauthn/webauthn"
)

const (
	sessionCookieName  = "__Host-ariel_session"
	ceremonyCookieName = "__Host-ariel_webauthn"
	sessionLifetime    = 24 * time.Hour
	ceremonyLifetime   = 5 * time.Minute
	maxAuthBodyBytes   = 1 << 20
	maxUsedCeremonies  = 1024
)

type Config struct {
	PublicOrigin    string
	RPID            string
	CredentialsFile string
	SessionKey      []byte
	SetupToken      string
}

type Manager struct {
	publicOrigin string
	setupToken   string
	store        *credentialStore
	codec        *cookieCodec
	webauthn     *lib.WebAuthn
	now          func() time.Time
	usedMu       sync.Mutex
	used         map[[32]byte]time.Time
}

type sessionClaims struct {
	Subject string `json:"sub"`
}

type ceremonyClaims struct {
	Kind      string          `json:"kind"`
	Bootstrap bool            `json:"bootstrap,omitempty"`
	Session   lib.SessionData `json:"session"`
}

func New(cfg Config) (*Manager, error) {
	origin, err := url.Parse(cfg.PublicOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("public origin must be an HTTPS origin without a path")
	}
	host := origin.Hostname()
	if cfg.RPID == "" || (host != cfg.RPID && !strings.HasSuffix(host, "."+cfg.RPID)) || net.ParseIP(cfg.RPID) != nil {
		return nil, errors.New("RP ID must be the public host or its parent domain")
	}
	store, err := openCredentialStore(cfg.CredentialsFile)
	if err != nil {
		return nil, err
	}
	if len(store.snapshot().Credentials) == 0 && len(cfg.SetupToken) < 32 {
		return nil, errors.New("setup token must contain at least 32 characters until the first Passkey is enrolled")
	}
	codec, err := newCookieCodec(cfg.SessionKey, time.Now)
	if err != nil {
		return nil, err
	}
	wa, err := lib.New(&lib.Config{
		RPDisplayName: "Ariel",
		RPID:          cfg.RPID,
		RPOrigins:     []string{cfg.PublicOrigin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationRequired,
		},
	})
	if err != nil {
		return nil, err
	}
	return &Manager{publicOrigin: cfg.PublicOrigin, setupToken: cfg.SetupToken, store: store, codec: codec, webauthn: wa, now: time.Now, used: map[[32]byte]time.Time{}}, nil
}

func (m *Manager) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/status", m.status)
	mux.HandleFunc("POST /api/auth/passkey/register/options", m.registerOptions)
	mux.HandleFunc("POST /api/auth/passkey/register/verify", m.registerVerify)
	mux.HandleFunc("POST /api/auth/passkey/login/options", m.loginOptions)
	mux.HandleFunc("POST /api/auth/passkey/login/verify", m.loginVerify)
	mux.HandleFunc("POST /api/auth/logout", m.logout)
	return mux
}

func (m *Manager) Authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	var claims sessionClaims
	return m.codec.open("session", cookie.Value, &claims) == nil && claims.Subject == "owner"
}

func (m *Manager) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"mode": "passkey", "authenticated": m.Authenticated(r), "enrollmentRequired": len(m.store.snapshot().Credentials) == 0,
	})
}

func (m *Manager) registerOptions(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeMutation(w, r) {
		return
	}
	var input struct {
		SetupToken string `json:"setupToken"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	user := m.store.snapshot()
	bootstrap := len(user.Credentials) == 0
	if bootstrap {
		if len(input.SetupToken) != len(m.setupToken) || subtle.ConstantTimeCompare([]byte(input.SetupToken), []byte(m.setupToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid setup token")
			return
		}
	} else if !m.Authenticated(r) {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	exclusions := lib.Credentials(user.Credentials).CredentialDescriptors()
	creation, session, err := m.webauthn.BeginRegistration(user, lib.WithExclusions(exclusions))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start registration")
		return
	}
	if !m.setCeremonyCookie(w, ceremonyClaims{Kind: "register", Bootstrap: bootstrap, Session: *session}) {
		writeError(w, http.StatusInternalServerError, "could not protect registration")
		return
	}
	writeJSON(w, http.StatusOK, creation)
}

func (m *Manager) registerVerify(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeMutation(w, r) {
		return
	}
	claims, ok := m.readCeremony(w, r, "register")
	if !ok {
		return
	}
	m.clearCookie(w, ceremonyCookieName)
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	credential, err := m.webauthn.FinishRegistration(m.store.snapshot(), claims.Session, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Passkey registration failed")
		return
	}
	var saveErr error
	if claims.Bootstrap {
		saveErr = m.store.addBootstrap(*credential)
	} else {
		saveErr = m.store.add(*credential)
	}
	if saveErr != nil {
		writeError(w, http.StatusInternalServerError, "could not save Passkey")
		return
	}
	if !m.setSessionCookie(w) {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Manager) loginOptions(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeMutation(w, r) {
		return
	}
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	user := m.store.snapshot()
	if len(user.Credentials) == 0 {
		writeError(w, http.StatusConflict, "Passkey enrollment required")
		return
	}
	assertion, session, err := m.webauthn.BeginLogin(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start login")
		return
	}
	if !m.setCeremonyCookie(w, ceremonyClaims{Kind: "login", Session: *session}) {
		writeError(w, http.StatusInternalServerError, "could not protect login")
		return
	}
	writeJSON(w, http.StatusOK, assertion)
}

func (m *Manager) loginVerify(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeMutation(w, r) {
		return
	}
	claims, ok := m.readCeremony(w, r, "login")
	if !ok {
		return
	}
	m.clearCookie(w, ceremonyCookieName)
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	credential, err := m.webauthn.FinishLogin(m.store.snapshot(), claims.Session, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Passkey login failed")
		return
	}
	if err := m.store.update(*credential); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update Passkey")
		return
	}
	if !m.setSessionCookie(w) {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Manager) logout(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeMutation(w, r) {
		return
	}
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	m.clearCookie(w, sessionCookieName)
	m.clearCookie(w, ceremonyCookieName)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (m *Manager) authorizeMutation(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != m.publicOrigin {
		writeError(w, http.StatusForbidden, "origin not allowed")
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "application/json required")
		return false
	}
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

func (m *Manager) setCeremonyCookie(w http.ResponseWriter, claims ceremonyClaims) bool {
	value, err := m.codec.seal("ceremony", claims, ceremonyLifetime)
	if err != nil {
		return false
	}
	m.setCookie(w, ceremonyCookieName, value, ceremonyLifetime)
	return true
}

func (m *Manager) readCeremony(w http.ResponseWriter, r *http.Request, kind string) (ceremonyClaims, bool) {
	cookie, err := r.Cookie(ceremonyCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Passkey ceremony expired")
		return ceremonyClaims{}, false
	}
	var claims ceremonyClaims
	if m.codec.open("ceremony", cookie.Value, &claims) != nil || claims.Kind != kind {
		writeError(w, http.StatusUnauthorized, "Passkey ceremony expired")
		return ceremonyClaims{}, false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	m.usedMu.Lock()
	now := m.now()
	for candidate, expiry := range m.used {
		if !now.Before(expiry) {
			delete(m.used, candidate)
		}
	}
	if _, consumed := m.used[key]; consumed {
		m.usedMu.Unlock()
		writeError(w, http.StatusUnauthorized, "Passkey ceremony already used")
		return ceremonyClaims{}, false
	}
	if len(m.used) >= maxUsedCeremonies {
		var oldestKey [32]byte
		var oldest time.Time
		for candidate, expiry := range m.used {
			if oldest.IsZero() || expiry.Before(oldest) {
				oldestKey, oldest = candidate, expiry
			}
		}
		delete(m.used, oldestKey)
	}
	m.used[key] = now.Add(ceremonyLifetime)
	m.usedMu.Unlock()
	return claims, true
}

func (m *Manager) setSessionCookie(w http.ResponseWriter) bool {
	value, err := m.codec.seal("session", sessionClaims{Subject: "owner"}, sessionLifetime)
	if err != nil {
		return false
	}
	m.setCookie(w, sessionCookieName, value, sessionLifetime)
	return true
}

func (m *Manager) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(ttl.Seconds()), Expires: m.now().Add(ttl)})
}

func (m *Manager) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
