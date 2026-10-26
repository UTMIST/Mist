package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const mistyCookie = "misty_login"

var mistyID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var mistyCode = regexp.MustCompile(`^[0-9]{6}$`)
var mistyEmail = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
var errMistyUnavailable = errors.New("Misty authentication unavailable")

type mistyStatus struct {
	mu     sync.Mutex
	until  time.Time
	active bool
	failed bool
}

type mistyClient struct {
	baseURL  string
	issuer   string
	origin   string
	apiKey   string
	http     *http.Client
	mu       sync.Mutex
	statuses map[string]*mistyStatus
}

func newMistyClient() (*mistyClient, error) {
	mode := envOr("MIST_AUTH_MODE", "local")
	if mode == "local" {
		return nil, nil
	}
	if mode != "misty" {
		return nil, errors.New("MIST_AUTH_MODE must be local or misty")
	}
	base := strings.TrimRight(os.Getenv("MISTY_GATEWAY_URL"), "/")
	environment := os.Getenv("MISTY_ENVIRONMENT")
	if environment != "local" && environment != "staging" && environment != "production" {
		return nil, errors.New("Misty login requires MISTY_ENVIRONMENT=local, staging, or production")
	}
	key := os.Getenv("MISTY_GATEWAY_API_KEY")
	if !safeMistyURL(base) || !safeMistyURL(os.Getenv("MIST_SITE_ORIGIN")) || !strings.HasPrefix(key, "gw_") {
		return nil, errors.New("Misty login requires MISTY_GATEWAY_URL and MIST_SITE_ORIGIN over HTTPS (HTTP only on loopback), and a scoped MISTY_GATEWAY_API_KEY")
	}
	return &mistyClient{baseURL: base, issuer: "misty:" + environment, origin: strings.TrimRight(os.Getenv("MIST_SITE_ORIGIN"), "/"), apiKey: key, statuses: map[string]*mistyStatus{}, http: &http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func safeMistyURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))
}

// Never return transport errors or upstream bodies: they can contain credentials.
func (c *mistyClient) request(ctx context.Context, method, path string, body any, out any) (int, error) {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, errMistyUnavailable
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, input)
	if err != nil {
		return 0, errMistyUnavailable
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return 0, errMistyUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 && out != nil {
		data, err := io.ReadAll(io.LimitReader(response.Body, 8193))
		var identity struct {
			Issuer string `json:"issuer"`
		}
		if err != nil || len(data) > 8192 || json.Unmarshal(data, &identity) != nil || identity.Issuer != c.issuer || json.Unmarshal(data, out) != nil {
			return 0, errMistyUnavailable
		}
	}
	return response.StatusCode, nil
}

func (c *mistyClient) active(ctx context.Context, personID string) (bool, error) {
	if !mistyID.MatchString(personID) {
		return false, errMistyUnavailable
	}
	c.mu.Lock()
	entry := c.statuses[personID]
	if entry == nil {
		if len(c.statuses) >= 4096 {
			clear(c.statuses)
		}
		entry = &mistyStatus{}
		c.statuses[personID] = entry
	}
	c.mu.Unlock()
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if time.Now().Before(entry.until) {
		if entry.failed {
			return false, errMistyUnavailable
		}
		return entry.active, nil
	}
	var result struct {
		Active *bool `json:"active"`
	}
	status, err := c.request(ctx, "GET", "/v1/auth/people/"+personID+"/status", nil, &result)
	if err != nil || status != 200 || result.Active == nil {
		entry.failed = true
		entry.until = time.Now().Add(5 * time.Second)
		return false, errMistyUnavailable
	}
	entry.active = *result.Active
	entry.failed = false
	entry.until = time.Now().Add(30 * time.Second)
	return entry.active, nil
}

func (s *AuthService) initMisty() error {
	client, err := newMistyClient()
	if err != nil {
		return err
	}
	s.misty = client
	// Always migrate; switching modes must not erase identity/session provenance.
	_, err = s.db.Exec(`CREATE TABLE IF NOT EXISTS misty_identity (
  person_id TEXT PRIMARY KEY, user_id TEXT NOT NULL UNIQUE REFERENCES user(id)
 );
 CREATE TABLE IF NOT EXISTS misty_login (
  binding_hash TEXT NOT NULL, challenge_id TEXT NOT NULL, email TEXT NOT NULL,
  expires_at INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, in_flight INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(binding_hash,challenge_id)
 );
 CREATE INDEX IF NOT EXISTS misty_login_expiry ON misty_login(expires_at);
 CREATE TABLE IF NOT EXISTS misty_legacy_owner (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1), user_id TEXT NOT NULL REFERENCES user(id)
 );
 CREATE TABLE IF NOT EXISTS misty_issuer (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1), issuer TEXT NOT NULL, gateway_url TEXT NOT NULL
 );`)
	if err != nil {
		return fmt.Errorf("initialize Misty login tables: %w", err)
	}
	var exists int
	if err = s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('session') WHERE name='mistyPersonId'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		_, err = s.db.Exec(`ALTER TABLE session ADD COLUMN mistyPersonId TEXT`)
	}
	if err != nil {
		return err
	}
	if client != nil {
		if err := s.pinMistyIssuerAndAccounts(client); err != nil {
			return err
		}
	}
	err = s.db.QueryRow(`SELECT user_id FROM misty_legacy_owner WHERE singleton=1`).Scan(&s.legacyUserID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if client == nil || s.legacyEmail == "" {
		return nil
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*),COALESCE(min(id),'') FROM user WHERE lower(email)=lower(?)`, s.legacyEmail).Scan(&count, &s.legacyUserID); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("MIST_LEGACY_OWNER_EMAIL must match one existing compute account before enabling Misty login")
	}
	_, err = s.db.Exec(`INSERT INTO misty_legacy_owner(singleton,user_id) VALUES(1,?)`, s.legacyUserID)
	return err
}

// Pin this database to one gateway and issuer. Account migration is an explicit
// operator mapping of immutable IDs, never proof inferred from a recycled email.
func (s *AuthService) pinMistyIssuerAndAccounts(client *mistyClient) error {
	// Check the durable origin before sending any credential to a newly configured
	// URL. The transaction below checks again before changing identity state.
	var pinnedIssuer, pinnedGateway string
	err := s.db.QueryRow(`SELECT issuer,gateway_url FROM misty_issuer WHERE singleton=1`).Scan(&pinnedIssuer, &pinnedGateway)
	if err == nil && (pinnedIssuer != client.issuer || pinnedGateway != client.baseURL) {
		return errors.New("auth database is pinned to a different Misty issuer or gateway; use a separate database")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	status, err := client.request(context.Background(), "GET", "/v1/auth/issuer", nil, &struct{}{})
	if err != nil || status != 200 {
		return errors.New("cannot verify the configured Misty issuer")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var issuer, gateway string
	err = tx.QueryRow(`SELECT issuer,gateway_url FROM misty_issuer WHERE singleton=1`).Scan(&issuer, &gateway)
	if errors.Is(err, sql.ErrNoRows) {
		var unbound int
		if err = tx.QueryRow(`SELECT (SELECT count(*) FROM misty_identity)+(SELECT count(*) FROM session WHERE COALESCE(mistyPersonId,'')<>'')`).Scan(&unbound); err != nil {
			return err
		}
		if unbound != 0 {
			return errors.New("existing Misty identities require an explicit issuer migration")
		}
		if _, err = tx.Exec(`INSERT INTO misty_issuer(singleton,issuer,gateway_url) VALUES(1,?,?)`, client.issuer, client.baseURL); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if issuer != client.issuer || gateway != client.baseURL {
		return errors.New("auth database is pinned to a different Misty issuer or gateway; use a separate database")
	}
	if raw := os.Getenv("MISTY_ACCOUNT_LINKS"); raw != "" {
		var links map[string]string
		if json.Unmarshal([]byte(raw), &links) != nil || links == nil {
			return errors.New("MISTY_ACCOUNT_LINKS must map Misty person UUIDs to existing compute user IDs")
		}
		for personID, userID := range links {
			if !mistyID.MatchString(personID) || userID == "" {
				return errors.New("invalid MISTY_ACCOUNT_LINKS mapping")
			}
			var count int
			if err = tx.QueryRow(`SELECT count(*) FROM user WHERE id=?`, userID).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return errors.New("MISTY_ACCOUNT_LINKS references an unknown compute account")
			}
			if _, err = tx.Exec(`INSERT INTO misty_identity(person_id,user_id) VALUES(?,?) ON CONFLICT(person_id) DO NOTHING`, personID, userID); err != nil {
				return errors.New("conflicting MISTY_ACCOUNT_LINKS mapping")
			}
			var linked string
			if err = tx.QueryRow(`SELECT user_id FROM misty_identity WHERE person_id=?`, personID).Scan(&linked); err != nil {
				return err
			}
			if linked != userID {
				return errors.New("MISTY_ACCOUNT_LINKS cannot overwrite an existing identity")
			}
		}
	}
	return tx.Commit()
}

func (s *AuthService) mistyGuard(w http.ResponseWriter, r *http.Request) bool {
	if s.misty == nil {
		writeAuthError(w, 404, "Endpoint not found", "NOT_FOUND")
		return false
	}
	if r.Header.Get("Origin") != s.misty.origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeAuthError(w, 403, "Untrusted request origin", "FORBIDDEN")
		return false
	}
	return true
}

func decodeMisty(w http.ResponseWriter, r *http.Request, out any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeAuthError(w, 400, "Invalid login request", "BAD_REQUEST")
		return false
	}
	return true
}

func browserBinding(r *http.Request) (string, string) {
	cookie, err := r.Cookie(mistyCookie)
	if err != nil || len(cookie.Value) != 64 {
		return "", ""
	}
	if _, err = hex.DecodeString(cookie.Value); err != nil {
		return "", ""
	}
	return cookie.Value, bindingHash(cookie.Value)
}

func bindingHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Admission fails closed if durable rate-limit state cannot be read or written.
func (s *AuthService) admitMisty(w http.ResponseWriter, r *http.Request, key string, maximum int) bool {
	var count int
	now := time.Now().Unix()
	err := s.db.QueryRowContext(r.Context(), `INSERT INTO rate_limit(key,count,lastRequest) VALUES(?,1,?)
 ON CONFLICT(key) DO UPDATE SET
 count=CASE WHEN rate_limit.lastRequest<=? THEN 1 ELSE rate_limit.count+1 END,
 lastRequest=CASE WHEN rate_limit.lastRequest<=? THEN excluded.lastRequest ELSE rate_limit.lastRequest END
 WHERE rate_limit.lastRequest<=? OR rate_limit.count<? RETURNING count`, key, now, now-60, now-60, now-60, maximum).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		w.Header().Set("Retry-After", "60")
		writeAuthError(w, 429, "Too many attempts. Please wait.", "TOO_MANY_REQUESTS")
		return false
	}
	if err != nil {
		writeAuthError(w, 503, "Sign-in is temporarily unavailable", "AUTH_UNAVAILABLE")
		return false
	}
	// Bound storage for expired IP buckets; challenges have their own expiry.
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM rate_limit WHERE key LIKE 'misty-%' AND lastRequest<?`, now-3600)
	return true
}

func (s *AuthService) handleMistyStart(w http.ResponseWriter, r *http.Request) {
	if !s.mistyGuard(w, r) {
		return
	}
	var payload struct {
		Email string `json:"email"`
	}
	if !decodeMisty(w, r, &payload) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(payload.Email))
	if len(email) > 254 || !mistyEmail.MatchString(email) {
		writeAuthError(w, 400, "Enter a valid email address", "BAD_REQUEST")
		return
	}
	ip := clientAddress(r)
	if net.ParseIP(ip) == nil {
		writeAuthError(w, 400, "Invalid client address", "BAD_REQUEST")
		return
	}
	if !s.admitMisty(w, r, "misty-start:"+ip, 5) {
		return
	}
	var result struct {
		ChallengeID string `json:"challenge_id"`
		ExpiresIn   int    `json:"expires_in"`
	}
	status, err := s.misty.request(r.Context(), "POST", "/v1/auth/email/start", map[string]string{"email": email, "client_ip": ip}, &result)
	if status == 429 {
		w.Header().Set("Retry-After", "60")
		writeAuthError(w, 429, "Please wait before requesting another code", "TOO_MANY_REQUESTS")
		return
	}
	if err != nil || status != 202 || !mistyID.MatchString(result.ChallengeID) || result.ExpiresIn <= 0 || result.ExpiresIn > 600 {
		writeAuthError(w, 503, "Sign-in is temporarily unavailable", "AUTH_UNAVAILABLE")
		return
	}
	token, hash := browserBinding(r)
	if token == "" {
		token = randomToken(32)
		hash = bindingHash(token)
	}
	now := time.Now().Unix()
	if _, err = s.db.ExecContext(r.Context(), `DELETE FROM misty_login WHERE expires_at<=?`, now); err != nil {
		writeAuthError(w, 503, "Sign-in is temporarily unavailable", "AUTH_UNAVAILABLE")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO misty_login(binding_hash,challenge_id,email,expires_at) VALUES(?,?,?,?)`, hash, result.ChallengeID, email, now+int64(result.ExpiresIn))
	if err != nil {
		writeAuthError(w, 503, "Sign-in is temporarily unavailable", "AUTH_UNAVAILABLE")
		return
	}
	secure := strings.HasPrefix(os.Getenv("MIST_SITE_ORIGIN"), "https:") || r.TLS != nil
	http.SetCookie(w, &http.Cookie{Name: mistyCookie, Value: token, Path: "/auth", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 600})
	writeJSON(w, 202, map[string]any{"challenge_id": result.ChallengeID, "expires_in": result.ExpiresIn, "message": "If an active Misty account matches, a code will be sent."})
}

func (s *AuthService) handleMistyConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.mistyGuard(w, r) {
		return
	}
	var payload struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if !decodeMisty(w, r, &payload) {
		return
	}
	_, hash := browserBinding(r)
	if hash == "" || !mistyID.MatchString(payload.ChallengeID) || !mistyCode.MatchString(payload.Code) {
		writeAuthError(w, 400, "Invalid or expired login. Request a new code.", "INVALID_LOGIN")
		return
	}
	ip := clientAddress(r)
	if net.ParseIP(ip) == nil {
		writeAuthError(w, 400, "Invalid client address", "BAD_REQUEST")
		return
	}
	if !s.admitMisty(w, r, "misty-confirm:"+ip, 30) {
		return
	}
	var email string
	err := s.db.QueryRowContext(r.Context(), `UPDATE misty_login SET in_flight=1,attempts=attempts+1
  WHERE binding_hash=? AND challenge_id=? AND expires_at>? AND in_flight=0 AND attempts<3 RETURNING email`, hash, payload.ChallengeID, time.Now().Unix()).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		writeAuthError(w, 400, "Invalid or expired login. Request a new code.", "INVALID_LOGIN")
		return
	}
	if err != nil {
		writeAuthError(w, 503, "Sign-in is temporarily unavailable", "AUTH_UNAVAILABLE")
		return
	}
	defer s.db.Exec(`UPDATE misty_login SET in_flight=0 WHERE binding_hash=? AND challenge_id=?`, hash, payload.ChallengeID)
	var proof struct {
		PersonID string `json:"person_id"`
		Email    string `json:"email"`
	}
	status, err := s.misty.request(r.Context(), "POST", "/v1/auth/email/confirm", map[string]string{"challenge_id": payload.ChallengeID, "code": payload.Code, "client_ip": ip}, &proof)
	if err != nil || (status != 200 && status != 400 && status != 429) {
		writeAuthError(w, 503, "Sign-in is temporarily unavailable", "AUTH_UNAVAILABLE")
		return
	}
	if status == 429 {
		// A definitive quota rejection is not a wrong code. Ambiguous transport
		// failures retain their reservation because verification may have happened.
		_, _ = s.db.Exec(`UPDATE misty_login SET attempts=attempts-1 WHERE binding_hash=? AND challenge_id=?`, hash, payload.ChallengeID)
		w.Header().Set("Retry-After", "60")
		writeAuthError(w, 429, "Too many attempts. Please wait.", "TOO_MANY_REQUESTS")
		return
	}
	if status != 200 {
		writeAuthError(w, 400, "Invalid or expired code. Check the code or request a new one.", "INVALID_LOGIN")
		return
	}
	if !mistyID.MatchString(proof.PersonID) || proof.Email != email {
		writeAuthError(w, 503, "Sign-in is temporarily unavailable", "AUTH_UNAVAILABLE")
		return
	}
	user, token, err := s.createMistySession(r, proof.PersonID, email, hash, payload.ChallengeID)
	if err != nil {
		writeAuthError(w, 403, "This compute account is unavailable. Contact an administrator.", "ACCOUNT_UNAVAILABLE")
		return
	}
	secure := strings.HasPrefix(os.Getenv("MIST_SITE_ORIGIN"), "https:") || r.TLS != nil
	setSessionCookies(w, token, 604800, secure)
	writeJSON(w, 200, map[string]any{"user": user})
}

func (s *AuthService) createMistySession(r *http.Request, personID, email, binding, challenge string) (*Member, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	var userID string
	err = tx.QueryRow(`SELECT user_id FROM misty_identity WHERE person_id=?`, personID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = tx.QueryRow(`SELECT count(*),COALESCE(min(id),'') FROM user WHERE lower(email)=?`, email).Scan(&count, &userID); err != nil {
			return nil, "", err
		}
		if count != 0 {
			return nil, "", errors.New("existing account requires an explicit Misty identity mapping")
		}
		if count == 0 {
			userID = "user-" + randomToken(12)
			_, err = tx.Exec(`INSERT INTO user(id,name,email,role,emailVerified,createdAt,updatedAt) VALUES(?,?,?,'user',1,?,?)`, userID, email, email, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
			if err != nil {
				return nil, "", err
			}
		}
		if _, err = tx.Exec(`INSERT INTO misty_identity(person_id,user_id) VALUES(?,?)`, personID, userID); err != nil {
			return nil, "", err
		}
	} else if err != nil {
		return nil, "", err
	}
	var user Member
	if err = tx.QueryRow(`SELECT id,name,email,role FROM user WHERE id=? AND COALESCE(banned,0)=0`, userID).Scan(&user.ID, &user.Name, &user.Email, &user.Role); err != nil {
		return nil, "", err
	}
	// The stable directory identity owns the existing compute ID, even after an
	// email change. Conflicting local emails fail closed; roles never come from Misty.
	var conflicts int
	if err = tx.QueryRow(`SELECT count(*) FROM user WHERE lower(email)=? AND id<>?`, email, userID).Scan(&conflicts); err != nil {
		return nil, "", err
	}
	if conflicts != 0 {
		return nil, "", errors.New("conflicting existing account")
	}
	if _, err = tx.Exec(`UPDATE user SET email=?,emailVerified=1,updatedAt=? WHERE id=?`, email, time.Now().UTC().Format(time.RFC3339Nano), userID); err != nil {
		return nil, "", err
	}
	user.Email = email
	token := randomToken(24)
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	if _, err = tx.Exec(`INSERT INTO session(id,token,userId,expiresAt,createdAt,updatedAt,ipAddress,userAgent,mistyPersonId) VALUES(?,?,?,?,?,?,?,?,?)`, "sess-"+randomToken(12), token, userID, now.Add(7*24*time.Hour).Format(time.RFC3339Nano), stamp, stamp, clientAddress(r), r.UserAgent(), personID); err != nil {
		return nil, "", err
	}
	if old := extractSessionToken(r); old != "" {
		if _, err = tx.Exec(`DELETE FROM session WHERE token=?`, old); err != nil {
			return nil, "", err
		}
	}
	if _, err = tx.Exec(`DELETE FROM misty_login WHERE binding_hash=? AND challenge_id=?`, binding, challenge); err != nil {
		return nil, "", err
	}
	if err = tx.Commit(); err != nil {
		return nil, "", err
	}
	return &user, token, nil
}
