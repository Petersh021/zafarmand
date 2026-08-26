# Zafarmand

Zafarmand is a server-rendered Go and PostgreSQL website for a design studio.
It includes the public catalogue and Contact experience, protected editorial
workflows, guarded migrations, inquiry retention, and production operations
boundaries.

## Project status

Stages 1–25 are complete. Stage 26 is the final acceptance and deployment
stage. The repository can produce and smoke-test a provider-neutral Linux/amd64
container;
an actual public deployment still requires an approved provider, domain/TLS
edge, PostgreSQL roles, secret manager, policy owners, and production content.
Those external decisions are tracked in the
[Stage 26 acceptance and deployment record](docs/development/acceptance-deployment.md).

## Verification

Use the exact Go release declared by `go.mod`:

```text
go mod verify
go test -count=1 ./...
go vet ./...
```

PostgreSQL integration tests are destructive and remain disabled unless the
separate disposable-database URL and exact confirmation are supplied. Follow
the [database guide](docs/development/database.md); never point the suite at a
development, shared, staging, or production database.

## Runtime

The HTTP server requires a least-privilege `DATABASE_URL`. Migrations and
administrator bootstrap use separate role-specific processes and credentials.
Templates and static files are runtime dependencies, so copying only the Go
binary is not a valid web deployment. The tracked container and CI smoke gate
package and verify the complete runtime filesystem.

Start with these guides:

- [Database and migrations](docs/development/database.md)
- [Administrator access](docs/development/admin-access.md)
- [Production operations](docs/development/operations.md)
- [Retention, backup, and recovery](docs/development/retention.md)
- [Stage 26 acceptance and deployment](docs/development/acceptance-deployment.md)
- [Working roadmap](docs/development/roadmap.md)

Never commit or print a database URL, password, session value, inquiry, backup,
or other production secret. Deployment platforms must inject secrets through
their approved secret facility.
