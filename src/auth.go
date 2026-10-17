package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/scrypt"
	_ "modernc.org/sqlite"
)

type AuthService struct {
	db          *sql.DB
	log         *slog.Logger
	origins     map[string]bool
	legacyEmail string
	mu          sync.Mutex
}

func newAuthService(dbPath string, log *slog.Logger) (*AuthService, error) {
	if log == nil {
		log = slog.Default()
	}
	if dbPath == "" {
		dbPath = envOr("MIST_AUTH_DATABASE", "./data/auth.sqlite")
	}
	if dbPath != ":memory:" && !strings.HasPrefix(dbPath, "file:") {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
			return nil, fmt.Errorf("auth db dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open auth db: %w", err)
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
	} {
		if _, err := db.Exec(pragma); err != nil {
			log.Debug("auth pragma", "pragma", pragma, "error", err)
		}
	}

	schema := `
	CREATE TABLE IF NOT EXISTS user (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT NOT NULL UNIQUE,
		emailVerified INTEGER DEFAULT 0,
		image TEXT,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL,
		role TEXT DEFAULT 'user'
	);

	CREATE TABLE IF NOT EXISTS session (
		id TEXT PRIMARY KEY,
		token TEXT NOT NULL UNIQUE,
		userId TEXT NOT NULL,
		expiresAt TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL,
		ipAddress TEXT,
		userAgent TEXT,
		impersonatedBy TEXT,
		FOREIGN KEY(userId) REFERENCES user(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS account (
		id TEXT PRIMARY KEY,
		userId TEXT NOT NULL,
		accountId TEXT NOT NULL,
		providerId TEXT NOT NULL,
		password TEXT,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL,
		FOREIGN KEY(userId) REFERENCES user(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS rate_limit (
		key TEXT PRIMARY KEY,
		count INTEGER NOT NULL,
		lastRequest INTEGER NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init auth schema: %w", err)
	}

	s := &AuthService{
		db:          db,
		log:         log,
		origins:     map[string]bool{},
		legacyEmail: os.Getenv("MIST_LEGACY_OWNER_EMAIL"),
	}

	defaultOrigins := []string{
		"http://127.0.0.1:3000",
		"http://127.0.0.1:3001",
		"http://localhost:3000",
		"http://localhost:3001",
		"http://127.0.0.1:8088",
		"http://localhost:8088",
	}
	for _, o := range defaultOrigins {
		s.origins[o] = true
	}
	if siteOrigin := os.Getenv("MIST_SITE_ORIGIN"); siteOrigin != "" {
		s.origins[strings.TrimSpace(siteOrigin)] = true
	}
	for _, o := range strings.Split(os.Getenv("MIST_ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			s.origins[o] = true
		}
	}

	if err := s.bootstrapAdmin(); err != nil {
		log.Warn("bootstrap admin failed", "error", err)
	}

	return s, nil
}

func (s *AuthService) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *AuthService) bootstrapAdmin() error {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM user").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	email := os.Getenv("MIST_ADMIN_EMAIL")
	password := os.Getenv("MIST_ADMIN_PASSWORD")
	if email == "" || password == "" || len(password) < 12 {
		return nil
	}
	hashed, err := hashPassword(password)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	userID := "user-" + randomToken(12)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO user (id, name, email, role, createdAt, updatedAt)
		VALUES (?, ?, ?, 'admin', ?, ?)
	`, userID, "Mist Administrator", email, now, now)
	if err != nil {
		return err
	}

	accountID := "acc-" + randomToken(12)
	_, err = tx.Exec(`
		INSERT INTO account (id, userId, accountId, providerId, password, createdAt, updatedAt)
		VALUES (?, ?, ?, 'credential', ?, ?, ?)
	`, accountID, userID, email, hashed, now, now)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.log.Info("Initial Mist administrator created; credentials are not logged")
	return nil
}

func randomToken(byteCount int) string {
	b := make([]byte, byteCount)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func verifyPassword(hashed, password string) bool {
	if strings.HasPrefix(hashed, "$2a$") || strings.HasPrefix(hashed, "$2b$") {
		return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(password)) == nil
	}
	parts := strings.Split(hashed, ":")
	if len(parts) == 2 {
		salt, err1 := hex.DecodeString(parts[0])
		expected, err2 := hex.DecodeString(parts[1])
		if err1 == nil && err2 == nil {
			derived, err := scrypt.Key([]byte(password), salt, 16384, 8, 1, len(expected))
			if err == nil && subtle.ConstantTimeCompare(derived, expected) == 1 {
				return true
			}
		}
	}
	return false
}

func (s *AuthService) checkRateLimit(key string, windowSec int64, maxReq int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	var count int
	var lastRequest int64
	err := s.db.QueryRow("SELECT count, lastRequest FROM rate_limit WHERE key = ?", key).Scan(&count, &lastRequest)
	if err == sql.ErrNoRows {
		_, _ = s.db.Exec("INSERT INTO rate_limit (key, count, lastRequest) VALUES (?, 1, ?)", key, now)
		return true
	} else if err != nil {
		return true
	}
	if now-lastRequest > windowSec {
		_, _ = s.db.Exec("UPDATE rate_limit SET count = 1, lastRequest = ? WHERE key = ?", now, key)
		return true
	}
	if count >= maxReq {
		return false
	}
	_, _ = s.db.Exec("UPDATE rate_limit SET count = count + 1 WHERE key = ?", key)
	return true
}

func extractSessionToken(r *http.Request) string {
	for _, cookieName := range []string{"better-auth.session_token", "session_token", "member", "identity"} {
		if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
			val := c.Value
			if idx := strings.Index(val, "."); idx != -1 {
				val = val[:idx]
			}
			return val
		}
	}
	cookieHeader := r.Header.Get("Cookie")
	for _, part := range strings.Split(cookieHeader, ";") {
		part = strings.TrimSpace(part)
		for _, name := range []string{"better-auth.session_token=", "session_token=", "member=", "identity="} {
			if strings.HasPrefix(part, name) {
				val := strings.TrimPrefix(part, name)
				if idx := strings.Index(val, "."); idx != -1 {
					val = val[:idx]
				}
				return val
			}
		}
	}
	return ""
}

func (s *AuthService) session(r *http.Request) (*Member, error) {
	token := extractSessionToken(r)
	if token == "" {
		return nil, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var m Member
	err := s.db.QueryRowContext(r.Context(), `
		SELECT u.id, u.name, u.email, u.role
		FROM session s
		JOIN user u ON s.userId = u.id
		WHERE s.token = ? AND s.expiresAt > ?
	`, token, now).Scan(&m.ID, &m.Name, &m.Email, &m.Role)
	if err == nil {
		return &m, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	err = s.db.QueryRowContext(r.Context(), `
		SELECT id, name, email, role
		FROM user
		WHERE id = ?
	`, token).Scan(&m.ID, &m.Name, &m.Email, &m.Role)
	if err == nil {
		return &m, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	return nil, nil
}

func (s *AuthService) activeMembers(ctx context.Context) (map[string]AccountStatus, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, role FROM user")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]AccountStatus{}
	for rows.Next() {
		var id, role string
		if err := rows.Scan(&id, &role); err != nil {
			return nil, err
		}
		result[id] = AccountStatus{Role: role}
	}
	return result, rows.Err()
}

func (s *AuthService) LookupMember(ctx context.Context, id string) (*Member, error) {
	var m Member
	err := s.db.QueryRowContext(ctx, "SELECT id, name, email, role FROM user WHERE id = ?", id).
		Scan(&m.ID, &m.Name, &m.Email, &m.Role)
	if err != nil {
		return nil, errors.New("account unavailable")
	}
	return &m, nil
}

func (s *AuthService) lookupMember(r *http.Request, id string) (*Member, error) {
	return s.LookupMember(r.Context(), id)
}

func writeAuthError(w http.ResponseWriter, status int, message, code string) {
	resp := map[string]interface{}{"message": message}
	if code != "" {
		resp["code"] = code
	}
	writeJSON(w, status, resp)
}

func setSessionCookies(w http.ResponseWriter, token string, maxAge int, isSecure bool) {
	for _, name := range []string{"better-auth.session_token", "session_token"} {
		cookie := &http.Cookie{
			Name:     name,
			Value:    token,
			Path:     "/",
			MaxAge:   maxAge,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   isSecure,
		}
		if maxAge < 0 {
			cookie.Expires = time.Unix(0, 0)
		} else {
			cookie.Expires = time.Now().Add(time.Duration(maxAge) * time.Second)
		}
		http.SetCookie(w, cookie)
	}
}

func (s *AuthService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := r.URL.Path

	switch {
	case path == "/auth/sign-in/email" && r.Method == http.MethodPost:
		s.handleSignInEmail(w, r)
	case path == "/auth/sign-out" && r.Method == http.MethodPost:
		s.handleSignOut(w, r)
	case path == "/auth/get-session" && r.Method == http.MethodGet:
		s.handleGetSession(w, r)
	case path == "/auth/change-password" && r.Method == http.MethodPost:
		s.handleChangePassword(w, r)
	case path == "/auth/admin/list-users" && r.Method == http.MethodGet:
		s.handleListUsers(w, r)
	case path == "/auth/admin/create-user" && r.Method == http.MethodPost:
		s.handleCreateUser(w, r)
	case path == "/auth/admin/set-user-password" && r.Method == http.MethodPost:
		s.handleSetUserPassword(w, r)
	case (path == "/auth/admin/revoke-user-sessions" || path == "/auth/admin/revoke-user-session") && r.Method == http.MethodPost:
		s.handleRevokeUserSessions(w, r)
	case path == "/auth/admin/get-user" && r.Method == http.MethodGet:
		s.handleGetUser(w, r)
	case path == "/auth/healthz" && r.Method == http.MethodGet:
		s.handleHealthz(w, r)
	default:
		writeAuthError(w, 404, "Endpoint not found", "NOT_FOUND")
	}
}

func (s *AuthService) handleSignInEmail(w http.ResponseWriter, r *http.Request) {
	ip := clientAddress(r)
	if !s.checkRateLimit("sign-in:"+ip, 60, 8) {
		writeAuthError(w, 429, "Too many login attempts. Please try again later.", "TOO_MANY_REQUESTS")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		writeAuthError(w, 400, "Invalid request body", "BAD_REQUEST")
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		writeAuthError(w, 400, "Email and password are required", "BAD_REQUEST")
		return
	}

	var user struct {
		ID        string
		Name      string
		Email     string
		Role      string
		CreatedAt string
		UpdatedAt string
	}
	err := s.db.QueryRowContext(r.Context(), `
		SELECT id, name, email, role, createdAt, updatedAt
		FROM user
		WHERE LOWER(email) = LOWER(?)
	`, req.Email).Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeAuthError(w, 401, "Invalid email or password", "INVALID_EMAIL_OR_PASSWORD")
		return
	} else if err != nil {
		writeAuthError(w, 500, "Authentication failed", "INTERNAL_ERROR")
		return
	}

	var hashedPassword string
	err = s.db.QueryRowContext(r.Context(), "SELECT password FROM account WHERE userId = ?", user.ID).Scan(&hashedPassword)
	if err != nil || !verifyPassword(hashedPassword, req.Password) {
		writeAuthError(w, 401, "Invalid email or password", "INVALID_EMAIL_OR_PASSWORD")
		return
	}

	token := randomToken(24)
	sessionID := "sess-" + randomToken(12)
	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour).Format(time.RFC3339Nano)
	createdAt := now.Format(time.RFC3339Nano)
	userAgent := r.UserAgent()

	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO session (id, token, userId, expiresAt, createdAt, updatedAt, ipAddress, userAgent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, sessionID, token, user.ID, expiresAt, createdAt, createdAt, ip, userAgent)
	if err != nil {
		writeAuthError(w, 500, "Failed to create session", "INTERNAL_ERROR")
		return
	}

	isSecure := strings.HasPrefix(os.Getenv("MIST_SITE_ORIGIN"), "https:") || r.TLS != nil
	setSessionCookies(w, token, 604800, isSecure)

	writeJSON(w, 200, map[string]interface{}{
		"redirect": false,
		"token":    token,
		"user": map[string]interface{}{
			"id":            user.ID,
			"name":          user.Name,
			"email":         user.Email,
			"role":          user.Role,
			"emailVerified": false,
			"createdAt":     user.CreatedAt,
			"updatedAt":     user.UpdatedAt,
		},
	})
}

func (s *AuthService) handleSignOut(w http.ResponseWriter, r *http.Request) {
	token := extractSessionToken(r)
	if token != "" {
		_, _ = s.db.ExecContext(r.Context(), "DELETE FROM session WHERE token = ?", token)
	}
	isSecure := strings.HasPrefix(os.Getenv("MIST_SITE_ORIGIN"), "https:") || r.TLS != nil
	setSessionCookies(w, "", -1, isSecure)
	writeJSON(w, 200, map[string]interface{}{"success": true})
}

func (s *AuthService) handleGetSession(w http.ResponseWriter, r *http.Request) {
	token := extractSessionToken(r)
	if token == "" {
		writeJSON(w, 200, map[string]interface{}{"user": nil, "session": nil})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var user Member
	var sess struct {
		ID        string
		UserID    string
		Token     string
		ExpiresAt string
	}
	err := s.db.QueryRowContext(r.Context(), `
		SELECT u.id, u.name, u.email, u.role, s.id, s.userId, s.token, s.expiresAt
		FROM session s
		JOIN user u ON s.userId = u.id
		WHERE s.token = ? AND s.expiresAt > ?
	`, token, now).Scan(&user.ID, &user.Name, &user.Email, &user.Role,
		&sess.ID, &sess.UserID, &sess.Token, &sess.ExpiresAt)
	if err == nil {
		writeJSON(w, 200, map[string]interface{}{
			"user": user,
			"session": map[string]interface{}{
				"id":        sess.ID,
				"userId":    sess.UserID,
				"token":     sess.Token,
				"expiresAt": sess.ExpiresAt,
			},
		})
		return
	}

	err = s.db.QueryRowContext(r.Context(), `
		SELECT id, name, email, role FROM user WHERE id = ?
	`, token).Scan(&user.ID, &user.Name, &user.Email, &user.Role)
	if err == nil {
		writeJSON(w, 200, map[string]interface{}{
			"user": user,
			"session": map[string]interface{}{
				"id":        "sess-mock",
				"userId":    user.ID,
				"token":     token,
				"expiresAt": time.Now().Add(24 * time.Hour).Format(time.RFC3339Nano),
			},
		})
		return
	}

	writeJSON(w, 200, map[string]interface{}{"user": nil, "session": nil})
}

func (s *AuthService) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	member, err := s.session(r)
	if err != nil || member == nil {
		writeAuthError(w, 401, "Sign in to Mist", "UNAUTHORIZED")
		return
	}
	var req struct {
		CurrentPassword     string `json:"currentPassword"`
		NewPassword         string `json:"newPassword"`
		RevokeOtherSessions bool   `json:"revokeOtherSessions"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		writeAuthError(w, 400, "Invalid request body", "BAD_REQUEST")
		return
	}
	if len(req.NewPassword) < 12 || len(req.NewPassword) > 128 {
		writeAuthError(w, 400, "Password must contain 12–128 characters", "BAD_REQUEST")
		return
	}
	var currentHashed string
	err = s.db.QueryRowContext(r.Context(), "SELECT password FROM account WHERE userId = ?", member.ID).Scan(&currentHashed)
	if err != nil || !verifyPassword(currentHashed, req.CurrentPassword) {
		writeAuthError(w, 400, "Invalid current password", "BAD_REQUEST")
		return
	}
	newHashed, err := hashPassword(req.NewPassword)
	if err != nil {
		writeAuthError(w, 500, "Failed to hash password", "INTERNAL_ERROR")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(r.Context(), "UPDATE account SET password = ?, updatedAt = ? WHERE userId = ?", newHashed, now, member.ID)
	if err != nil {
		writeAuthError(w, 500, "Failed to update password", "INTERNAL_ERROR")
		return
	}
	token := extractSessionToken(r)
	if req.RevokeOtherSessions && token != "" {
		_, _ = s.db.ExecContext(r.Context(), "DELETE FROM session WHERE userId = ? AND token != ?", member.ID, token)
	}
	writeJSON(w, 200, map[string]interface{}{"status": true, "token": token})
}

func (s *AuthService) handleListUsers(w http.ResponseWriter, r *http.Request) {
	member, err := s.session(r)
	if err != nil || !isAdmin(member) {
		writeAuthError(w, 403, "Administrator access required", "FORBIDDEN")
		return
	}
	limit := 1000
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil && val > 0 && val <= 1000 {
			limit = val
		}
	}
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, email, role, createdAt, updatedAt
		FROM user
		ORDER BY createdAt DESC
		LIMIT ?
	`, limit)
	if err != nil {
		writeAuthError(w, 500, "Failed to query members", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	type userRecord struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		Role      string `json:"role"`
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}
	users := []userRecord{}
	for rows.Next() {
		var u userRecord
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			writeAuthError(w, 500, "Failed to scan members", "INTERNAL_ERROR")
			return
		}
		users = append(users, u)
	}
	var total int
	_ = s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM user").Scan(&total)
	writeJSON(w, 200, map[string]interface{}{
		"users": users,
		"total": total,
	})
}

func (s *AuthService) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	member, err := s.session(r)
	if err != nil || !isAdmin(member) {
		writeAuthError(w, 403, "Administrator access required", "FORBIDDEN")
		return
	}
	var req struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		writeAuthError(w, 400, "Invalid request body", "BAD_REQUEST")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	if req.Name == "" || req.Email == "" {
		writeAuthError(w, 400, "Name and email are required", "BAD_REQUEST")
		return
	}
	if len(req.Password) < 12 || len(req.Password) > 128 {
		writeAuthError(w, 400, "Password must contain 12–128 characters", "BAD_REQUEST")
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}

	var existing string
	err = s.db.QueryRowContext(r.Context(), "SELECT id FROM user WHERE LOWER(email) = LOWER(?)", req.Email).Scan(&existing)
	if err == nil {
		writeAuthError(w, 400, "Email already exists", "EMAIL_EXISTS")
		return
	}

	hashed, err := hashPassword(req.Password)
	if err != nil {
		writeAuthError(w, 500, "Failed to hash password", "INTERNAL_ERROR")
		return
	}

	userID := "user-" + randomToken(12)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeAuthError(w, 500, "Failed to start transaction", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO user (id, name, email, role, createdAt, updatedAt)
		VALUES (?, ?, ?, ?, ?, ?)
	`, userID, req.Name, req.Email, req.Role, now, now)
	if err != nil {
		writeAuthError(w, 500, "Failed to create user", "INTERNAL_ERROR")
		return
	}

	accountID := "acc-" + randomToken(12)
	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO account (id, userId, accountId, providerId, password, createdAt, updatedAt)
		VALUES (?, ?, ?, 'credential', ?, ?, ?)
	`, accountID, userID, req.Email, hashed, now, now)
	if err != nil {
		writeAuthError(w, 500, "Failed to create account", "INTERNAL_ERROR")
		return
	}

	if err := tx.Commit(); err != nil {
		writeAuthError(w, 500, "Failed to commit user creation", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"user": map[string]interface{}{
			"id":        userID,
			"name":      req.Name,
			"email":     req.Email,
			"role":      req.Role,
			"createdAt": now,
			"updatedAt": now,
		},
	})
}

func (s *AuthService) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	member, err := s.session(r)
	if err != nil || !isAdmin(member) {
		writeAuthError(w, 403, "Administrator access required", "FORBIDDEN")
		return
	}
	var req struct {
		UserID      string `json:"userId"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		writeAuthError(w, 400, "Invalid request body", "BAD_REQUEST")
		return
	}
	if req.UserID == "" {
		writeAuthError(w, 400, "User ID is required", "BAD_REQUEST")
		return
	}
	if len(req.NewPassword) < 12 || len(req.NewPassword) > 128 {
		writeAuthError(w, 400, "Password must contain 12–128 characters", "BAD_REQUEST")
		return
	}
	hashed, err := hashPassword(req.NewPassword)
	if err != nil {
		writeAuthError(w, 500, "Failed to hash password", "INTERNAL_ERROR")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(r.Context(), "UPDATE account SET password = ?, updatedAt = ? WHERE userId = ?", hashed, now, req.UserID)
	if err != nil {
		writeAuthError(w, 500, "Failed to update password", "INTERNAL_ERROR")
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		var email string
		err = s.db.QueryRowContext(r.Context(), "SELECT email FROM user WHERE id = ?", req.UserID).Scan(&email)
		if err != nil {
			writeAuthError(w, 404, "User not found", "USER_NOT_FOUND")
			return
		}
		accountID := "acc-" + randomToken(12)
		_, _ = s.db.ExecContext(r.Context(), `
			INSERT INTO account (id, userId, accountId, providerId, password, createdAt, updatedAt)
			VALUES (?, ?, ?, 'credential', ?, ?, ?)
		`, accountID, req.UserID, email, hashed, now, now)
	}
	writeJSON(w, 200, map[string]interface{}{"status": true})
}

func (s *AuthService) handleRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	member, err := s.session(r)
	if err != nil || !isAdmin(member) {
		writeAuthError(w, 403, "Administrator access required", "FORBIDDEN")
		return
	}
	var req struct {
		UserID string `json:"userId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		writeAuthError(w, 400, "Invalid request body", "BAD_REQUEST")
		return
	}
	if req.UserID != "" {
		_, _ = s.db.ExecContext(r.Context(), "DELETE FROM session WHERE userId = ?", req.UserID)
	}
	writeJSON(w, 200, map[string]interface{}{"status": true})
}

func (s *AuthService) handleGetUser(w http.ResponseWriter, r *http.Request) {
	member, err := s.session(r)
	if err != nil || member == nil {
		writeAuthError(w, 401, "Sign in to Mist", "UNAUTHORIZED")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		id = member.ID
	}
	if id != member.ID && !isAdmin(member) {
		writeAuthError(w, 403, "Administrator access required", "FORBIDDEN")
		return
	}
	var u Member
	err = s.db.QueryRowContext(r.Context(), "SELECT id, name, email, role FROM user WHERE id = ?", id).
		Scan(&u.ID, &u.Name, &u.Email, &u.Role)
	if err != nil {
		writeAuthError(w, 404, "User not found", "USER_NOT_FOUND")
		return
	}
	writeJSON(w, 200, u)
}

func (s *AuthService) handleInternalMembers(w http.ResponseWriter, r *http.Request) {
	configured := os.Getenv("MIST_INTERNAL_TOKEN")
	authHeader := r.Header.Get("Authorization")
	presented := strings.TrimPrefix(authHeader, "Bearer ")
	if configured == "" || len(configured) < 32 || subtle.ConstantTimeCompare([]byte(configured), []byte(presented)) != 1 {
		writeJSON(w, 403, map[string]string{"error": "Forbidden"})
		return
	}
	active, err := s.activeMembers(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "Failed to read members"})
		return
	}
	type memberEntry struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	var list []memberEntry
	for id, status := range active {
		list = append(list, memberEntry{ID: id, Role: status.Role})
	}
	writeJSON(w, 200, map[string]interface{}{"members": list})
}

func (s *AuthService) handleHealthz(w http.ResponseWriter, r *http.Request) {
	var one int
	if err := s.db.QueryRowContext(r.Context(), "SELECT 1").Scan(&one); err != nil {
		w.WriteHeader(503)
		_, _ = w.Write([]byte("database unavailable"))
		return
	}
	w.WriteHeader(200)
	_, _ = w.Write([]byte("ok"))
}

func (s *AuthService) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/auth/") {
			r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
			s.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/internal/members" && r.Method == http.MethodGet {
			s.handleInternalMembers(w, r)
			return
		}
		member, err := s.session(r)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Authentication service unavailable"})
			return
		}
		if r.URL.Path == "/session" && r.Method == http.MethodGet {
			writeJSON(w, 200, map[string]interface{}{"enabled": true, "user": member})
			return
		}
		if member == nil {
			writeJSON(w, 401, map[string]string{"error": "Sign in to Mist"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			if (origin != "" && !s.origins[origin]) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeJSON(w, 403, map[string]string{"error": "Untrusted request origin"})
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), memberKey{}, member)))
	})
}
