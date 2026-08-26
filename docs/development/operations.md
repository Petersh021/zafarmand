# Production operations and security runbook

Stage 25 turns the existing application into a bounded, observable service
without making request logs a second store of visitor or administrator data.
This guide is the deployment contract for the HTTP listener, HTTPS edge,
PostgreSQL transport, health probes, structured logs, and overload behavior.
Retention and backup procedures are documented separately in
[`retention.md`](retention.md).
Stage 26 release evidence and the target-specific sign-off inventory are in
[`acceptance-deployment.md`](acceptance-deployment.md).

## Required release baseline

Build and test with the exact minimum Go release recorded by `go.mod`; do not
silently substitute an older local toolchain. The module pins pgx rather than
using an unreviewed floating version. Before a release, CI must successfully
run module verification, formatting checks, `go vet`, race-enabled tests, the
guarded PostgreSQL integration suite, and `govulncheck`.

The tracked workflow obtains Go from `go.mod` and uses a disposable PostgreSQL
18 service whose database name ends in `_test` and whose destructive-test
confirmation is fixed for CI. Its independent cluster confirmation authorizes
temporary smoke-role creation only inside that job-owned PostgreSQL service. It
supplies `ZAFARMAND_TEST_DATABASE_URL` as the sole connection string, never
`DATABASE_URL` or a production credential. Repository or platform branch
protection must require this workflow before a release is promoted.

Dependency updates are ordinary reviewed code changes. Never run an automatic
production update that can replace the compiled binary without the repository's
tests and migration checks.

## Runtime environment

The server reads these values at startup:

| Variable | Required | Contract |
| --- | --- | --- |
| `DATABASE_URL` | Yes | Least-privilege runtime PostgreSQL connection string. Never log or commit it. |
| `ZAFARMAND_REQUIRE_DATABASE_TLS` | No | Exact `true` or `false`; production should use `true`. When true, every pgx target and fallback must use TLS. |
| `ZAFARMAND_HTTP_ADDRESS` | No | Explicit host and numeric port. Defaults to `127.0.0.1:8080`. |
| `ZAFARMAND_EXTERNAL_HTTPS` | No | Exact `true` or `false`. Set `true` only when every public request passes through the reviewed HTTPS edge. |

An omitted or false `ZAFARMAND_EXTERNAL_HTTPS` permits only a loopback HTTP
listener. A wildcard or any host other than `localhost` or a loopback IP fails
startup unless external HTTPS is explicitly declared. This prevents an
accidental `:8080` configuration from publishing an unencrypted service.

For a local PostgreSQL server, a development-only URL may use
`sslmode=disable`. A production URL should use the provider's authenticated TLS
mode, normally `sslmode=verify-full`, together with
`ZAFARMAND_REQUIRE_DATABASE_TLS=true`. The boolean is a fail-closed transport
check; it is not a replacement for certificate and hostname verification in
the connection string.

Example local PowerShell configuration:

```powershell
# First define Set-SecretProcessVariable from admin-access.md, then enter the
# complete PostgreSQL URL at its hidden prompt so it is absent from history.
Set-SecretProcessVariable `
    -Name 'DATABASE_URL' `
    -Prompt 'Enter the PostgreSQL URL for the runtime role'
$env:ZAFARMAND_REQUIRE_DATABASE_TLS = 'false'
$env:ZAFARMAND_HTTP_ADDRESS = '127.0.0.1:8080'
$env:ZAFARMAND_EXTERNAL_HTTPS = 'false'
go run .
```

Use the `Set-SecretProcessVariable` definition in
[`admin-access.md`](admin-access.md#creating-the-first-administrator-safely).
Do not copy a real URL into source, documentation, screenshots, tickets,
shell-history examples, or commit messages.

## HTTPS edge contract

The Go process currently serves HTTP; production TLS terminates at a reviewed
reverse proxy or platform edge. That edge must:

1. redirect public HTTP to HTTPS before forwarding application traffic;
2. use a valid certificate and current TLS policy;
3. forward only to a non-public or firewall-restricted application listener;
4. replace, rather than append to, forwarding headers supplied by clients;
5. impose request, connection, and per-client rate limits, especially for
   `POST /admin/login` and `POST /contact`;
6. restrict `/health/live` and `/health/ready` to trusted platform probes and
   rate-limit them outside the process; and
7. preserve the application response's security and cache-control headers.

The application deliberately ignores `Forwarded` and `X-Forwarded-Proto` when
deciding whether cookies are `Secure`. An attacker-controlled header therefore
cannot upgrade or downgrade cookie policy. Direct TLS in tests, or the reviewed
process-level `ZAFARMAND_EXTERNAL_HTTPS=true` declaration, marks the request as
secure. The same declaration enables HSTS. Do not set it when the listener is
directly reachable over public plaintext HTTP.

The application also does not retain or log client IP addresses. Per-client
abuse controls belong at the trusted edge, where the real transport peer is
known. The process still has a small nonblocking concurrency guard around
administrator lookup and password derivation, but that CPU safeguard is not an
edge rate limiter.

## Health and readiness

The service exposes two fixed, non-cacheable endpoints:

```text
GET or HEAD /health/live
GET or HEAD /health/ready
```

`/health/live` returns `200 OK` when the process can handle HTTP. It does not
query PostgreSQL and bypasses the application concurrency limit, so a database
outage or traffic saturation does not create a restart loop.

`/health/ready` returns `200 OK` only when PostgreSQL responds and
`public.schema_migrations` is the complete, checksum-valid embedded migration
catalog. Missing, pending, reordered, renamed, or modified migrations produce
the same fixed `503 Service Unavailable` response. The endpoint never returns
SQL, a database host, credentials, or migration details. The runtime database
role therefore needs read access to the migration ledger.

Use liveness for process restart decisions and readiness for load-balancer
admission. Do not use liveness as proof that database-backed pages are usable.
Both paths accept only GET and HEAD; other methods return 405. They do not
provide application authentication, and liveness intentionally bypasses the
64-request application limit. Keep both paths behind the platform firewall or
probe ACL so public traffic cannot use liveness to consume connection and log
capacity outside that bound.

## Bounded resources and shutdown

One process uses these explicit ceilings:

- 64 admitted application/readiness requests, with immediate non-cacheable
  `503` and `Retry-After: 1` backpressure when full;
- exactly 2 concurrent administrator account-lookup/password-work operations,
  with immediate neutral backpressure when full;
- 10 open and 5 idle PostgreSQL connections, with a 5-minute maximum idle time
  and 30-minute maximum connection lifetime;
- a 2-second PostgreSQL and migration-ledger readiness deadline;
- 5-second header, 15-second request, 30-second response, and 60-second idle
  HTTP limits;
- a 64 KiB request-header ceiling, in addition to the smaller form and media
  limits owned by individual handlers; and
- a 5-second graceful-shutdown window.

The process handles both interrupt and termination signals. It stops accepting
connections, waits within the shutdown window, then closes the shared database
pool. The service manager should send `SIGTERM` and allow more than five seconds
before a forced kill.

These numbers are a deliberate single-process starting budget, not universal
capacity claims. The Stage 26 repository review keeps them unchanged because no
production target or representative load profile has been supplied. A target
owner may lower or raise them only from measured PostgreSQL capacity, handler
latency, error rate, and resource saturation.

## Privacy-safe structured logs

Operational logs are newline-delimited JSON on standard error. A normal request
record contains only:

```text
event/message: http_request
request_id
method
matched route pattern, or unmatched
status
response bytes
duration_ms
```

The server replaces inbound `X-Request-ID` values with a server-generated,
process-owned identifier that contains no request or visitor value. It derives
the 128-bit identifier by authenticating an internal counter with a random
per-process HMAC-SHA256 key, so a returned ID does not reveal request order or
intervening request volume. The identifier is for correlation, not
authentication or authorization.

The application does not log raw paths, query strings, client addresses,
request or response headers, form fields, email addresses, messages, cookies,
session values, CSRF values, database URLs, SQL diagnostics, template data, or
panic values. HTTP-server and panic diagnostics collapse to fixed event
categories. Only request-aware events are guaranteed to carry a request ID.
Fixed legacy application failure messages are bridged into the JSON handler at
`ERROR` severity, but they do not gain request context; correlate those records
by service instance and time, then inspect nearby request records' route
patterns and response statuses.

Treat logs as access-controlled operational data even with those exclusions.
Set a documented platform retention period, encrypt stored logs, restrict
export, and test deletion. Do not add a request-body logger or broad error
serialization in the proxy, application, or log collector.

Alert on trends rather than visitor identifiers. Useful signals are:

- readiness failures or a process missing from the ready pool;
- sustained 5xx rates or rising duration for a matched route pattern;
- repeated global backpressure responses;
- repeated administrator login saturation responses;
- PostgreSQL connection saturation or availability failures; and
- failed or forced shutdown events.

## Release and incident checklist

For a normal release:

1. complete the backup procedure and verify its checksum;
2. run the reviewed migration command with the schema-owner URL;
3. replace it with the least-privilege runtime URL before starting the server;
4. start the candidate with the reviewed listener, HTTPS, and database-TLS
   settings;
5. require successful readiness before admitting traffic; and
6. smoke-test public pages, Contact, administrator login/logout, and one
   authorized read without copying personal data into the release record.

If readiness fails, remove the instance from traffic and inspect fixed process
events plus migration status using the operator role. Do not weaken readiness,
disable TLS checks, expose database errors, or apply an unreviewed migration to
make the probe green. If a release must be rolled back, first verify that its
older binary understands the already-applied schema; database rollback remains
an explicit, separately confirmed operation on disposable environments unless
a reviewed production recovery plan says otherwise.
