# Misty login verification — 2026-10-26

The local implementation passed independent code review and the checks below.
No live service was deployed and no real email was sent. Tailscale was not needed.

## Cross-service browser check

Chromium exercised the built compute frontend and real Go authentication code
against freshly built Misty gateway, team-tracking and verification containers.
Each service used disposable storage; gateway and verification had their own
Postgres databases. Private calls used separately issued scoped credentials.
Only the email sender and its code generator were test replacements. Unrelated
compute job/cluster routes were stubs; no Kubernetes jobs ran.

Observed results:

- An active registered email received a challenge; an incorrect code failed.
- The correct code created a compute session, preserving the seeded existing
  local user ID and administrator role.
- Reusing the redeemed challenge failed.
- Password account-creation and reset controls were absent in Misty mode.
- Retiring the member in the real directory caused the existing compute session
  to be rejected after the configured status cache expired, within 40 seconds.
- Unknown and inactive emails received the generic start response, could not
  establish sessions, and created no verification-code rows.
- The gateway also passed its resolver credential-separation, upstream outage,
  revocation, migration/readiness and email-login smoke assertions.
- Chromium reported no page errors.

A separate Chromium run through real compute authentication with a fake gateway
verified sign-out, HttpOnly session cookies, and the absence of a gateway key in
browser requests.

## Automated checks

- API and CLI: Linux `go test -race ./...` passed. Regressions cover concurrent
  redemption, expiry, attempts, browser binding, strict origin/body validation,
  database failures, retired members, upstream outages, legacy sessions, account
  collisions/bans, preserved ownership after email changes and quota responses.
- Frontend: Vitest, TypeScript, ESLint on changed sources, Prettier checks on
  changed sources, and the production build passed. The resend regression
  selects an earlier request explicitly after a replacement request.
- `git diff --check` passed.

The macOS API build hits pre-existing Linux-specific `statfs` type differences,
so Go tests ran in `golang:1.26` containers. `npm ci` encountered missing optional
platform packages in the existing lockfile; `npm install --ignore-scripts`
installed the same existing package versions plus missing optional entries for
verification. The original lockfile was restored; dependency repair is separate
from this feature.

## Local evidence

The operator workspace retains the disposable test harness and logs:

- `/tmp/misty-compute-browser/fullstack.py`: container orchestration, derived
  from Misty's existing `services/gateway/tests/smoke_stack.py`.
- `/tmp/misty-compute-browser/fullstack_harness_test.go`: compute auth/static
  frontend harness, injected only into a temporary test checkout.
- `/tmp/misty-compute-browser/fullstack.cjs`: browser assertions.
- `/tmp/misty-compute-fullstack.log` and
  `/tmp/misty-compute-fullstack-browser.log`: successful integration results.
- `/tmp/misty-compute-browser/fullstack-signed-in.png`: authenticated account UI.
- `/tmp/misty-compute-browser/artifact-hash.txt`: SHA-256 over changed Go and
  TypeScript source/test paths and contents.

These are local temporary artifacts, not deployment assets. All test containers,
networks and disposable databases were removed after verification.

## Remaining live checks

The private compute host was unreachable because this Mac's Tailscale client was
logged out. Live HTTPS, ingress/proxy-IP configuration, real email delivery,
database backup/cutover and existing production account mappings remain
unverified. Misty's staging gateway still requires deployment and provisioning.
Follow [the rollout guide](misty-login.md) before enabling the feature.
