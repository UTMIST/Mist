//go:build mistdev

package main

import (
	"github.com/UTMIST/Mist/internal/api"
	"github.com/UTMIST/Mist/internal/identity/identitytest"
)

// Each process gets its own in-memory fixture, identical at start;
// membership changes are not shared between api and scheduler.
func newDevIdentity() (*devIdentity, error) {
	f := identitytest.New()
	return &devIdentity{authz: f, admit: f, header: api.DevHeader{}}, nil
}
