package identitytest

import (
	"context"
	"errors"
	"testing"

	"github.com/UTMIST/Mist/internal/identity"
)

func TestAuthorizeMatrix(t *testing.T) {
	f := New()
	ctx := context.Background()
	acct := func(u string) identity.Principal {
		return identity.Principal{Kind: identity.PrincipalAccount, Username: u}
	}
	aliceJobA := identity.Target{Team: "team-a", Owner: "alice"}
	cases := []struct {
		who  string
		act  identity.Action
		ok   bool
		note string
	}{
		{"alice", identity.ActionAttachJob, true, "owner attaches"},
		{"bob", identity.ActionViewJob, true, "teammate views"},
		{"bob", identity.ActionCancelJob, false, "member cannot cancel others"},
		{"bob", identity.ActionAttachJob, false, "teammate cannot attach"},
		{"carol", identity.ActionViewJob, false, "other team"},
		{"root-admin", identity.ActionCancelJob, true, "admin cancels"},
		{"root-admin", identity.ActionAttachJob, false, "admin cannot attach as user"},
	}
	for _, c := range cases {
		err := f.Authorize(ctx, acct(c.who), c.act, aliceJobA)
		if (err == nil) != c.ok {
			t.Errorf("%s: got %v", c.note, err)
		}
	}
	if err := f.Authorize(ctx, acct("alice"), identity.ActionCancelJob, identity.Target{Team: "team-a", Owner: "bob"}); err != nil {
		t.Errorf("lead cancels team job: %v", err)
	}
}

func TestLockGrantAndRevocation(t *testing.T) {
	f := New()
	ctx := context.Background()
	g, err := f.LockGrant(ctx, nil, "alice", "team-b", 0)
	if err != nil || g.Team.GID != 40002 || g.ExtraGIDs[0] != MistUsersGID {
		t.Fatalf("grant %+v, %v", g, err)
	}

	f.SetProvisioned("team-b", false)
	if _, err := f.LockGrant(ctx, nil, "alice", "team-b", 0); !errors.Is(err, identity.ErrNotProvisioned) {
		t.Fatalf("want ErrNotProvisioned, got %v", err)
	}
	f.SetProvisioned("team-b", true)

	f.RemoveMember("alice", "team-b")
	if _, err := f.LockGrant(ctx, nil, "alice", "team-b", 0); !errors.Is(err, identity.ErrRevoked) {
		t.Fatalf("want ErrRevoked, got %v", err)
	}
	if _, err := f.LockGrant(ctx, nil, "alice", "team-a", g.Generation); !errors.Is(err, identity.ErrStaleGeneration) {
		t.Fatalf("want ErrStaleGeneration for old generation, got %v", err)
	}
	revs, last, err := f.Revocations(ctx, nil, 0)
	if err != nil || len(revs) != 1 || revs[0].Team != "team-b" || last != 1 {
		t.Fatalf("revocations %+v last=%d err=%v", revs, last, err)
	}
	if revs, _, _ := f.Revocations(ctx, nil, last); len(revs) != 0 {
		t.Fatalf("revocations after %d: %+v", last, revs)
	}

	f.Unavailable = true
	if _, err := f.LockGrant(ctx, nil, "bob", "team-a", 0); !errors.Is(err, identity.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}
