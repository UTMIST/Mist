package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testPersonID = "00000000-0000-4000-8000-000000000001"
const testChallengeID = "00000000-0000-4000-8000-000000000002"

func newMistyTestAuth(t *testing.T) (*AuthService, *atomic.Bool, *atomic.Int32) {
	t.Helper()
	active, calls := &atomic.Bool{}, &atomic.Int32{}
	active.Store(true)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "gw_test_private" {
			t.Error("missing gateway credential")
		}
		switch r.URL.Path {
		case "/v1/auth/email/start":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["client_ip"] != "192.0.2.1" {
				t.Error("client IP was not derived from socket")
			}
			writeJSON(w, 202, map[string]any{"challenge_id": testChallengeID, "expires_in": 600})
		case "/v1/auth/email/confirm":
			calls.Add(1)
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["code"] != "123456" || !active.Load() {
				writeJSON(w, 400, map[string]string{"detail": "invalid"})
				return
			}
			writeJSON(w, 200, map[string]string{"person_id": testPersonID, "email": "member@example.com"})
		case "/v1/auth/people/" + testPersonID + "/status":
			writeJSON(w, 200, map[string]bool{"active": active.Load()})
		default:
			t.Error("unexpected gateway path", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(gateway.Close)
	t.Setenv("MIST_AUTH_MODE", "misty")
	t.Setenv("MISTY_GATEWAY_URL", gateway.URL)
	t.Setenv("MISTY_GATEWAY_API_KEY", "gw_test_private")
	t.Setenv("MIST_SITE_ORIGIN", "https://compute.example.com")
	s := newTestAuthService(t)
	return s, active, calls
}

func mistyRequest(s *AuthService, path string, body any, cookies []*http.Cookie) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Origin", "https://compute.example.com")
	r.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestMistyLoginPreservesUserIDAndRole(t *testing.T) {
	s, _, _ := newMistyTestAuth(t)
	_, err := s.db.Exec(`INSERT INTO user(id,name,email,role,createdAt,updatedAt) VALUES('existing-user','Member','MEMBER@example.com','admin',datetime('now'),datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	start := mistyRequest(s, "/auth/misty/start", map[string]string{"email": "member@example.com"}, nil)
	if start.Code != 202 {
		t.Fatalf("start: %d %s", start.Code, start.Body.String())
	}
	confirm := mistyRequest(s, "/auth/misty/confirm", map[string]string{"code": "123456", "challenge_id": testChallengeID}, start.Result().Cookies())
	if confirm.Code != 200 {
		t.Fatalf("confirm: %d %s", confirm.Code, confirm.Body.String())
	}
	var response struct {
		User Member `json:"user"`
	}
	json.Unmarshal(confirm.Body.Bytes(), &response)
	if response.User.ID != "existing-user" || response.User.Role != "admin" {
		t.Fatal("lost account identity", response.User)
	}
	for _, cookie := range confirm.Result().Cookies() {
		if cookie.MaxAge > 0 && (!cookie.Secure || !cookie.HttpOnly) {
			t.Error("insecure session cookie")
		}
	}
	if strings.Contains(confirm.Body.String(), `"token"`) {
		t.Error("session token exposed to JavaScript")
	}
	replay := mistyRequest(s, "/auth/misty/confirm", map[string]string{"code": "123456", "challenge_id": testChallengeID}, start.Result().Cookies())
	if replay.Code != 400 {
		t.Fatalf("replay: %d", replay.Code)
	}
}

func TestMistyConfirmRequiresBrowserBinding(t *testing.T) {
	s, _, calls := newMistyTestAuth(t)
	result := mistyRequest(s, "/auth/misty/confirm", map[string]string{"code": "123456", "challenge_id": testChallengeID}, nil)
	if result.Code != 400 || calls.Load() != 0 {
		t.Fatal("unbound confirm reached gateway", result.Code, calls.Load())
	}
}

func TestMistyLoginDisablesPasswordAndRequiresOrigin(t *testing.T) {
	s, _, _ := newMistyTestAuth(t)
	if got := authRequest(s, "POST", "/auth/sign-in/email", map[string]string{"email": "x", "password": "x"}, ""); got.Code != 403 {
		t.Fatalf("password bypass status: %d", got.Code)
	}
	if got := authRequest(s, "POST", "/auth/misty/start", map[string]string{"email": "member@example.com"}, ""); got.Code != 403 {
		t.Fatalf("missing Origin: %d", got.Code)
	}
}

func startMistyTest(t *testing.T, s *AuthService) []*http.Cookie {
	t.Helper()
	r := mistyRequest(s, "/auth/misty/start", map[string]string{"email": "member@example.com"}, nil)
	if r.Code != 202 {
		t.Fatalf("start: %d %s", r.Code, r.Body.String())
	}
	return r.Result().Cookies()
}

func TestMistyAttemptBudgetAndConcurrentRedemption(t *testing.T) {
	t.Run("attempt budget", func(t *testing.T) {
		s, _, calls := newMistyTestAuth(t)
		cookies := startMistyTest(t, s)
		for i := 0; i < 5; i++ {
			if r := mistyRequest(s, "/auth/misty/confirm", map[string]string{"challenge_id": testChallengeID, "code": "000000"}, cookies); r.Code != 400 {
				t.Fatal(r.Code)
			}
		}
		if r := mistyRequest(s, "/auth/misty/confirm", map[string]string{"challenge_id": testChallengeID, "code": "123456"}, cookies); r.Code != 400 || calls.Load() != 5 {
			t.Fatal("budget bypass", r.Code, calls.Load())
		}
	})
	t.Run("concurrent redemption", func(t *testing.T) {
		s, _, calls := newMistyTestAuth(t)
		cookies := startMistyTest(t, s)
		var successes atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Go(func() {
				r := mistyRequest(s, "/auth/misty/confirm", map[string]string{"challenge_id": testChallengeID, "code": "123456"}, cookies)
				if r.Code == 200 {
					successes.Add(1)
				} else if r.Code != 400 {
					t.Errorf("unexpected status %d", r.Code)
				}
			})
		}
		wg.Wait()
		if successes.Load() != 1 || calls.Load() != 1 {
			t.Fatal("multiple redemption", successes.Load(), calls.Load())
		}
	})
}

func TestMistyLinkingFailsClosed(t *testing.T) {
	for _, scenario := range []string{"banned", "ambiguous", "different identity"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, _ := newMistyTestAuth(t)
			_, err := s.db.Exec(`INSERT INTO user(id,name,email,role,createdAt,updatedAt) VALUES('existing','Member','member@example.com','admin',datetime('now'),datetime('now'))`)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "banned":
				_, err = s.db.Exec(`UPDATE user SET banned=1`)
			case "ambiguous":
				_, err = s.db.Exec(`INSERT INTO user(id,name,email,role,createdAt,updatedAt) VALUES('other','Member','MEMBER@example.com','user',datetime('now'),datetime('now'))`)
			case "different identity":
				_, err = s.db.Exec(`INSERT INTO misty_identity VALUES('00000000-0000-4000-8000-000000000099','existing')`)
			}
			if err != nil {
				t.Fatal(err)
			}
			cookies := startMistyTest(t, s)
			r := mistyRequest(s, "/auth/misty/confirm", map[string]string{"challenge_id": testChallengeID, "code": "123456"}, cookies)
			if r.Code != 403 || len(r.Result().Cookies()) != 0 {
				t.Fatal("linked unavailable account", r.Code)
			}
			var count int
			if err = s.db.QueryRow(`SELECT count(*) FROM session`).Scan(&count); err != nil || count != 0 {
				t.Fatal("session created", count, err)
			}
		})
	}
}

func TestMistySessionStatusAndProvenance(t *testing.T) {
	s, active, _ := newMistyTestAuth(t)
	cookies := startMistyTest(t, s)
	confirm := mistyRequest(s, "/auth/misty/confirm", map[string]string{"challenge_id": testChallengeID, "code": "123456"}, cookies)
	if confirm.Code != 200 {
		t.Fatal(confirm.Code)
	}
	r := httptest.NewRequest("GET", "/session", nil)
	for _, c := range confirm.Result().Cookies() {
		r.AddCookie(c)
	}
	member, err := s.session(r)
	if err != nil || member == nil || member.Role != "user" {
		t.Fatal("new member session", member, err)
	}
	active.Store(false)
	s.misty.statuses[testPersonID].until = time.Time{}
	if member, err = s.session(r); err != nil || member != nil {
		t.Fatal("inactive session accepted", member, err)
	}
	s.misty.statuses[testPersonID].until = time.Time{}
	s.misty.baseURL = "http://127.0.0.1:1"
	if member, err = s.session(r); err == nil || member != nil {
		t.Fatal("outage did not fail closed")
	}
	if _, err = s.db.Exec(`UPDATE session SET mistyPersonId=NULL`); err != nil {
		t.Fatal(err)
	}
	if member, err = s.session(r); err != nil || member != nil {
		t.Fatal("legacy session bypass", member, err)
	}
}

func TestMistyValidationExpiryAndDatabaseFailure(t *testing.T) {
	s, _, calls := newMistyTestAuth(t)
	for _, body := range []map[string]string{{"email": "x"}, {"email": "member@example.com", "client_ip": "192.0.2.99"}} {
		if r := mistyRequest(s, "/auth/misty/start", body, nil); r.Code != 400 {
			t.Fatal("bad input accepted", r.Code)
		}
	}
	r := httptest.NewRequest("POST", "/auth/misty/start", strings.NewReader(`{"email":"member@example.com"}`))
	r.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("legacy origin accepted", w.Code)
	}
	cookies := startMistyTest(t, s)
	if _, err := s.db.Exec(`UPDATE misty_login SET expires_at=0`); err != nil {
		t.Fatal(err)
	}
	if r := mistyRequest(s, "/auth/misty/confirm", map[string]string{"challenge_id": testChallengeID, "code": "123456"}, cookies); r.Code != 400 || calls.Load() != 0 {
		t.Fatal("expired challenge attempted")
	}
	s.db.Close()
	if r := mistyRequest(s, "/auth/misty/start", map[string]string{"email": "member@example.com"}, nil); r.Code != 503 {
		t.Fatal("database failure admitted", r.Code)
	}
}

func TestMistyConfiguration(t *testing.T) {
	for _, raw := range []string{"http://compute.example.com", "https://user:pass@example.com", "https://example.com/path", "https://example.com?secret=x"} {
		if safeMistyURL(raw) {
			t.Error("unsafe URL accepted", raw)
		}
	}
	for _, raw := range []string{"https://compute.example.com", "http://127.0.0.1:3000", "http://[::1]:3000"} {
		if !safeMistyURL(raw) {
			t.Error("safe URL rejected", raw)
		}
	}
	t.Setenv("MIST_AUTH_MODE", "typo")
	if _, err := newMistyClient(); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestMistyLegacyOwnershipSurvivesEmailChangeAndReassignment(t *testing.T) {
	s, _, _ := newMistyTestAuth(t)
	_, err := s.db.Exec(`INSERT INTO user(id,name,email,role,createdAt,updatedAt) VALUES('legacy-user','Owner','old@example.com','admin',datetime('now'),datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	s.legacyEmail = "old@example.com"
	if err = s.initMisty(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/auth/misty/confirm", nil)
	owner, _, err := s.createMistySession(r, testPersonID, "old@example.com", "unused", "unused")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err = s.createMistySession(r, testPersonID, "new@example.com", "unused", "unused")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.createMistySession(r, "00000000-0000-4000-8000-000000000099", "old@example.com", "unused", "unused")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{auth: s, executor: &KubernetesExecutor{owner: "legacy-owner"}}
	for _, member := range []*Member{owner, other} {
		request := r.WithContext(context.WithValue(r.Context(), memberKey{}, member))
		got := a.requestExecutor(request).owner
		if (got == "legacy-owner") != (member.ID == "legacy-user") {
			t.Fatalf("incorrect legacy ownership for %s", member.ID)
		}
	}
	// Reboots and rollback to local mode retain the recorded owner.
	t.Setenv("MIST_AUTH_MODE", "local")
	s.legacyUserID = ""
	if err = s.initMisty(); err != nil || s.legacyUserID != "legacy-user" {
		t.Fatal("lost durable ownership", err)
	}
}

type mistyRoundTrip func(*http.Request) (*http.Response, error)

func (f mistyRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMistyQuotaResponseIsRetryableWithoutSpendingGuess(t *testing.T) {
	s, _, _ := newMistyTestAuth(t)
	cookies := startMistyTest(t, s)
	s.misty.http.Transport = mistyRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
	})
	r := mistyRequest(s, "/auth/misty/confirm", map[string]string{"challenge_id": testChallengeID, "code": "123456"}, cookies)
	if r.Code != 429 || r.Header().Get("Retry-After") == "" {
		t.Fatal("quota not preserved", r.Code)
	}
	var attempts int
	if err := s.db.QueryRow(`SELECT attempts FROM misty_login WHERE challenge_id=?`, testChallengeID).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("quota charged a guess", attempts, err)
	}
}
