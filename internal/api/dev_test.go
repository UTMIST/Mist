//go:build mistdev

package api

import (
	"net/http"
	"testing"
)

func TestDevHeader(t *testing.T) {
	e := newEnv(t)
	e.s.Accounts = []Authenticator{DevHeader{}}
	if r := e.do("GET", "/v1/jobs", "", nil, DevUserHeader, "alice"); r.status != http.StatusOK {
		t.Fatalf("dev header in a development build: %d %s", r.status, r.body)
	}
}
