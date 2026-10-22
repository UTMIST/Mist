package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestAuthService(t *testing.T) *AuthService {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test-auth.sqlite")
	s, err := newAuthService(dbPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func authRequest(s *AuthService, method, path string, body any, cookie string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestAuthService_BootstrapAdmin(t *testing.T) {
	t.Setenv("MIST_ADMIN_EMAIL", "admin@mist.local")
	t.Setenv("MIST_ADMIN_PASSWORD", "adminpassword123")
	s := newTestAuthService(t)

	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM user WHERE role = 'admin'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 admin user, got %d", count)
	}

	rec := authRequest(s, "POST", "/auth/sign-in/email", map[string]string{
		"email":    "admin@mist.local",
		"password": "adminpassword123",
	}, "")
	if rec.Code != 200 {
		t.Fatalf("expected 200 login for bootstrap admin, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAuthService_SignInAndSessionLifecycle(t *testing.T) {
	s := newTestAuthService(t)

	// Create user
	hashed, _ := hashPassword("initialpassword123")
	_, err := s.db.Exec(`INSERT INTO user (id, name, email, role, createdAt, updatedAt) VALUES ('u1', 'Test User', 'user@mist.local', 'user', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO account (id, userId, accountId, providerId, password, createdAt, updatedAt) VALUES ('a1', 'u1', 'user@mist.local', 'credential', ?, datetime('now'), datetime('now'))`, hashed)
	if err != nil {
		t.Fatal(err)
	}

	// Bad password
	rec := authRequest(s, "POST", "/auth/sign-in/email", map[string]string{
		"email":    "user@mist.local",
		"password": "wrongpassword123",
	}, "")
	if rec.Code != 401 {
		t.Fatalf("expected 401 for wrong password, got %d", rec.Code)
	}

	// Non-existent user
	rec = authRequest(s, "POST", "/auth/sign-in/email", map[string]string{
		"email":    "nobody@mist.local",
		"password": "wrongpassword123",
	}, "")
	if rec.Code != 401 {
		t.Fatalf("expected 401 for non-existent user, got %d", rec.Code)
	}

	// Valid login
	rec = authRequest(s, "POST", "/auth/sign-in/email", map[string]string{
		"email":    "user@mist.local",
		"password": "initialpassword123",
	}, "")
	if rec.Code != 200 {
		t.Fatalf("expected 200 for valid login, got %d %s", rec.Code, rec.Body.String())
	}

	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "better-auth.session_token=") {
		t.Fatalf("missing better-auth.session_token cookie: %s", cookie)
	}

	// Parse session token from cookie
	var sessionToken string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "better-auth.session_token" {
			sessionToken = c.Value
		}
	}
	if sessionToken == "" {
		t.Fatal("empty session token")
	}

	// Get session
	rec = authRequest(s, "GET", "/auth/get-session", nil, "better-auth.session_token="+sessionToken)
	if rec.Code != 200 {
		t.Fatalf("expected 200 get-session, got %d", rec.Code)
	}
	var sessResp struct {
		User *Member `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sessResp); err != nil {
		t.Fatal(err)
	}
	if sessResp.User == nil || sessResp.User.ID != "u1" {
		t.Fatalf("unexpected session response: %+v", sessResp.User)
	}

	// Change password
	rec = authRequest(s, "POST", "/auth/change-password", map[string]any{
		"currentPassword":     "initialpassword123",
		"newPassword":         "newpassword12345",
		"revokeOtherSessions": true,
	}, "better-auth.session_token="+sessionToken)
	if rec.Code != 200 {
		t.Fatalf("expected 200 change-password, got %d %s", rec.Code, rec.Body.String())
	}

	// Sign out
	rec = authRequest(s, "POST", "/auth/sign-out", map[string]string{}, "better-auth.session_token="+sessionToken)
	if rec.Code != 200 {
		t.Fatalf("expected 200 sign-out, got %d", rec.Code)
	}

	// Get session after logout -> should be nil
	rec = authRequest(s, "GET", "/auth/get-session", nil, "better-auth.session_token="+sessionToken)
	if rec.Code != 200 {
		t.Fatalf("expected 200 get-session after logout, got %d", rec.Code)
	}
	json.Unmarshal(rec.Body.Bytes(), &sessResp)
	if sessResp.User != nil {
		t.Fatalf("expected nil user after logout, got %+v", sessResp.User)
	}
}

func TestAuthService_AdminUserManagement(t *testing.T) {
	s := newTestAuthService(t)

	// Seed admin and user
	hashed, _ := hashPassword("adminpassword123")
	_, _ = s.db.Exec(`INSERT INTO user (id, name, email, role, createdAt, updatedAt) VALUES ('admin1', 'Admin', 'admin@mist.local', 'admin', datetime('now'), datetime('now'))`)
	_, _ = s.db.Exec(`INSERT INTO account (id, userId, accountId, providerId, password, createdAt, updatedAt) VALUES ('a_admin', 'admin1', 'admin@mist.local', 'credential', ?, datetime('now'), datetime('now'))`, hashed)

	_, _ = s.db.Exec(`INSERT INTO user (id, name, email, role, createdAt, updatedAt) VALUES ('user1', 'User', 'user@mist.local', 'user', datetime('now'), datetime('now'))`)
	_, _ = s.db.Exec(`INSERT INTO account (id, userId, accountId, providerId, password, createdAt, updatedAt) VALUES ('a_user', 'user1', 'user@mist.local', 'credential', ?, datetime('now'), datetime('now'))`, hashed)

	seedTestSession(t, s, "admin1")
	seedTestSession(t, s, "user1")

	// Admin creates a new user
	rec := authRequest(s, "POST", "/auth/admin/create-user", map[string]string{
		"name":     "Bob",
		"email":    "bob@mist.local",
		"password": "bobpassword123",
		"role":     "user",
	}, "better-auth.session_token=test-session-admin1")
	if rec.Code != 200 {
		t.Fatalf("create-user failed: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		User Member `json:"user"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.User.Email != "bob@mist.local" {
		t.Fatalf("unexpected user created: %+v", created.User)
	}

	// Non-admin cannot create user
	rec = authRequest(s, "POST", "/auth/admin/create-user", map[string]string{
		"name":     "Mallory",
		"email":    "mallory@mist.local",
		"password": "mallorypassword123",
	}, "better-auth.session_token=test-session-user1")
	if rec.Code != 403 {
		t.Fatalf("expected 403 for non-admin create-user, got %d", rec.Code)
	}

	// List users
	rec = authRequest(s, "GET", "/auth/admin/list-users?limit=100", nil, "better-auth.session_token=test-session-admin1")
	if rec.Code != 200 {
		t.Fatalf("list-users failed: %d %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Users []Member `json:"users"`
		Total int      `json:"total"`
	}
	json.Unmarshal(rec.Body.Bytes(), &listResp)
	if listResp.Total < 3 || len(listResp.Users) < 3 {
		t.Fatalf("expected at least 3 users, got %d (total %d)", len(listResp.Users), listResp.Total)
	}

	// Reset password
	rec = authRequest(s, "POST", "/auth/admin/set-user-password", map[string]string{
		"userId":      created.User.ID,
		"newPassword": "newbobpassword123",
	}, "better-auth.session_token=test-session-admin1")
	if rec.Code != 200 {
		t.Fatalf("set-user-password failed: %d %s", rec.Code, rec.Body.String())
	}

	// Login with new password
	rec = authRequest(s, "POST", "/auth/sign-in/email", map[string]string{
		"email":    "bob@mist.local",
		"password": "newbobpassword123",
	}, "")
	if rec.Code != 200 {
		t.Fatalf("expected 200 for user login with new password, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAuthService_InternalMembers(t *testing.T) {
	token := "abcdef12345678901234567890123456"
	t.Setenv("MIST_INTERNAL_TOKEN", token)
	s := newTestAuthService(t)

	_, _ = s.db.Exec(`INSERT INTO user (id, name, email, role, createdAt, updatedAt) VALUES ('u1', 'Alice', 'alice@mist.local', 'user', datetime('now'), datetime('now'))`)
	_, _ = s.db.Exec(`INSERT INTO user (id, name, email, role, createdAt, updatedAt) VALUES ('u2', 'Bob', 'bob@mist.local', 'admin', datetime('now'), datetime('now'))`)

	req := httptest.NewRequest("GET", "/internal/members", nil)
	rec := httptest.NewRecorder()
	s.handleInternalMembers(rec, req)
	if rec.Code != 403 {
		t.Fatalf("expected 403 without token, got %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "/internal/members", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	s.handleInternalMembers(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 with token, got %d", rec.Code)
	}
	var body struct {
		Members []struct {
			ID     string `json:"id"`
			Active bool   `json:"active"`
			Role   string `json:"role"`
		} `json:"members"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(body.Members))
	}
}

// Fixture sessions exercise the same lookup as real sign-in, never user-ID cookies.
func seedTestSession(t *testing.T, service *AuthService, id string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := service.db.Exec(`INSERT INTO session (id,token,userId,expiresAt,createdAt,updatedAt) VALUES (?,?,?,?,?,?)`, "session-"+id, "test-session-"+id, id, now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuthServiceRejectsUserIDAndExpiredSessionCookies(t *testing.T) {
	service := newTestAuthService(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := service.db.Exec(`INSERT INTO user (id,name,email,role,createdAt,updatedAt) VALUES (?,?,?,?,?,?)`, "known-user", "Known", "known@mist.local", "admin", now, now)
	if err != nil {
		t.Fatal(err)
	}
	seedTestSession(t, service, "known-user")
	for _, cookie := range []string{"identity=known-user", "member=known-user", "session_token=known-user", "better-auth.session_token=known-user"} {
		request := httptest.NewRequest("GET", "/session", nil)
		request.Header.Set("Cookie", cookie)
		member, err := service.session(request)
		if err != nil || member != nil {
			t.Fatalf("forged identity cookie accepted: %v", err)
		}
		response := authRequest(service, "GET", "/auth/get-session", nil, cookie)
		var body struct {
			User *Member `json:"user"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.User != nil {
			t.Fatal("session endpoint accepted a forged identity")
		}
	}
	if _, err := service.db.Exec(`UPDATE session SET expiresAt=?`, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/session", nil)
	request.Header.Set("Cookie", "better-auth.session_token=test-session-known-user")
	if member, err := service.session(request); err != nil || member != nil {
		t.Fatalf("expired session accepted: %v", err)
	}
}

func TestBetterAuthPasswordCompatibility(t *testing.T) {
	// Public test vector generated with Better Auth's Node scrypt parameters.
	const hash = "0123456789abcdef0123456789abcdef:8582d1a409fb81869c9f080d49ce2d8839d611a9905642ce9e7b9e5c1cf7e7b71add2af990c5095edf0733f174780d62b2f7ff2fb42cd07a5edf561bef26dc34"
	if !verifyPassword(hash, "legacy-password-123") || verifyPassword(hash, "wrong-password") {
		t.Fatal("existing Better Auth password compatibility is broken")
	}
}

func TestDeactivatedAccountsCannotUseExistingSessions(t *testing.T) {
	service := newTestAuthService(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := service.db.Exec(`INSERT INTO user (id,name,email,role,banned,createdAt,updatedAt) VALUES (?,?,?,?,?,?,?)`, "inactive", "Inactive", "inactive@mist.local", "admin", 1, now, now)
	if err != nil {
		t.Fatal(err)
	}
	seedTestSession(t, service, "inactive")
	request := httptest.NewRequest("GET", "/session", nil)
	request.Header.Set("Cookie", "better-auth.session_token=test-session-inactive")
	if member, err := service.session(request); err != nil || member != nil {
		t.Fatal("deactivated account retained access")
	}
	active, err := service.activeMembers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := active["inactive"]; exists {
		t.Fatal("deactivated account can keep running jobs")
	}
	if _, err := service.LookupMember(context.Background(), "inactive"); err == nil {
		t.Fatal("deactivated account can be enrolled")
	}
	response := authRequest(service, "GET", "/auth/get-session", nil, "better-auth.session_token=test-session-inactive")
	var body struct {
		User *Member `json:"user"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.User != nil {
		t.Fatal("session endpoint exposes deactivated account")
	}
}

func TestExistingBetterAuthSchemaCanCreateAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE user (id TEXT PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL UNIQUE, emailVerified INTEGER NOT NULL, createdAt TEXT NOT NULL, updatedAt TEXT NOT NULL, role TEXT)`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIST_ADMIN_EMAIL", "existing-schema@mist.local")
	t.Setenv("MIST_ADMIN_PASSWORD", "test-password-123")
	service, err := newAuthService(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	login := authRequest(service, "POST", "/auth/sign-in/email", map[string]string{"email": "existing-schema@mist.local", "password": "test-password-123"}, "")
	if login.Code != 200 {
		t.Fatal("bootstrap must support old NOT NULL columns")
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	created := authRequest(service, "POST", "/auth/admin/create-user", map[string]string{"name": "New member", "email": "new-schema@mist.local", "password": "test-password-456"}, "better-auth.session_token="+body.Token)
	if created.Code != 200 {
		t.Fatalf("member creation failed against existing schema: %s", created.Body)
	}
}
