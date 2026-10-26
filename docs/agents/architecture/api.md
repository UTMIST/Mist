# API

`mistd api` is the only way clients and agents reach Mist state. This document
fixes the conventions every endpoint follows, how callers authenticate, and the
job wire types shared by the CLI, portal and scheduler. Each component document
lists the endpoints it owns. Overview: [ARCHITECTURE.md](../../../ARCHITECTURE.md).

## Conventions

- **Format.** JSON over HTTP. Paths are versioned: `/v1/...` for users and
  admins, `/internal/v1/...` for host agents.
- **Placement.** One `mistd api` process on the control node (NVIDIA box). The
  portal's Nginx forwards `/api/*` to it and strips the prefix. Agents reach it
  on the direct link (`192.168.240.1`).
- **Logic lives in the API.** Clients don't compute permissions, defaults or
  states; the API returns ready-to-display fields (for example `pending_reason`
  and `can_cancel`). Clients may poll: every 5–10 s is fine.
- **Errors** use one shape and a stable `code`:

  ```json
  { "error": { "code": "limit_exceeded", "message": "Team vision already has 2 GPUs running", "details": {} } }
  ```

  Codes: `unauthenticated`, `forbidden`, `not_found`, `invalid`,
  `limit_exceeded`, `conflict`, `unavailable`, `internal`.
- **Lists** take `?limit=` and `?cursor=` and return `{ "items": [...], "next_cursor": "..." }`.
- **Times** are RFC 3339 UTC strings. **Durations** are integer seconds.
  **Sizes** are integer bytes.
- **Streams** (log follow, interactive attach) use WebSockets on the same paths
  with `?follow=1` or an `Upgrade` header.

## Authentication

| Caller | Credential | Principal |
|---|---|---|
| Portal | Session cookie set at sign-in | The signed-in account |
| CLI on the login gateway | Relay token + `X-Mist-Asserted-User` (see [login-gateway.md](login-gateway.md)) | The asserted account |
| Provisioner, relay key lookup | Per-host agent token | The host; `/internal/*` only |
| Laptop CLI (later) | Personal API token | The token's account |

One middleware resolves the principal, and authorization goes through
`identity.Authorize` (see [identity-and-accounts.md](identity-and-accounts.md)).
Handlers never read identity headers directly.

## Endpoint index

| Area | Endpoints | Owner doc |
|---|---|---|
| Account | `GET/PATCH /v1/me`, `/v1/me/ssh-keys` | identity |
| Teams | `GET /v1/teams`, `GET /v1/teams/{slug}` | identity |
| Storage | `GET /v1/teams/{slug}/usage`, `GET /v1/volumes` (admin) | storage |
| Jobs | `POST /v1/jobs`, `GET /v1/jobs`, `GET /v1/jobs/{id}`, `POST /v1/jobs/{id}/cancel`, `GET /v1/jobs/{id}/log`, `GET /v1/jobs/{id}/attach` (WebSocket) | scheduler / executors |
| Cluster | `GET /v1/partitions`, `GET /v1/queue` | scheduler |
| Admin | `/v1/admin/accounts`, `/v1/admin/partitions`, `/v1/admin/qos`, `/v1/admin/limits`, `/v1/admin/nodes` | identity, scheduler |
| Agents | `/internal/v1/provision/*`, `/internal/v1/ssh-keys/{username}` | identity |

## Job wire types

### Submitting: `POST /v1/jobs`

```json
{
  "team": "vision",
  "partition": "gpu",
  "qos": "normal",
  "name": "resnet-sweep-3",
  "kind": "batch",
  "script": "#!/bin/bash\n#MIST --gpus=1\npython train.py --lr 3e-4\n",
  "args": [],
  "workdir": "/mist/teams/vision/alice/resnet",
  "output": "mist-%j.out",
  "env": { "WANDB_PROJECT": "resnet" },
  "resources": {
    "cpus": 8,
    "memory_bytes": 34359738368,
    "gpus": 1,
    "tt_boards": 0,
    "scratch_bytes": 107374182400
  },
  "time_limit_seconds": 14400,
  "signal_grace_seconds": 60
}
```

- `kind`:
  - `batch` needs `script`.
  - `command` (from `mist run`) takes `argv` instead of `script`.
  - `interactive` (from `mist shell`) takes neither.
- **The API fills in missing fields** from partition and QoS defaults and
  returns the complete, normalised spec. Defaults live only in the database,
  never in clients.
- **Validation:**
  - The caller must be a member of `team`.
  - `workdir` must be inside `/mist/teams/<team>/`.
  - Resources and time must be within partition and QoS maximums.
  - Every `gpu` job needs `gpus >= 1`.
- **Reply:** `201` with the job object, in state `PENDING`.

### Job object: `GET /v1/jobs/{id}`

```json
{
  "id": 412,
  "name": "resnet-sweep-3",
  "user": "alice",
  "team": "vision",
  "partition": "gpu",
  "qos": "normal",
  "kind": "batch",
  "state": "PENDING",
  "pending_reason": "RESOURCES",
  "pending_detail": "Waiting for 1 GPU; estimated start after job 409 ends",
  "exit_reason": null,
  "exit_code": null,
  "resources": { "cpus": 8, "memory_bytes": 34359738368, "gpus": 1, "tt_boards": 0, "scratch_bytes": 107374182400 },
  "time_limit_seconds": 14400,
  "node": null,
  "workdir": "/mist/teams/vision/alice/resnet",
  "output_path": "/mist/teams/vision/alice/resnet/mist-412.out",
  "submitted_at": "2026-11-02T15:04:05Z",
  "started_at": null,
  "ended_at": null,
  "can_cancel": true,
  "can_attach": false
}
```

**States and reasons** are defined in [scheduler.md](scheduler.md#job-states).
Other teams' jobs appear in `GET /v1/queue` with `user`, `team`, `name`,
`workdir` and `output_path` set to `null` (D15).

### Logs: `GET /v1/jobs/{id}/log`

- **Running job:** returns the live output stream from the executor. Add
  `?follow=1` to stream it over a WebSocket.
- **Finished job:** returns the stored tail (last 64 KiB) from the database.
- **The full log is always the `.out` file in `/mist`.** The response includes
  `output_path` so clients can say where it is.

### Attach: `GET /v1/jobs/{id}/attach` (WebSocket)

Binary frames carry terminal bytes. Text frames carry control messages:

```json
{ "type": "resize", "cols": 120, "rows": 40 }
{ "type": "signal", "name": "SIGINT" }
{ "type": "exit", "code": 0 }
```

Only the job's owner can attach. The CLI's `mist shell` and the later browser
terminal both use this endpoint.

## Versioning

Additive changes (new fields, endpoints or enum values) don't change the
version. Clients must ignore unknown fields and show unknown enum values as
plain text. Breaking changes go to `/v2`.
