package webauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"time"
)

const (
	remoteLoginLifetime  = 3 * time.Minute
	maxRemoteLogins      = 32
	remoteLoginIDBytes   = 16
	remotePollTokenBytes = 32
)

type remoteChallenge struct {
	tokenHash [32]byte
	code      string
	expires   time.Time
	status    string
}

func (m *Manager) remoteLoginStart(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeMutation(w, r) || !decodeJSON(w, r, &struct{}{}) {
		return
	}
	if len(m.store.snapshot().Credentials) == 0 {
		writeError(w, http.StatusConflict, "Passkey enrollment required")
		return
	}
	id, err := randomToken(remoteLoginIDBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create remote login")
		return
	}
	pollToken, err := randomToken(remotePollTokenBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create remote login")
		return
	}
	now := m.now()
	challenge := &remoteChallenge{tokenHash: sha256.Sum256([]byte(pollToken)), code: remoteLoginCode(id), expires: now.Add(remoteLoginLifetime), status: "pending"}
	m.remoteMu.Lock()
	m.purgeExpiredRemote(now)
	if len(m.remote) >= maxRemoteLogins {
		m.remoteMu.Unlock()
		writeError(w, http.StatusTooManyRequests, "remote login capacity exceeded")
		return
	}
	m.remote[id] = challenge
	m.remoteMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "pollToken": pollToken, "expiresAt": challenge.expires, "code": challenge.code})
}

func (m *Manager) remoteLoginStatus(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeMutation(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	challenge, status, ok := m.remoteSnapshot(body.ID)
	if !ok {
		writeError(w, http.StatusNotFound, "remote login not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "expiresAt": challenge.expires, "code": challenge.code})
}

func (m *Manager) remoteLoginApprove(w http.ResponseWriter, r *http.Request) {
	m.decideRemoteLogin(w, r, "approved")
}

func (m *Manager) remoteLoginReject(w http.ResponseWriter, r *http.Request) {
	m.decideRemoteLogin(w, r, "rejected")
}

func (m *Manager) decideRemoteLogin(w http.ResponseWriter, r *http.Request, decision string) {
	if !m.authorizeMutation(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &body) || !m.requireSession(w, r) {
		return
	}
	m.remoteMu.Lock()
	defer m.remoteMu.Unlock()
	challenge, status, ok := m.remoteStateLocked(body.ID)
	if !ok {
		writeError(w, http.StatusNotFound, "remote login not found")
		return
	}
	if status == "expired" {
		delete(m.remote, body.ID)
		writeError(w, http.StatusGone, "remote login expired")
		return
	}
	if challenge.status == decision {
		writeJSON(w, http.StatusOK, map[string]string{"status": decision})
		return
	}
	if challenge.status != "pending" {
		writeError(w, http.StatusConflict, "remote login already decided")
		return
	}
	challenge.status = decision
	writeJSON(w, http.StatusOK, map[string]string{"status": decision})
}

func (m *Manager) remoteLoginPoll(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeMutation(w, r) {
		return
	}
	var body struct {
		ID        string `json:"id"`
		PollToken string `json:"pollToken"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	m.remoteMu.Lock()
	challenge, status, ok := m.remoteStateLocked(body.ID)
	if !ok || !pollTokenMatches(challenge, body.PollToken) {
		m.remoteMu.Unlock()
		writeError(w, http.StatusNotFound, "remote login not found")
		return
	}
	if status == "expired" {
		delete(m.remote, body.ID)
		m.remoteMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"status": "expired"})
		return
	}
	if challenge.status == "approved" {
		delete(m.remote, body.ID)
		m.remoteMu.Unlock()
		if !m.setSessionCookie(w) {
			m.remoteMu.Lock()
			m.remote[body.ID] = challenge
			m.remoteMu.Unlock()
			writeError(w, http.StatusInternalServerError, "could not create session")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
		return
	}
	current := challenge.status
	m.remoteMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"status": current, "expiresAt": challenge.expires, "code": challenge.code})
}

func (m *Manager) remoteLoginCancel(w http.ResponseWriter, r *http.Request) {
	if !m.authorizeMutation(w, r) {
		return
	}
	var body struct {
		ID        string `json:"id"`
		PollToken string `json:"pollToken"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	m.remoteMu.Lock()
	defer m.remoteMu.Unlock()
	challenge, status, ok := m.remoteStateLocked(body.ID)
	if !ok || !pollTokenMatches(challenge, body.PollToken) {
		writeError(w, http.StatusNotFound, "remote login not found")
		return
	}
	if status == "expired" {
		delete(m.remote, body.ID)
		writeError(w, http.StatusGone, "remote login expired")
		return
	}
	if challenge.status != "pending" && challenge.status != "approved" {
		writeError(w, http.StatusConflict, "remote login already decided")
		return
	}
	challenge.status = "cancelled"
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (m *Manager) requireSession(w http.ResponseWriter, r *http.Request) bool {
	if !m.Authenticated(r) {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return false
	}
	return true
}

func (m *Manager) remoteSnapshot(id string) (remoteChallenge, string, bool) {
	m.remoteMu.Lock()
	defer m.remoteMu.Unlock()
	challenge, status, ok := m.remoteStateLocked(id)
	if !ok {
		return remoteChallenge{}, "", false
	}
	return *challenge, status, true
}

func (m *Manager) remoteStateLocked(id string) (*remoteChallenge, string, bool) {
	if !validRemoteLoginID(id) {
		return nil, "", false
	}
	challenge, ok := m.remote[id]
	if !ok {
		return nil, "", false
	}
	if !m.now().Before(challenge.expires) {
		return challenge, "expired", true
	}
	return challenge, challenge.status, true
}

func (m *Manager) purgeExpiredRemote(now time.Time) {
	for id, challenge := range m.remote {
		if !now.Before(challenge.expires) {
			delete(m.remote, id)
		}
	}
}

func pollTokenMatches(challenge *remoteChallenge, token string) bool {
	sum := sha256.Sum256([]byte(token))
	if challenge == nil {
		var zero [32]byte
		subtle.ConstantTimeCompare(sum[:], zero[:])
		return false
	}
	return subtle.ConstantTimeCompare(sum[:], challenge.tokenHash[:]) == 1
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func validRemoteLoginID(id string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(decoded) == remoteLoginIDBytes && base64.RawURLEncoding.EncodeToString(decoded) == id
}

func remoteLoginCode(id string) string {
	sum := sha256.Sum256([]byte("ariel-remote-login-display:" + id))
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, 6)
	for i := range out {
		out[i] = alphabet[int(sum[i])%len(alphabet)]
	}
	return string(out)
}
