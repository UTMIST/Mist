# Mist member authentication

Small Node service using maintained Better Auth 1.7.7 and better-sqlite3 13.0.3.
It is internal to `mist-system`; the Go gateway proxies `/auth` on the website's
origin and checks sessions before authorizing data routes. No public signup.

`npm ci && npm start` requires Node 22.16 or newer and these environment variables:

- `BETTER_AUTH_SECRET`: protected random secret, at least 32 characters.
- `MIST_SITE_ORIGIN`: exact website origin; HTTPS enables Secure cookies.
- `MIST_ALLOWED_ORIGINS`: comma-separated trusted origins.
- `MIST_AUTH_DATABASE`: local SQLite path (default `./data/auth.sqlite`).
- `MIST_ADMIN_EMAIL`, `MIST_ADMIN_PASSWORD`: only needed when initializing an empty
  database. The first user is an administrator. Existing users are never reset.
- `HOST`, `PORT`: listener (default localhost:3002).

Never put a real secret or bootstrap password in the image, Git, shell arguments
or logs. Kubernetes injects the protected Secret. SQLite uses WAL, a busy timeout
and startup migrations on a persistent local PVC, with one Recreate replica.
Database-backed rate limiting survives restarts. Upload/storage metadata live
in Go/NFS, not in this auth database. Member passwords use the library's password
implementation; Go does not hash passwords or mint authentication tokens.

See [operations and recovery](../deploy/private/README.md) for online backups,
restoration, private HTTP/HTTPS configuration and deployment.
