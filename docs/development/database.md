# Stage 13-18 database development on Windows

Stage 13 establishes an explicit PostgreSQL migration workflow. Stage 14 uses
that foundation for the first database-backed public feature: Contact inquiry
persistence. Stage 15 adds administrator identities and revocable sessions;
the separate [admin access guide](admin-access.md) explains that security slice.
Stage 16 adds a protected, read-only inquiry list and detail reader without a
schema migration; its exact data boundary is documented in the [administrator
inquiry guide](admin-inquiries.md).
Stage 17 adds an explicit administrator status writer using the table's
existing `status`, `updated_at`, and closed status constraint, so it also needs
no schema migration.
Stage 18 adds migration 4 and the first database-backed public catalogue. The
new `public.products` table stores minimal durable identity, public text,
ordering, publication-state, and timestamp data for the catalogue; the
complete Stage 18 behavior is documented in the
[Products development guide](products.md).

The boundary is intentional:

- `go run .` reads `DATABASE_URL`, opens one shared pool, and never applies
  migrations automatically.
- Only `go run . migrate ...` changes schema.
- No public HTTP request is allowed to run a migration.
- A valid Contact POST inserts one inquiry through a narrow repository and then
  uses Post/Redirect/Get.
- A per-form key makes retries idempotent; the same submission cannot create a
  second row.
- The private login stores one-way password verifiers and only digests of the
  browser's session and CSRF secrets.
- Authenticated owners and editors can read bounded inquiry pages through a
  separate repository; the list does not select email, message, or submission
  key data.
- Authenticated and explicitly authorized owners and editors can manually set
  one supported status through another narrow repository; opening a detail
  page never performs that update.
- The public Products repository can select only published catalogue fields.
  Migration 4 inserts no sample products, and draft or archived records remain
  indistinguishable from missing records on public detail routes.
- The success page claims only that the inquiry was saved for studio review. It
  does not claim email delivery or guarantee a response time.

Keeping schema management separate from request handling makes database changes
visible, reviewable, and deliberate. It also prevents two server processes from
trying to alter the same schema during startup.

## Prerequisites

You need all of the following before running a migration command:

1. The Go version required by `go.mod`.
2. A supported PostgreSQL server available to your Windows account.
3. A dedicated local development database and credentials for it.

PostgreSQL's `psql` command-line client is additionally required for the manual
verification steps in this guide. The Go migration command itself connects
through pgx and does not launch `psql`.

PostgreSQL and `psql` must be installed and discoverable before the live checks
in this guide can run. Ordinary unit tests remain database-independent.

Use the [official PostgreSQL Windows download
page](https://www.postgresql.org/download/windows/) and the documentation for
the supported release you select. Installer choices, service names, and
installation directories differ between releases and machines, so this guide
does not invent a version-specific path or setup sequence.

After installation, open a new PowerShell window and check what is actually
available:

```powershell
psql --version
Get-Command psql
Get-Service | Where-Object { $_.Name -like 'postgresql*' }
```

If `Get-Command` cannot find `psql`, follow the documentation for the installed
distribution to make its command-line tools available. Do not paste a guessed
versioned `Program Files` path into project documentation.

The [official `psql`
reference](https://www.postgresql.org/docs/current/app-psql.html) describes its
connection strings, commands, and exit behavior.

## Supplying `DATABASE_URL` safely

Migration commands and the public server each read one required environment
variable named `DATABASE_URL`. Set it only for the current PowerShell process:

```powershell
$env:DATABASE_URL = 'postgres://<user>:<password>@localhost:5432/<database>?sslmode=disable'
```

Replace every angle-bracketed value locally. Percent-encode characters that
have a special meaning inside a URL. `sslmode=disable` is suitable only for a
trusted local development server; deployment must use the TLS policy required
by its PostgreSQL provider.

Use a direct PostgreSQL endpoint, or a pooler explicitly configured for session
pooling, for migration commands. The runner holds a PostgreSQL session advisory
lock on one pinned connection for each operation. A transaction-pooling proxy
can switch server sessions between statements, so it cannot preserve that lock.
If a hosted provider offers both "direct" and "pooled" URLs, use its documented
direct migration URL; do not guess from the hostname.

PowerShell session variables disappear when that process closes. Avoid `setx`
for database passwords because it persists the value for future processes.
Remove the current value explicitly when finished:

```powershell
Remove-Item Env:DATABASE_URL
```

Use separate PostgreSQL roles and connection strings even though both commands
read the same variable name. A migration session needs a schema-owner role that
can create and alter the migration ledger, tables, constraints, and sequences.
After migrations finish, replace `DATABASE_URL` in the server's environment
with a least-privilege runtime role. The current server needs database/schema
connection privileges; `INSERT` and `SELECT` on `public.inquiries` plus
column-level `UPDATE` on only its `status` and `updated_at`; `SELECT` on
the `id`, `slug`, `name`, `category`, `sort_order`, and `publication_status`
columns of `public.products`; `SELECT` on
`public.admin_users`; and `SELECT`, `INSERT`, and `UPDATE` on
`public.admin_sessions`. The broader inquiry read is required by Stage 16's
protected list and detail statements, while the narrow column update is
required by Stage 17's status statement. Stage 18 needs no Product insert,
update, delete, or identity-sequence permission because its repository is
read-only. Separate application-level interfaces keep administrative inquiry
capabilities unavailable to the public Contact handler and keep unpublished
Product state outside public templates. Inquiry and administrator identity
sequences need the access required by their existing writers. The separate
administrator bootstrap process additionally needs `INSERT` on
`public.admin_users`, but neither runtime should own the schema or receive
migration privileges. Keep role creation and credentials outside this
repository and follow the deployment provider's role-management documentation.

Never:

- commit a real connection string;
- paste one into source code, migration SQL, screenshots, issues, or commit
  messages;
- print `DATABASE_URL` while diagnosing configuration;
- include it in application logs or wrapped errors; or
- reuse production credentials for local development or tests.

The repository ignores `.env` and `.env.*` files while permitting a future
credential-free `.env.example`. Go does **not** automatically load `.env`
files, and this project intentionally adds no dotenv package. Creating a local
`.env` file therefore does not configure the program; use the PowerShell
environment variable shown above.

You can confirm the ignore rules without displaying a secret:

```powershell
git check-ignore -v .env
git check-ignore -v .env.local
git check-ignore .env.example
```

The first two commands should identify `.gitignore`. The last command should
produce no match because `.env.example` is explicitly allowed.

## Understanding the Go connection lifecycle

Stages 13-18 use `database/sql` with pgx as the PostgreSQL driver. A `*sql.DB` is
a concurrency-safe database handle and connection pool, not one permanently
open socket.

The explicit migration command owns its complete lifecycle:

```text
Read DATABASE_URL
        ↓
sql.Open creates the handle
        ↓
PingContext verifies a real connection with a bounded timeout
        ↓
Run the selected migration operation
        ↓
Close releases the handle on every return path
```

`sql.Open` may validate its arguments without contacting PostgreSQL. That is
why a successful `PingContext` is the startup boundary before migration SQL can
run. If configuration or the ping fails, the command exits without applying a
migration and without echoing the connection string.

The migration command opens one handle for the command, shares it with the
migration code, and closes it once. Individual migration functions do not own
or close the pool. Opening a new pool for each statement would hide ownership,
waste connections, and teach the wrong request lifecycle.

In Stages 14-18, the long-running application—not an HTTP handler—owns the
shared pool. The public inquiry writer, public Product reader, private inquiry
reader, private inquiry status updater, and administrator authentication
repository borrow it and accept request contexts.
The server opens and pings the pool before listening, stops accepting requests
on Ctrl+C, waits for active handlers, and then closes the pool.

See the official Go documentation for [`sql.DB`
connection management](https://go.dev/doc/database/manage-connections) and the
[pgx `database/sql` compatibility
layer](https://github.com/jackc/pgx/tree/master/stdlib).

## Migration commands

Run every command from the repository root, in the same PowerShell process in
which `DATABASE_URL` is set.

### Inspect migration state

```powershell
go run . migrate status
```

`status` reports the applied and pending versions. It does not start the public
server and does not apply a pending business migration. On an empty database it
does create the runner-owned `public.schema_migrations` metadata table, so the
migration role needs permission to create that table even for the first status
check.

### Apply pending migrations

```powershell
go run . migrate up
```

`up` validates the recorded migration history and applies pending versions in
ascending order. Running it again when nothing is pending should be a truthful
no-op.

### Roll back the latest migration

```powershell
go run . migrate down --confirm
```

`down` is destructive and intentionally requires the literal `--confirm`
argument. It rolls back only the newest applied migration.

Use `down` only with a dedicated disposable database created for migration
practice. Never point it at production, shared development data, or the local
database containing anything you need to retain. Check the database name in the
connection information before confirming a rollback.

## Authoring the next migration

Migration history is compiled into the Go executable from `migrations/`. Every
new schema version must follow one strict contract:

1. Add an exact pair such as
   `000005_descriptive_name.up.sql` and
   `000005_descriptive_name.down.sql`.
2. Use six digits, the next contiguous positive version, and a globally unique
   lowercase name made from words separated by single underscores.
3. Put the forward change in `up` and the narrow exact reverse in `down`. Avoid
   `CASCADE` and broad cleanup behavior unless a later stage explicitly proves
   that they are necessary and safe.
4. Keep SQL static. Visitor or administrator values belong in parameterized Go
   repository queries, never in migration files.
5. Do not write `BEGIN`, `START TRANSACTION`, `COMMIT`, `END`, `ROLLBACK`,
   `ABORT`, `PREPARE TRANSACTION`, or savepoint commands. The Go runner owns the
   transaction surrounding each migration and rejects those commands while
   loading the catalog.
6. Do not use PostgreSQL operations that are forbidden inside a transaction,
   such as `VACUUM`, `CREATE DATABASE`, or `CREATE INDEX CONCURRENTLY`. They
   fail safely rather than partially applying, but they require a separately
   reviewed migration strategy if the project ever needs them.

A file may contain multiple ordinary SQL statements. pgx executes the static,
zero-argument migration text as one simple-protocol script inside the runner's
transaction. The schema SQL and its ledger insert therefore commit together;
if any statement fails, that version is not recorded.

The ledger stores a SHA-256 checksum over the version, name, up SQL, and down
SQL. Never edit or rename a migration after it has been applied anywhere. A
changed historical pair is reported as checksum drift; create a new version to
correct or extend the schema. Git stores migration SQL with LF line endings so
the same logical files receive the same checksum on Windows and deployment
hosts.

Running any migration command without `DATABASE_URL` should fail safely:

```powershell
Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue
go run . migrate status
```

The error should identify the missing variable but must not print a connection
string. Set the variable again before continuing.

## Manual verification with `psql`

Avoid putting a credential-bearing URL directly in `psql`'s command-line
arguments, where another local process inspector could display it. For this
PowerShell session, let libpq read the same connection string through its
`PGDATABASE` environment variable instead:

```powershell
$env:PGDATABASE = $env:DATABASE_URL
```

Environment variables still contain secrets and must be protected, but this
keeps the URL out of the visible command arguments. A PostgreSQL password file
or service definition is a better long-lived setup; follow PostgreSQL's
official [libpq environment-variable
documentation](https://www.postgresql.org/docs/current/libpq-envars.html) and
[password-file documentation](https://www.postgresql.org/docs/current/libpq-pgpass.html)
rather than committing either file to this repository.

First prove that the URL reaches the intended database:

```powershell
psql -X --set ON_ERROR_STOP=on --command '\conninfo'
```

Inspect schema state before applying migrations:

```powershell
go run . migrate status
psql -X --set ON_ERROR_STOP=on --command '\dt'
```

Apply the pending migration and inspect the resulting schema:

```powershell
go run . migrate up
go run . migrate status
psql -X --set ON_ERROR_STOP=on --command '\dt'
psql -X --set ON_ERROR_STOP=on --command '\d inquiries'
psql -X --set ON_ERROR_STOP=on --command '\d admin_users'
psql -X --set ON_ERROR_STOP=on --command '\d admin_sessions'
psql -X --set ON_ERROR_STOP=on --command '\d products'
psql -X --set ON_ERROR_STOP=on --command '\di products_published_order_idx'
```

Inspect table definitions only. Do not select or copy password hashes, session
digests, or CSRF digests into terminals, screenshots, tickets, or documentation.

Stage 14 connects the public Contact route to the inquiry repository. Start the
server in the same PowerShell process so it can read `DATABASE_URL`:

```powershell
go run .
```

Open `http://localhost:8080/contact`, submit one test inquiry, and verify the
browser reaches `/contact#contact-form-response` with the saved-for-review
confirmation. Refreshing that GET must not submit again or repeat the one-time
confirmation. Stop the server with `Ctrl+C`, then inspect only disposable test
values—never copy a real visitor's personal information into logs or docs:

```powershell
psql -X --set ON_ERROR_STOP=on --command 'SELECT name, email, discipline, status FROM inquiries ORDER BY id DESC LIMIT 1;'
```

The `name` and `email` columns must match the normalized form values, discipline
must be one supported machine value, and the initial status must be `new`.
Submitting the exact same already-rendered form twice must leave one row because
its hidden `submission_token` maps to the unique `submission_key` column.

Stages 16 and 17 reuse this table and its primary-key index; neither required a
new inquiry migration. Stage 17 also reuses the existing status constraint and
`updated_at` column. A same-status write succeeds while preserving the stored
timestamp value. A real transition changes `status` and assigns PostgreSQL's
`CURRENT_TIMESTAMP` to `updated_at`; finite timestamp precision means that
value cannot be promised to advance strictly on two immediate transitions.
Concurrent administrators use documented last-successful-write-wins behavior
rather than a hidden versioning promise. Migration 4 belongs to the separate
Stage 18 Product catalogue and does not alter inquiry storage.
Its protected inbox orders by descending ID and requests older pages with the
exclusive `before=<id>` keyset. Verify that interface with one fictional local
Contact submission rather than selecting a real visitor's personal data into a
terminal. The [administrator inquiry guide](admin-inquiries.md) gives the exact
read and manual-status browser checks, explains why the 20-item reader does not
use `OFFSET`, and documents the receipt-order timestamp caveat.

Migration 4 creates `public.products` and its partial
`products_published_order_idx`. It deliberately inserts no business records,
so `SELECT COUNT(*) FROM public.products` must return zero immediately after a
fresh migration. The public reader selects only published rows and does not
receive draft or archived state as display data. Use the [Products development
guide](products.md) for schema details, fictional local verification data, and
the public list/detail behavior.

To exercise rollback, first switch `DATABASE_URL` to a separate disposable
database. Reconfirm the connection before doing anything destructive:

```powershell
# Run this immediately after assigning the disposable URL to DATABASE_URL.
$env:PGDATABASE = $env:DATABASE_URL
psql -X --set ON_ERROR_STOP=on --command '\conninfo'
go run . migrate up
go run . migrate down --confirm
go run . migrate status
psql -X --set ON_ERROR_STOP=on --command '\dt'
```

Apply `up` again if that scratch database should finish in the current schema
state. Do not use rollback verification as a cleanup strategy for valuable
data.

Remove libpq's copied connection value when manual inspection is finished:

```powershell
Remove-Item Env:PGDATABASE -ErrorAction SilentlyContinue
```

## Optional live PostgreSQL integration test

The normal suite never connects to PostgreSQL. Separate integration tests cover
the advisory lock, real DDL, multi-statement rollback, ledger, status,
idempotent up/down/reapply, the public inquiry repository's name/email mapping
and retry behavior, the private Stage 16 list/detail reader, and the Stage 17
status writer's transition, no-op, and safe-failure behavior. Stage 18 adds the
real Product schema plus published-list and published-detail repository checks.

On the verified local Windows PostgreSQL 18 installation, the guarded helper
can run the same acceptance cycle without persisting or printing a password:

```powershell
.\stage14_postgres_check.ps1
```

The helper keeps its historical Stage 14 filename and disposable database name,
but its `Postgres` test selection runs the complete current v1-to-v4 suite,
including Stage 15 admin schema/repository checks and Stage 16 inquiry reads.
It also checks Stage 17 status updates and Stage 18's unseeded Product schema,
published ordering, numbering, detail mapping, and non-public exclusion.

The helper uses a visible secure password prompt, refuses to reuse a database,
creates only `zafarmand_stage14_codex_test`, runs the opt-in integration tests
and migration CLI status/up checks, and then drops only the database it created.
The integration test itself covers one-version-at-a-time rollback and reapply.
A failed cleanup is reported explicitly rather than broadening the deletion
target.

Create a dedicated empty database whose name ends in `_test`. It must not
contain `public.inquiries`, `public.admin_users`, `public.admin_sessions`,
`public.products`, `public.schema_migrations`,
`public.stage13_atomicity_probe`, or
`public.stage13_intentionally_missing_table`. Then opt in with two test-only
variables:

```powershell
$env:ZAFARMAND_TEST_DATABASE_URL = 'postgres://<user>:<password>@localhost:5432/zafarmand_stage14_test?sslmode=disable'
$env:ZAFARMAND_TEST_DATABASE_CONFIRM = 'stage13-disposable-database'
go test -count=1 -run 'Postgres' ./...
```

The test never falls back to `DATABASE_URL`, verifies both the configured and
server-reported database names, and checks every reserved relation before
mutation. It cleans up only `public.admin_sessions`, `public.admin_users`,
`public.products`, `public.inquiries`, `public.schema_migrations`, and its exact
atomicity-probe table; the intentionally missing relation is checked but never
dropped. Still use a database created solely for this test; the confirmation
is a safety gate, not a backup.

Remove the test credentials afterward:

```powershell
Remove-Item Env:ZAFARMAND_TEST_DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:ZAFARMAND_TEST_DATABASE_CONFIRM -ErrorAction SilentlyContinue
```

The integration tests skip only when their explicit opt-in variables are
absent; they never fall back to development credentials. Once opted in, an
unreachable PostgreSQL server is a test failure. The Go tests connect through
pgx and do not require `psql`; `psql` is required only for the guarded helper
and the manual inspection commands above. Run the selected check and record its
result before treating the local database runtime as verified.

## Normal development checks

The ordinary automated suite, including Stage 16 reads, Stage 17 status
authorization, and Stage 18 Product migration, repository, handler, and
template checks, must remain database-independent. It should pass even when
PostgreSQL is stopped or absent:

```powershell
go fmt ./...
go test ./...
go vet ./...
```

The opt-in integration test described above is intentionally skipped unless its
separate test URL and confirmation are both supplied. It must never silently
reuse `DATABASE_URL` from a development or production environment.

Before committing a database stage, review only intended files:

```powershell
git status --short
git diff -- .gitignore .gitattributes docs/development/database.md docs/development/products.md
git diff -- migrations
```

Migration SQL is stored with LF line endings through `.gitattributes`. Once a
migration file exists, verify its effective attribute with its real path:

```powershell
git check-attr eol -- migrations/000001_create_inquiries.up.sql
git check-attr eol -- migrations/000002_add_inquiry_submission_key.up.sql
git check-attr eol -- migrations/000003_create_admin_access.up.sql
git check-attr eol -- migrations/000004_create_products.up.sql
git check-attr eol -- migrations/000004_create_products.down.sql
```

The reported value should be `lf`. Use the actual migration filename if it
differs; do not create a second file only to satisfy this example.
