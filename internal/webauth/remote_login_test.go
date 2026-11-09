package webauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	lib "github.com/go-webauthn/webauthn/webauthn"
)

func enrollOwner(t *testing.T, m *Manager) {
	t.Helper()
	if err := m.store.add(lib.Credential{ID: []byte("owner-passkey")}); err != nil {
		t.Fatal(err)
	}
}

func issueSession(t *testing.T, m *Manager) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if !m.setSessionCookie(rec) {
		t.Fatal("could not issue session")
	}
	return rec.Result().Cookies()[0]
}

func (m *Manager) postAuth(path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Origin", m.publicOrigin)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	m.Handler().ServeHTTP(res, req)
	return res
}

func decodeBody(t *testing.T, res *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s: %v", res.Body.String(), err)
	}
	return payload
}

func sessionSetCookie(res *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" && cookie.MaxAge > 0 {
			return cookie
		}
	}
	return nil
}

func TestRemoteLoginRequiresPasskeySessionToApproveAndRedeemsOnce(t *testing.T) {
	m := testManager(t)
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.codec.now = m.now

	unenrolled := m.postAuth("/api/auth/remote-login/start", `{}`, nil)
	if unenrolled.Code != http.StatusConflict || sessionSetCookie(unenrolled) != nil {
		t.Fatalf("unenrolled start: %d %s", unenrolled.Code, unenrolled.Body.String())
	}
	enrollOwner(t, m)

	wrongOrigin := httptest.NewRequest(http.MethodPost, "/api/auth/remote-login/start", strings.NewReader(`{}`))
	wrongOrigin.Header.Set("Origin", "https://evil.example")
	wrongOrigin.Header.Set("Content-Type", "application/json")
	wrongOriginRes := httptest.NewRecorder()
	m.Handler().ServeHTTP(wrongOriginRes, wrongOrigin)
	if wrongOriginRes.Code != http.StatusForbidden {
		t.Fatalf("origin: %d", wrongOriginRes.Code)
	}

	started := m.postAuth("/api/auth/remote-login/start", `{}`, nil)
	if started.Code != http.StatusOK || sessionSetCookie(started) != nil {
		t.Fatalf("start: %d %s", started.Code, started.Body.String())
	}
	created := decodeBody(t, started)
	id, _ := created["id"].(string)
	pollToken, _ := created["pollToken"].(string)
	code, _ := created["code"].(string)
	if id == "" || pollToken == "" || code == "" || strings.Contains(id, pollToken) {
		t.Fatalf("challenge fields: %#v", created)
	}
	if strings.Contains(started.Body.String(), sessionCookieName) {
		t.Fatalf("start leaked a session cookie name: %s", started.Body.String())
	}

	status := m.postAuth("/api/auth/remote-login/status", `{"id":"`+id+`"}`, nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"status":"pending"`) || strings.Contains(status.Body.String(), pollToken) {
		t.Fatalf("status leaked or failed: %d %s", status.Code, status.Body.String())
	}

	anonymous := m.postAuth("/api/auth/remote-login/approve", `{"id":"`+id+`"}`, nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous approve: %d %s", anonymous.Code, anonymous.Body.String())
	}
	ceremonyRec := httptest.NewRecorder()
	if !m.setCeremonyCookie(ceremonyRec, ceremonyClaims{Kind: "login"}) {
		t.Fatal("ceremony")
	}
	ceremony := m.postAuth("/api/auth/remote-login/approve", `{"id":"`+id+`"}`, ceremonyRec.Result().Cookies()[0])
	if ceremony.Code != http.StatusUnauthorized {
		t.Fatalf("ceremony approve: %d %s", ceremony.Code, ceremony.Body.String())
	}

	session := issueSession(t, m)
	refreshed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.AddCookie(session)
	m.Handler().ServeHTTP(refreshed, req)
	if !strings.Contains(refreshed.Body.String(), `"authenticated":true`) {
		t.Fatal("session fixture was not authenticated")
	}
	stillPending := m.postAuth("/api/auth/remote-login/poll", `{"id":"`+id+`","pollToken":"`+pollToken+`"}`, nil)
	if stillPending.Code != http.StatusOK || !strings.Contains(stillPending.Body.String(), `"status":"pending"`) || sessionSetCookie(stillPending) != nil {
		t.Fatalf("owning a session must not approve: %d %s", stillPending.Code, stillPending.Body.String())
	}

	wrongPoll := m.postAuth("/api/auth/remote-login/poll", `{"id":"`+id+`","pollToken":"`+strings.Repeat("a", 43)+`"}`, nil)
	if wrongPoll.Code != http.StatusNotFound || sessionSetCookie(wrongPoll) != nil {
		t.Fatalf("wrong poll: %d %s", wrongPoll.Code, wrongPoll.Body.String())
	}

	approved := m.postAuth("/api/auth/remote-login/approve", `{"id":"`+id+`"}`, session)
	if approved.Code != http.StatusOK || sessionSetCookie(approved) != nil || strings.Contains(approved.Body.String(), pollToken) {
		t.Fatalf("approve: %d cookies=%v body=%s", approved.Code, approved.Result().Cookies(), approved.Body.String())
	}
	again := m.postAuth("/api/auth/remote-login/approve", `{"id":"`+id+`"}`, session)
	if again.Code != http.StatusOK {
		t.Fatalf("repeat approve: %d %s", again.Code, again.Body.String())
	}
	rejected := m.postAuth("/api/auth/remote-login/reject", `{"id":"`+id+`"}`, session)
	if rejected.Code != http.StatusConflict {
		t.Fatalf("reject after approve: %d %s", rejected.Code, rejected.Body.String())
	}

	redeemed := m.postAuth("/api/auth/remote-login/poll", `{"id":"`+id+`","pollToken":"`+pollToken+`"}`, nil)
	cookie := sessionSetCookie(redeemed)
	if redeemed.Code != http.StatusOK || !strings.Contains(redeemed.Body.String(), `"status":"approved"`) || cookie == nil {
		t.Fatalf("redeem: %d %s", redeemed.Code, redeemed.Body.String())
	}
	for _, part := range []string{"__Host-ariel_session=", "Path=/", "HttpOnly", "Secure", "SameSite=Strict"} {
		if !strings.Contains(redeemed.Header().Get("Set-Cookie"), part) {
			t.Fatalf("redeem cookie lacks %s: %s", part, redeemed.Header().Get("Set-Cookie"))
		}
	}
	authed := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	authed.AddCookie(cookie)
	if !m.Authenticated(authed) {
		t.Fatal("redeemed cookie is not a Passkey session")
	}
	if strings.Contains(redeemed.Body.String(), pollToken) || strings.Contains(redeemed.Body.String(), cookie.Value) {
		t.Fatal("redeem response contained secret material")
	}

	replay := m.postAuth("/api/auth/remote-login/poll", `{"id":"`+id+`","pollToken":"`+pollToken+`"}`, nil)
	if replay.Code != http.StatusNotFound || sessionSetCookie(replay) != nil {
		t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
	}
	replayApprove := m.postAuth("/api/auth/remote-login/approve", `{"id":"`+id+`"}`, session)
	if replayApprove.Code == http.StatusOK && strings.Contains(replayApprove.Body.String(), `"status":"approved"`) {
		// A consumed challenge must not look approvable again. 404 or conflict are both closed.
		t.Fatalf("replay approve succeeded: %s", replayApprove.Body.String())
	}
}

func TestRemoteLoginExpiryRejectionAndCancelDoNotIssueSessions(t *testing.T) {
	m := testManager(t)
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	m.codec.now = m.now
	enrollOwner(t, m)
	session := issueSession(t, m)

	start := func() (string, string) {
		t.Helper()
		res := m.postAuth("/api/auth/remote-login/start", `{}`, nil)
		if res.Code != http.StatusOK {
			t.Fatalf("start: %d %s", res.Code, res.Body.String())
		}
		body := decodeBody(t, res)
		return body["id"].(string), body["pollToken"].(string)
	}

	expiredID, expiredToken := start()
	now = now.Add(3*time.Minute + time.Second)
	expiredApprove := m.postAuth("/api/auth/remote-login/approve", `{"id":"`+expiredID+`"}`, session)
	if expiredApprove.Code != http.StatusGone || sessionSetCookie(expiredApprove) != nil {
		t.Fatalf("expired approve: %d %s", expiredApprove.Code, expiredApprove.Body.String())
	}
	expiredPoll := m.postAuth("/api/auth/remote-login/poll", `{"id":"`+expiredID+`","pollToken":"`+expiredToken+`"}`, nil)
	if sessionSetCookie(expiredPoll) != nil || strings.Contains(expiredPoll.Body.String(), `"status":"approved"`) {
		t.Fatalf("expired poll issued a session: %d %s", expiredPoll.Code, expiredPoll.Body.String())
	}

	rejectedID, rejectedToken := start()
	rejected := m.postAuth("/api/auth/remote-login/reject", `{"id":"`+rejectedID+`"}`, session)
	if rejected.Code != http.StatusOK || sessionSetCookie(rejected) != nil {
		t.Fatalf("reject: %d %s", rejected.Code, rejected.Body.String())
	}
	rejectedPoll := m.postAuth("/api/auth/remote-login/poll", `{"id":"`+rejectedID+`","pollToken":"`+rejectedToken+`"}`, nil)
	if rejectedPoll.Code != http.StatusOK || !strings.Contains(rejectedPoll.Body.String(), `"status":"rejected"`) || sessionSetCookie(rejectedPoll) != nil {
		t.Fatalf("rejected poll: %d %s", rejectedPoll.Code, rejectedPoll.Body.String())
	}
	revive := m.postAuth("/api/auth/remote-login/approve", `{"id":"`+rejectedID+`"}`, session)
	if revive.Code != http.StatusConflict {
		t.Fatalf("approve after reject: %d %s", revive.Code, revive.Body.String())
	}

	cancelledID, cancelledToken := start()
	cancelled := m.postAuth("/api/auth/remote-login/cancel", `{"id":"`+cancelledID+`","pollToken":"`+cancelledToken+`"}`, nil)
	if cancelled.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", cancelled.Code, cancelled.Body.String())
	}
	cancelledApprove := m.postAuth("/api/auth/remote-login/approve", `{"id":"`+cancelledID+`"}`, session)
	if cancelledApprove.Code != http.StatusConflict || sessionSetCookie(cancelledApprove) != nil {
		t.Fatalf("approve after cancel: %d %s", cancelledApprove.Code, cancelledApprove.Body.String())
	}
	wrongCancelID, wrongCancelToken := start()
	wrongCancel := m.postAuth("/api/auth/remote-login/cancel", `{"id":"`+wrongCancelID+`","pollToken":"`+strings.Repeat("b", 43)+`"}`, nil)
	if wrongCancel.Code != http.StatusNotFound {
		t.Fatalf("wrong cancel: %d %s", wrongCancel.Code, wrongCancel.Body.String())
	}
	stillWaiting := m.postAuth("/api/auth/remote-login/poll", `{"id":"`+wrongCancelID+`","pollToken":"`+wrongCancelToken+`"}`, nil)
	if !strings.Contains(stillWaiting.Body.String(), `"status":"pending"`) || sessionSetCookie(stillWaiting) != nil {
		t.Fatalf("wrong token cancelled the challenge: %s", stillWaiting.Body.String())
	}
}

func TestRemoteLoginCapacityAndSingleRedeemUnderRace(t *testing.T) {
	m := testManager(t)
	enrollOwner(t, m)
	ids := make([]string, 0, 32)
	for i := 0; i < 32; i++ {
		res := m.postAuth("/api/auth/remote-login/start", `{}`, nil)
		if res.Code != http.StatusOK {
			t.Fatalf("start %d: %d %s", i, res.Code, res.Body.String())
		}
		ids = append(ids, decodeBody(t, res)["id"].(string))
	}
	full := m.postAuth("/api/auth/remote-login/start", `{}`, nil)
	if full.Code != http.StatusTooManyRequests || sessionSetCookie(full) != nil {
		t.Fatalf("capacity: %d %s", full.Code, full.Body.String())
	}

	m.now = func() time.Time { return time.Now().Add(4 * time.Minute) }
	m.codec.now = m.now
	afterExpiry := m.postAuth("/api/auth/remote-login/start", `{}`, nil)
	if afterExpiry.Code != http.StatusOK {
		t.Fatalf("start after expiry: %d %s", afterExpiry.Code, afterExpiry.Body.String())
	}
	_ = ids

	fresh := testManager(t)
	enrollOwner(t, fresh)
	session := issueSession(t, fresh)
	started := fresh.postAuth("/api/auth/remote-login/start", `{}`, nil)
	body := decodeBody(t, started)
	id := body["id"].(string)
	pollToken := body["pollToken"].(string)
	if fresh.postAuth("/api/auth/remote-login/approve", `{"id":"`+id+`"}`, session).Code != http.StatusOK {
		t.Fatal("approve")
	}
	var wg sync.WaitGroup
	codes := make([]int, 8)
	cookies := make([]bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			res := fresh.postAuth("/api/auth/remote-login/poll", `{"id":"`+id+`","pollToken":"`+pollToken+`"}`, nil)
			codes[index] = res.Code
			cookies[index] = sessionSetCookie(res) != nil
		}(i)
	}
	wg.Wait()
	ok, issued := 0, 0
	for i := range codes {
		if codes[i] == http.StatusOK {
			ok++
		}
		if cookies[i] {
			issued++
		}
	}
	if ok != 1 || issued != 1 {
		t.Fatalf("concurrent redeem ok=%d cookies=%d codes=%v", ok, issued, codes)
	}
}
