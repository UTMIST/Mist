// Package identity is the contract between Identity (accounts, teams,
// authorization) and the rest of mistd. The behaviour is specified in
// docs/agents/architecture/identity-and-accounts.md.
//
// This file only declares what other owners call. The Postgres-backed
// implementation belongs to the Identity owner; identitytest provides
// fixtures until it exists.
package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// PrincipalKind is how a request was authenticated.
type PrincipalKind string

const (
	PrincipalAccount PrincipalKind = "account" // portal session, relay, API token
	PrincipalHost    PrincipalKind = "host"    // provisioner or cleanup reporter; /internal only
)

// Principal is the authenticated caller, resolved once by API middleware.
// Handlers never read identity headers themselves.
type Principal struct {
	Kind     PrincipalKind
	Username string // PrincipalAccount
	Host     string // PrincipalHost
}

// Action is something a principal may be allowed to do.
type Action string

const (
	ActionSubmitJob Action = "job.submit" // Target.Team = charged team
	ActionViewJob   Action = "job.view"   // metadata, script and logs
	ActionCancelJob Action = "job.cancel"
	ActionAttachJob Action = "job.attach"
	ActionAdmin     Action = "admin"
)

// Target is what an Action applies to.
type Target struct {
	Team  string // team slug
	Owner string // username of the job's submitter, when there is one
}

// Account is a Mist account as jobs need it (D4).
type Account struct {
	Username    string
	UID         int
	PersonalGID int
	Active      bool
	IsAdmin     bool
}

// Team is a team as jobs need it.
type Team struct {
	Slug     string
	GID      int
	VolumeID string
}

// Grant is a current, locked authorization for one account to use one team.
type Grant struct {
	Account    Account
	Team       Team
	Generation int64 // authorization generation; frozen into the LaunchSpec
	ExtraGIDs  []int // always includes mist-users (19000)
}

// Errors returned by Authorizer and Admitter. The scheduler maps them to
// pending reasons; the API maps them to error codes.
var (
	// ErrForbidden: known and current, but not allowed. Job and log
	// endpoints report it as not_found to non-members.
	ErrForbidden = errors.New("identity: forbidden")
	// ErrRevoked: the account is disabled or no longer in the team.
	// Unstarted work is cancelled; started work is stopped.
	ErrRevoked = errors.New("identity: access revoked")
	// ErrStaleGeneration: the request was made under an older generation
	// than the current one; unstarted work must be resubmitted.
	ErrStaleGeneration = errors.New("identity: authorization generation changed")
	// ErrNotProvisioned: folders or host state are not ready (PROVISIONING).
	ErrNotProvisioned = errors.New("identity: provisioning not acknowledged")
	// ErrUnavailable: freshness cannot be established (AUTH_UNAVAILABLE).
	// Never fall back to a stale grant.
	ErrUnavailable = errors.New("identity: authorization unavailable")
)

// Authorizer decides whether a principal may act, using current membership.
// It is called on every request and on every stream establishment.
type Authorizer interface {
	Authorize(ctx context.Context, p Principal, a Action, t Target) error
}

// Admitter is used inside scheduler transactions. Both methods take row
// locks on the account, then the membership (lock order steps 1–2 in
// package db), so they must be called before the scheduler locks any job,
// allocation or node row in the same transaction.
type Admitter interface {
	// LockGrant locks and returns the current grant for username in team.
	// It fails with ErrRevoked, ErrNotProvisioned or ErrUnavailable as
	// described above. If minGeneration is nonzero and the current
	// generation differs, it returns ErrStaleGeneration.
	LockGrant(ctx context.Context, tx pgx.Tx, username, team string, minGeneration int64) (Grant, error)

	// Revocations returns (username, team) pairs whose access was revoked
	// after the given revocation sequence number, and the newest number.
	// The scheduler stores the number it has applied, so revocation is a
	// durable record rather than an in-memory notification.
	Revocations(ctx context.Context, tx pgx.Tx, after int64) ([]Revocation, int64, error)
}

// Revocation is one durable revocation record. Team is empty when the
// whole account was disabled.
type Revocation struct {
	Seq      int64
	Username string
	Team     string
}

// Unavailable is an Admitter that can never establish authorization. It
// keeps a scheduler without a real Identity backend failing closed.
type Unavailable struct{}

func (Unavailable) LockGrant(context.Context, pgx.Tx, string, string, int64) (Grant, error) {
	return Grant{}, ErrUnavailable
}

func (Unavailable) Revocations(context.Context, pgx.Tx, int64) ([]Revocation, int64, error) {
	return nil, 0, ErrUnavailable
}
