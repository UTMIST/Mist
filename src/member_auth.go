package main

import (
	"crypto/sha256"
	"encoding/hex"
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
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

type AccountStatus struct {
	Role string `json:"role"`
}

type AuthGateway = AuthService

func newAuthGateway(raw string) (*AuthGateway, error) {
	return newAuthService(raw, nil)
}

func (a *App) requestExecutor(r *http.Request) *KubernetesExecutor {
	if access, _ := r.Context().Value(teamContextKey{}).(*TeamAccess); access != nil {
		clone := *a.executor
		clone.namespace, clone.owner = access.Team.namespace(), access.Team.ID
		clone.storage, clone.sharedPVC, clone.team = access.Store, "team-models", access
		clone.inputPVC = "team-storage"
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
