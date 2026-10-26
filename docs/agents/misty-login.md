# Sign in with Misty

`MIST_AUTH_MODE=misty` replaces local password login with Misty email codes.
The default is `local`; no deployment manifest enables the new mode automatically.
Misty checks its registered primary email and active directory record, generates
and sends the code, verifies it, and returns an immutable person ID. Mist keeps
its own SQLite sessions, user IDs, roles, teams, jobs and files.

## Configuration and rollout

Use `deploy/private/misty-login.env.example` as the configuration reference:

| Variable | Meaning |
| --- | --- |
| `MIST_AUTH_MODE` | `local` (default) or `misty`; unknown values fail startup |
| `MIST_SITE_ORIGIN` | Exact browser origin; required in Misty mode |
| `MISTY_GATEWAY_URL` | Gateway origin, without an API path |
| `MISTY_GATEWAY_API_KEY` | Server-only issued `gw_` key with `auth:email` and `auth:status` |

Both origins must use HTTPS; loopback HTTP is allowed for development only.
Misty mode forces authentication and rejects `MIST_AUTH_DISABLED=true`.
Every auth POST must have the exact configured Origin. The existing trusted
Nginx address list (`MIST_TRUSTED_PROXY_IPS`) must be correct: only those socket
peers may attest `X-Real-IP`. Browsers never supply the gateway key or client IP.

1. Deploy and verify Misty's gateway/verification changes in staging first.
   Follow `services/gateway/docs/DEPLOYMENT.md` in UTMIST/Misty for private
   consumer credentials, migration 002 and scoped gateway key issuance.
2. Establish HTTPS on the compute portal. Its existing private HTTP deployment
   cannot enable this mode. Do not expose the Kubernetes API or widen tailnet
   access just to enable login.
3. Back up the existing auth database using `deploy/private/backup-auth.sh`.
   Check that the active Misty email for each existing admin matches exactly one
   local user (case-insensitively). Duplicate matches or a local ban deny login.
4. Deploy this source using the normal reviewed compute release process. Load
   the gateway key through a protected Kubernetes Secret, never a frontend env
   variable or a checked-in manifest. Configure the remaining values on the API
   deployment and keep its existing single-replica SQLite/PVC arrangement.
5. Verify an existing account retains its local ID, role, teams and files; a new
   eligible member gets role `user`. Verify wrong-code errors, replay rejection,
   sign-out, and inactive-member denial before considering the cutover complete.

Startup adds `misty_identity`, `misty_login`, `misty_legacy_owner`, and nullable
`session.mistyPersonId`; existing rows and credentials remain. Old password
sessions are rejected while Misty mode is enabled. Password creation/change/reset
endpoints and their UI controls are disabled in this mode. Existing local admin
roles remain local authority; Misty never grants compute administrator access.

If `MIST_LEGACY_OWNER_EMAIL` is configured, first enablement requires it to match
one existing local user. That user's ID is pinned durably before any email can
change. The pin survives restarts and rollback to `local`; email reassignment
must never transfer old jobs or datasets. Changing the configured legacy email
does not overwrite a recorded pin. Handle deliberate ownership transfers as a
separate reviewed data migration.

For rollback, restore `MIST_AUTH_MODE=local` and restart the same code version.
Keep the additive tables, existing database and volume. Existing local passwords
work again; users first created through Misty have no password and need an admin
to provision one. Rolling back to older code that lacks the durable ownership pin
is unsafe after a legacy owner's email changes.

## Login and session boundaries

The browser calls `/auth/misty/start` with `{email}`, then
`/auth/misty/confirm` with `{challenge_id,code}`. The API forwards to gateway
`/v1/auth/email/start` and `/v1/auth/email/confirm` with its server credential and
socket-derived client IP. A hashed HttpOnly browser binding, expiry, attempt
budget and atomic in-flight reservation prevent unbound/replayed confirmation.
Neither the code nor a session token is returned to JavaScript or logged.
The API stores challenge metadata, not code values or code hashes.

The gateway's generic 202 does not disclose whether an account exists or an
email was sent. A failed/throttled resend can leave the earlier code valid;
the UI retains recent request IDs so users may explicitly choose that request.
It never silently tries a code against multiple challenges.

First successful login links the verified email to one existing local ID, or
creates a new ordinary member. Subsequent logins use the stored Misty person ID,
allowing email changes without transferring local ownership. Conflicting emails,
duplicate accounts, bans and already-linked identities fail closed.

Authenticated requests check `/v1/auth/people/{id}/status`. Results are cached
per person for 30 seconds; errors deny access and are cached for 5 seconds.
A retirement therefore blocks subsequent requests after at most 30 seconds of
cached eligibility. Ongoing jobs are governed by the existing local ban/team
revocation policy; Misty retirement does not automatically cancel running jobs.
Authentication is checked when a request starts, not continuously during a
long-running response.

Compute adds durable per-IP start (5/minute) and confirm (30/minute) admission,
five attempts per challenge, ten-minute expiry and strict 4 KiB inputs. Misty
also enforces application, client-IP and mailbox quotas. Database failures deny
admission before sending; upstream failures do not grant sessions. Definitive
upstream 429 responses preserve a wait response and do not use a local guess.
The gateway currently allows 60 status checks/minute per application: size its
quota/cache policy deliberately before exceeding about 30 continuously active
identities. HTTP clients have bounded timeouts and do not follow redirects.

## CLI

Use the HTTPS portal URL (the same origin configured on the server):

```sh
mist --api-url https://compute.example.com/api auth login --misty --email member@example.com
```

The CLI requests a code, reads it without echoing, confirms with the bound
challenge cookie and saves only session cookies in its existing mode-0600 config.
`--code-stdin` reads a code from standard input; do not put codes in arguments.
The old password flow remains available only in local mode.

## Validation and changed files

Run API/CLI tests on Linux (the existing storage code has Linux-specific statfs
types), including `go test -race ./...` in `src` and `cli`. Run frontend Vitest,
TypeScript, ESLint and the production build. All automated providers are local
fakes; tests must never send real mail.

See [the verification report](misty-login-verification.md) for the completed local
checks and the remaining live deployment checks.

- `src/misty_auth.go`: scoped gateway client, identity/challenge storage, login
  handlers, eligibility cache, configuration and durable legacy-owner migration.
- `src/auth.go`, `src/app.go`, `src/member_auth.go`: session provenance, opt-in
  mode enforcement, SQLite connection consistency and stable ownership routing.
- `src/misty_auth_test.go`: failure, concurrency, identity and session regressions.
- `cli/cmd/auth_misty.go`, `auth_login.go`, `auth_misty_test.go`: bound code login
  and reuse of protected session persistence, with regression coverage.
- `web-interface/src/components/MistyLogin.tsx` and its test: email/code flow,
  retry/error states and request selection after resend.
- `web-interface/src/auth.tsx`, `api.ts`, `components/AccountPage.tsx`: mode-aware
  account gate, API response type and password-control visibility.
- `deploy/private/misty-login.env.example` and this document: configuration,
  rollout/rollback and ownership/security boundaries.
- `docs/agents/misty-login-verification.md`: observed test evidence and limits.
