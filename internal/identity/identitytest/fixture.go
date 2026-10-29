// Package identitytest is an in-memory stand-in for Identity, for tests and
// development only. It never touches the transaction it is given, so it
// does not exercise lock order; tests of locking need the real package.
package identitytest

import (
	"context"
	"slices"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/UTMIST/Mist/internal/identity"
)

// MistUsersGID is the permanent researcher group (identity-and-accounts.md).
const MistUsersGID = 19000

// Fixture holds accounts, teams and memberships. The zero value is empty;
// New returns the acceptance-matrix accounts.
type Fixture struct {
	mu          sync.Mutex
	accounts    map[string]identity.Account
	teams       map[string]identity.Team
	members     map[[2]string]string // (username, team) -> "member" | "lead"
	unready     map[string]bool      // teams whose folders are not provisioned
	generation  int64
	revocations []identity.Revocation
	Unavailable bool // simulate a directory outage
}

// New returns Alice and Bob in team-a, Carol in team-b, and Alice also in
// team-b, matching the M1 acceptance matrix. Alice leads team-a.
func New() *Fixture {
	f := &Fixture{
		accounts:   map[string]identity.Account{},
		teams:      map[string]identity.Team{},
		members:    map[[2]string]string{},
		unready:    map[string]bool{},
		generation: 1,
	}
	f.AddTeam(identity.Team{Slug: "team-a", GID: 40001, VolumeID: "vol1"})
	f.AddTeam(identity.Team{Slug: "team-b", GID: 40002, VolumeID: "vol1"})
	f.AddAccount(identity.Account{Username: "alice", UID: 20001, PersonalGID: 20001, Active: true})
	f.AddAccount(identity.Account{Username: "bob", UID: 20002, PersonalGID: 20002, Active: true})
	f.AddAccount(identity.Account{Username: "carol", UID: 20003, PersonalGID: 20003, Active: true})
	f.AddAccount(identity.Account{Username: "root-admin", UID: 20004, PersonalGID: 20004, Active: true, IsAdmin: true})
	f.AddMember("alice", "team-a", "lead")
	f.AddMember("bob", "team-a", "member")
	f.AddMember("carol", "team-b", "member")
	f.AddMember("alice", "team-b", "member")
	return f
}

func (f *Fixture) AddAccount(a identity.Account) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accounts[a.Username] = a
}

func (f *Fixture) AddTeam(t identity.Team) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.teams[t.Slug] = t
}

// AddMember adds a membership and bumps the generation, as Identity does.
func (f *Fixture) AddMember(username, team, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.members[[2]string{username, team}] = role
	f.generation++
}

// RemoveMember removes a membership and records a revocation.
func (f *Fixture) RemoveMember(username, team string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.members, [2]string{username, team})
	f.revoke(username, team)
}

// Disable deactivates an account and records an account-wide revocation.
func (f *Fixture) Disable(username string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.accounts[username]
	a.Active = false
	f.accounts[username] = a
	f.revoke(username, "")
}

// SetProvisioned marks a team's folders ready or not.
func (f *Fixture) SetProvisioned(team string, ready bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unready[team] = !ready
}

// Generation returns the current authorization generation.
func (f *Fixture) Generation() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.generation
}

func (f *Fixture) revoke(username, team string) {
	f.generation++
	f.revocations = append(f.revocations, identity.Revocation{
		Seq: int64(len(f.revocations) + 1), Username: username, Team: team,
	})
}

func (f *Fixture) Authorize(_ context.Context, p identity.Principal, a identity.Action, t identity.Target) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Unavailable {
		return identity.ErrUnavailable
	}
	if p.Kind != identity.PrincipalAccount {
		return identity.ErrForbidden
	}
	acct, ok := f.accounts[p.Username]
	if !ok || !acct.Active {
		return identity.ErrRevoked
	}
	role, member := f.members[[2]string{p.Username, t.Team}]
	switch a {
	case identity.ActionAdmin:
		if acct.IsAdmin {
			return nil
		}
	case identity.ActionSubmitJob:
		if member {
			return nil
		}
	case identity.ActionViewJob:
		if member || acct.IsAdmin {
			return nil
		}
	case identity.ActionCancelJob:
		if (member && (t.Owner == p.Username || role == "lead")) || acct.IsAdmin {
			return nil
		}
	case identity.ActionAttachJob: // admins cannot attach as the user
		if member && t.Owner == p.Username {
			return nil
		}
	}
	return identity.ErrForbidden
}

func (f *Fixture) LockGrant(_ context.Context, _ pgx.Tx, username, team string, minGeneration int64) (identity.Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Unavailable {
		return identity.Grant{}, identity.ErrUnavailable
	}
	acct, ok := f.accounts[username]
	if !ok || !acct.Active {
		return identity.Grant{}, identity.ErrRevoked
	}
	if _, member := f.members[[2]string{username, team}]; !member {
		return identity.Grant{}, identity.ErrRevoked
	}
	if minGeneration != 0 && minGeneration != f.generation {
		return identity.Grant{}, identity.ErrStaleGeneration
	}
	if f.unready[team] {
		return identity.Grant{}, identity.ErrNotProvisioned
	}
	return identity.Grant{
		Account:    acct,
		Team:       f.teams[team],
		Generation: f.generation,
		ExtraGIDs:  []int{MistUsersGID},
	}, nil
}

func (f *Fixture) Revocations(_ context.Context, _ pgx.Tx, after int64) ([]identity.Revocation, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Unavailable {
		return nil, after, identity.ErrUnavailable
	}
	i, _ := slices.BinarySearchFunc(f.revocations, after+1, func(r identity.Revocation, seq int64) int {
		return int(r.Seq - seq)
	})
	out := slices.Clone(f.revocations[i:])
	return out, int64(len(f.revocations)), nil
}

var (
	_ identity.Authorizer = (*Fixture)(nil)
	_ identity.Admitter   = (*Fixture)(nil)
)
