# UBAA.Server

Go rewrite of the UBAA relay backend.

## Stack

- Go 1.26+
- Fiber
- FreeCache
- SQLite
- git

## Run

```bash
go run ./cmd/server
```

The default bind address matches the original Ktor backend:

- `SERVER_BIND_HOST=0.0.0.0`
- `SERVER_PORT=5432`
- `SQLITE_PATH=data/ubaa-server.db`

## Compatibility Scope

This repository is initialized from UBAA `upstream/dev` at commit `885db44`.

Implemented:

- Fiber server bootstrap
- Ktor-compatible JSON error shape and user-facing error messages
- Anonymous `/`, `/metrics`, `/health/live`, `/health/ready`
- Prometheus-compatible `/metrics` gauges for sessions, prelogin sessions, feature caches, and storage readiness
- Anonymous app endpoints `/api/v1/app/version` and `/api/v1/app/announcement`
- JWT issue/verify claims compatible with the Ktor backend
- SQLite-backed sessions, refresh tokens, login stats, and persistent cookies
- FreeCache-backed session cache
- WebVPN URL conversion helper
- CAS/SSO login flow skeleton with CAPTCHA detection, redirect following, password-expiry bypass, UC session validation, and cookie persistence
- Full frontend-facing route surface under `/api/v1`
- Upstream-backed implementations for schedule, exam, grade, signin, classroom, SPOC, Judge, evaluation, LibBook, CGYY, BYKC, and YGDK

The frontend should not be rewritten; all Go route paths and JSON DTO field names are kept aligned with the existing shared Kotlin client contract.
