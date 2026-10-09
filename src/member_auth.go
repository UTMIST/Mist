package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

type memberKey struct{}

func hashID(id string) string {
	digest := sha256.Sum256([]byte(id))
	return hex.EncodeToString(digest[:12])
}

type Member struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Banned bool   `json:"banned"`
}
type AuthGateway struct {
	upstream    *url.URL
	proxy       *httputil.ReverseProxy
	client      *http.Client
	origins     map[string]bool
	legacyEmail string
}

func newAuthGateway(raw string) (*AuthGateway, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("MIST_AUTH_URL must be an HTTP service URL")
	}
	g := &AuthGateway{upstream: u, client: &http.Client{Timeout: 5 * time.Second}, origins: map[string]bool{}, legacyEmail: os.Getenv("MIST_LEGACY_OWNER_EMAIL")}
	for _, origin := range strings.Split(os.Getenv("MIST_ALLOWED_ORIGINS"), ",") {
		if origin != "" {
			g.origins[strings.TrimSpace(origin)] = true
		}
	}
	g.proxy = &httputil.ReverseProxy{Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(u)
		p.Out.Host = u.Host
		p.Out.Header.Del("X-Forwarded-For")
		p.Out.Header.Del("X-Forwarded-Host")
		p.Out.Header.Del("X-Forwarded-Proto")
		p.Out.Header.Del("X-Real-IP")
		p.Out.Header.Set("X-Real-IP", clientAddress(p.In))
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
		writeJSON(w, 503, map[string]string{"error": "Authentication service unavailable"})
	}}
	return g, nil
}
func (g *AuthGateway) session(r *http.Request) (*Member, error) {
	u := *g.upstream
	u.Path = "/auth/get-session"
	u.RawQuery = ""
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", r.Header.Get("Cookie"))
	req.Header.Set("X-Real-IP", clientAddress(r))
	response, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("session verification failed")
	}
	var result struct {
		User *Member `json:"user"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&result); err != nil {
		return nil, err
	}
	if result.User != nil && (result.User.ID == "" || result.User.Banned) {
		return nil, nil
	}
	return result.User, nil
}

type AccountStatus struct {
	Active bool   `json:"active"`
	Role   string `json:"role"`
}

func (g *AuthGateway) activeMembers(ctx context.Context) (map[string]AccountStatus, error) {
	u := *g.upstream
	u.Path = "/internal/members"
	u.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("MIST_INTERNAL_TOKEN"))
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("authoritative account status unavailable")
	}
	var body struct {
		Members []struct {
			ID     string `json:"id"`
			Active bool   `json:"active"`
			Role   string `json:"role"`
		} `json:"members"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&body); err != nil {
		return nil, err
	}
	result := map[string]AccountStatus{}
	for _, m := range body.Members {
		result[m.ID] = AccountStatus{Active: m.Active, Role: m.Role}
	}
	return result, nil
}
func (g *AuthGateway) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/auth/") {
			r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
			g.proxy.ServeHTTP(w, r)
			return
		}
		member, err := g.session(r)
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
			if (origin != "" && !g.origins[origin]) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeJSON(w, 403, map[string]string{"error": "Untrusted request origin"})
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), memberKey{}, member)))
	})
}
func (a *App) requestExecutor(r *http.Request) *KubernetesExecutor {
	if access, _ := r.Context().Value(teamContextKey{}).(*TeamAccess); access != nil {
		clone := *a.executor
		clone.namespace, clone.owner = access.Team.namespace(), access.Team.ID
		clone.storage, clone.sharedPVC, clone.team = access.Store, "team-storage", access
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
