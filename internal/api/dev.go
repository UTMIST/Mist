//go:build mistdev

package api

import (
	"net/http"

	"github.com/UTMIST/Mist/internal/identity"
)

// DevHeader trusts DevUserHeader as the account name. It exists only in
// binaries built with -tags mistdev, for local development against the
// identitytest fixture.
type DevHeader struct{}

func (DevHeader) acceptsDevHeader() {}

func (DevHeader) Authenticate(r *http.Request) (identity.Principal, error) {
	u := r.Header.Get(DevUserHeader)
	if u == "" {
		return identity.Principal{}, ErrNoCredentials
	}
	return identity.Principal{Kind: identity.PrincipalAccount, Username: u}, nil
}
