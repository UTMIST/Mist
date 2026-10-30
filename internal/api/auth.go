package api

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/UTMIST/Mist/internal/identity"
)

// Authenticator resolves the caller of a request. It returns
// ErrNoCredentials when the request carries no credential of its kind, so
// the next authenticator can try, and any other error when a credential is
// present but invalid, which ends the request as unauthenticated.
//
// Identity provides the portal session and relay authenticators; this
// package provides host tokens for /internal and, in development builds
// only, a header naming the user.
type Authenticator interface {
	Authenticate(r *http.Request) (identity.Principal, error)
}

// ErrNoCredentials: the request carries no credential this authenticator
// understands.
var ErrNoCredentials = errors.New("api: no credentials")

// DevUserHeader names the account in development builds (tag mistdev).
// Production binaries do not contain the code that reads it, and reject
// any request that sends it, so a misconfigured proxy cannot let it
// through silently.
const DevUserHeader = "X-Mist-Dev-User"

// devAuthenticator is implemented only by the development authenticator.
type devAuthenticator interface{ acceptsDevHeader() }

type principalKey struct{}

func principalFrom(ctx context.Context) identity.Principal {
	p, _ := ctx.Value(principalKey{}).(identity.Principal)
	return p
}

type handler func(w http.ResponseWriter, r *http.Request, p identity.Principal) error

func unauthenticated(msg string) *Error {
	return &Error{http.StatusUnauthorized, CodeUnauthenticated, msg, nil}
}

// account authenticates a /v1 request as an account.
func (s *Server) account(h handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.authenticate(r, s.Accounts)
		if err == nil && (p.Kind != identity.PrincipalAccount || p.Username == "") {
			err = unauthenticated("account credentials required")
		}
		if err == nil {
			err = h(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)), p)
		}
		if err != nil {
			s.fail(w, r, err)
		}
	})
}

// host authenticates an /internal request as a host agent.
func (s *Server) host(h handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var list []Authenticator
		if s.Hosts != nil {
			list = []Authenticator{s.Hosts}
		}
		p, err := s.authenticate(r, list)
		if err == nil && (p.Kind != identity.PrincipalHost || p.Host == "") {
			err = unauthenticated("host credentials required")
		}
		if err == nil {
			err = h(w, r, p)
		}
		if err != nil {
			s.fail(w, r, err)
		}
	})
}

func (s *Server) authenticate(r *http.Request, list []Authenticator) (identity.Principal, error) {
	if r.Header.Get(DevUserHeader) != "" {
		allowed := false
		for _, a := range list {
			if _, ok := a.(devAuthenticator); ok {
				allowed = true
			}
		}
		if !allowed {
			return identity.Principal{}, unauthenticated("the development user header is not accepted by this server")
		}
	}
	for _, a := range list {
		p, err := a.Authenticate(r)
		if errors.Is(err, ErrNoCredentials) {
			continue
		}
		if err != nil {
			s.Log.Info("authentication failed", "path", r.URL.Path, "err", err)
			return identity.Principal{}, unauthenticated("invalid credentials")
		}
		return p, nil
	}
	return identity.Principal{}, unauthenticated("sign in or use the mist command on the login node")
}

// HostTokens authenticates host agents by bearer token. Only SHA-256
// hashes of tokens are stored, one per line: "<host> <scope> <hex>".
// Each instance accepts one scope, so a token issued for one operation
// cannot be used for another.
type HostTokens struct {
	scope   string
	entries []hostToken
}

type hostToken struct {
	host string
	hash [sha256.Size]byte
}

// LoadHostTokens reads a token file, keeping the entries for scope.
// Blank lines and lines starting with # are ignored.
func LoadHostTokens(path, scope string) (*HostTokens, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ht := &HostTokens{scope: scope}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: want \"<host> <scope> <sha256-hex>\"", path, n)
		}
		raw, err := hex.DecodeString(fields[2])
		if err != nil || len(raw) != sha256.Size {
			return nil, fmt.Errorf("%s:%d: token hash is not 64 hex digits", path, n)
		}
		if fields[1] != scope {
			continue
		}
		var t hostToken
		t.host = fields[0]
		copy(t.hash[:], raw)
		ht.entries = append(ht.entries, t)
	}
	return ht, sc.Err()
}

// HashToken is the form stored in a host token file.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (ht *HostTokens) Authenticate(r *http.Request) (identity.Principal, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return identity.Principal{}, ErrNoCredentials
	}
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || token == "" {
		return identity.Principal{}, errors.New("malformed Authorization header")
	}
	sum := sha256.Sum256([]byte(token))
	host := ""
	for _, t := range ht.entries { // compare every entry, in constant time each
		if subtle.ConstantTimeCompare(sum[:], t.hash[:]) == 1 {
			host = t.host
		}
	}
	if host == "" {
		return identity.Principal{}, fmt.Errorf("unknown %s token", ht.scope)
	}
	return identity.Principal{Kind: identity.PrincipalHost, Host: host}, nil
}
