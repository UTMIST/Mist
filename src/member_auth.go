package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
)

type memberKey struct{}

func hashID(id string) string {
	digest := sha256.Sum256([]byte(id))
	return hex.EncodeToString(digest[:12])
}

type Member struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	IsAdmin bool   `json:"is_admin"`
}

type AccountStatus struct {
	Role string `json:"role"`
}

// Authenticator provides an interface for user authentication, member lookups,
// and session middleware. This allows plugging in the incoming core auth system,
// the current Better Auth / SQLite implementation, or dummy mock implementations.
type Authenticator interface {
	http.Handler
	Close() error
	Session(r *http.Request) (*Member, error)
	LookupMember(ctx context.Context, id string) (*Member, error)
	ActiveMembers(ctx context.Context) (map[string]AccountStatus, error)
	Middleware(next http.Handler) http.Handler
}

// Ensure AuthService implements Authenticator.
var _ Authenticator = (*AuthService)(nil)

// DummyAuthenticator is a stub implementation of Authenticator for testing or headless runs.
type DummyAuthenticator struct {
	Members map[string]*Member
}

func NewDummyAuthenticator() *DummyAuthenticator {
	return &DummyAuthenticator{
		Members: make(map[string]*Member),
	}
}

func (d *DummyAuthenticator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (d *DummyAuthenticator) Close() error {
	return nil
}

func (d *DummyAuthenticator) Session(r *http.Request) (*Member, error) {
	return nil, nil
}

func (d *DummyAuthenticator) LookupMember(ctx context.Context, id string) (*Member, error) {
	if m, ok := d.Members[id]; ok {
		return m, nil
	}
	return nil, errors.New("account unavailable")
}

func (d *DummyAuthenticator) ActiveMembers(ctx context.Context) (map[string]AccountStatus, error) {
	res := make(map[string]AccountStatus)
	for id, m := range d.Members {
		res[id] = AccountStatus{Role: m.Role}
	}
	return res, nil
}

func (d *DummyAuthenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

type AuthGateway = AuthService

func newAuthGateway(raw string) (*AuthGateway, error) {
	return newAuthService(raw, nil)
}

func (a *App) requestExecutor(r *http.Request) *KubernetesExecutor {
	if access, _ := r.Context().Value(teamContextKey{}).(*TeamAccess); access != nil {
		clone := *a.executor
		clone.namespace, clone.owner = access.Team.namespace(), access.Team.ID
		clone.storage, clone.sharedPVC, clone.team = access.Store, "team-results", access
		clone.inputPVC = "team-datasets"
		return &clone
	}
	member, _ := r.Context().Value(memberKey{}).(*Member)
	if member == nil {
		return a.executor
	}
	clone := *a.executor
	if a.auth != nil && strings.EqualFold(member.Email, a.auth.legacyEmail) {
		clone.owner = a.executor.owner
	} else {
		digest := sha256.Sum256([]byte(member.ID))
		clone.owner = "user-" + hex.EncodeToString(digest[:12])
	}
	return &clone
}

// Trust exactly the deployed Nginx host/gateway, never a client-supplied chain.
func clientAddress(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	for _, trusted := range strings.Split(os.Getenv("MIST_TRUSTED_PROXY_IPS"), ",") {
		if trusted != "" && trusted == host {
			if ip := net.ParseIP(r.Header.Get("X-Real-IP")); ip != nil {
				return ip.String()
			}
		}
	}
	return host
}
