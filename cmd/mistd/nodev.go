//go:build !mistdev

package main

import "errors"

// Production builds contain no development identity.
func newDevIdentity() (*devIdentity, error) {
	return nil, errors.New("-dev-fixture needs a development build (go build -tags mistdev)")
}
