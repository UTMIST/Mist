package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRevocationReportsDatabaseFailure(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(map[bool]string{false: "logout", true: "administrator"}[admin], func(t *testing.T) {
			s, _, _ := newMistyTestAuth(t)
			start := startMistyTest(t, s)
			login := mistyRequest(s, "/auth/misty/confirm", map[string]string{"challenge_id": testChallengeID, "code": "123456"}, start)
			if login.Code != 200 {
				t.Fatal(login.Code)
			}
			request := httptest.NewRequest("GET", "/session", nil)
			for _, cookie := range login.Result().Cookies() {
				request.AddCookie(cookie)
			}
			member, err := s.session(request)
			if err != nil || member == nil {
				t.Fatal("missing session", err)
			}
			if _, err = s.db.Exec(`CREATE TRIGGER reject_session_delete BEFORE DELETE ON session BEGIN SELECT RAISE(FAIL,'synthetic write failure'); END`); err != nil {
				t.Fatal(err)
			}
			path, payload := "/auth/sign-out", map[string]string{}
			if admin {
				if _, err = s.db.Exec(`UPDATE user SET role='admin'`); err != nil {
					t.Fatal(err)
				}
				path, payload = "/auth/admin/revoke-user-sessions", map[string]string{"userId": member.ID}
			}
			response := mistyRequest(s, path, payload, login.Result().Cookies())
			if response.Code != 503 {
				t.Fatalf("failed revocation reported HTTP %d", response.Code)
			}
			if len(response.Result().Cookies()) != 0 {
				t.Fatal("failed logout discarded the retry credential")
			}
			if _, err = s.db.Exec(`DROP TRIGGER reject_session_delete`); err != nil {
				t.Fatal(err)
			}
			if retry := mistyRequest(s, path, payload, login.Result().Cookies()); retry.Code != 200 {
				t.Fatal("revocation retry failed", retry.Code)
			}
			if member, err = s.session(request); err != nil || member != nil {
				t.Fatal("revoked session survived", err)
			}
		})
	}
}

func TestMistyIssuerMismatchAndDatabasePin(t *testing.T) {
	t.Run("wrong environment", func(t *testing.T) {
		s, _, _ := newMistyTestAuth(t)
		t.Setenv("MISTY_ENVIRONMENT", "production")
		if err := s.initMisty(); err == nil {
			t.Fatal("staging/local issuer accepted as production")
		}
	})
	t.Run("changed origin", func(t *testing.T) {
		s, _, _ := newMistyTestAuth(t)
		called := false
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			writeJSON(w, 200, map[string]string{"issuer": "misty:local"})
		}))
		defer other.Close()
		t.Setenv("MISTY_GATEWAY_URL", other.URL)
		if err := s.initMisty(); err == nil {
			t.Fatal("database silently adopted a different gateway")
		}
		if called {
			t.Fatal("gateway credential sent before checking the database origin pin")
		}
	})
}

func TestMistyRejectsMissingOrChangedResponseIssuer(t *testing.T) {
	for _, issuer := range []string{"", "misty:production"} {
		t.Run(issuer, func(t *testing.T) {
			s, _, _ := newMistyTestAuth(t)
			cookies := startMistyTest(t, s)
			s.misty.http.Transport = mistyRoundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"issuer":"` + issuer + `","person_id":"` + testPersonID + `","email":"member@example.com"}`))}, nil
			})
			response := mistyRequest(s, "/auth/misty/confirm", map[string]string{"challenge_id": testChallengeID, "code": "123456"}, cookies)
			if response.Code != 503 {
				t.Fatal("untrusted issuer created a session", response.Code)
			}
		})
	}
}

func TestMistyUnlinkedExistingAccountCannotBeClaimedByEmail(t *testing.T) {
	for _, role := range []string{"user", "admin"} {
		t.Run(role, func(t *testing.T) {
			s, _, _ := newMistyTestAuth(t)
			if _, err := s.db.Exec(`INSERT INTO user(id,name,email,role,createdAt,updatedAt) VALUES('existing','Previous owner','member@example.com',?,datetime('now'),datetime('now'))`, role); err != nil {
				t.Fatal(err)
			}
			cookies := startMistyTest(t, s)
			response := mistyRequest(s, "/auth/misty/confirm", map[string]string{"challenge_id": testChallengeID, "code": "123456"}, cookies)
			if response.Code != 403 {
				t.Fatalf("unlinked %s account claimed by email: %d", role, response.Code)
			}
		})
	}
}

func TestMistyAccountMappingIsExplicitAndImmutable(t *testing.T) {
	s, _, _ := newMistyTestAuth(t)
	if _, err := s.db.Exec(`INSERT INTO user(id,name,email,role,createdAt,updatedAt) VALUES('first','First','member@example.com','admin',datetime('now'),datetime('now')),('second','Second','other@example.com','user',datetime('now'),datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISTY_ACCOUNT_LINKS", `{"`+testPersonID+`":"first"}`)
	if err := s.initMisty(); err != nil {
		t.Fatal(err)
	}
	if err := s.initMisty(); err != nil {
		t.Fatal("idempotent mapping failed", err)
	}
	t.Setenv("MISTY_ACCOUNT_LINKS", `{"`+testPersonID+`":"second"}`)
	if err := s.initMisty(); err == nil {
		t.Fatal("existing identity overwritten")
	}
	var linked string
	if err := s.db.QueryRow(`SELECT user_id FROM misty_identity WHERE person_id=?`, testPersonID).Scan(&linked); err != nil || linked != "first" {
		t.Fatal("original mapping lost", err)
	}
	t.Setenv("MISTY_ACCOUNT_LINKS", `{"00000000-0000-4000-8000-000000000099":"missing"}`)
	if err := s.initMisty(); err == nil {
		t.Fatal("missing target account accepted")
	}
}
