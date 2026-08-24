# Stage 25 privacy, retention, and PostgreSQL recovery

Stage 25 adds an offline, operator-controlled data-minimization boundary. It
can inspect or remove expired administrator sessions, remove deliberately
archived Contact inquiries before an explicit cutoff, and remove one exact
archived inquiry after a reviewed privacy request. It does not put deletion
authority in a public or administrator HTTP handler.

This is an operational mechanism, not a legal retention policy. Zafarmand must
choose and document an inquiry-retention period with the appropriate business,
privacy, and legal owners. The command deliberately has no invented default
number of days and never automatically deletes a `new` or `reviewed` inquiry.

## Apply migration 10 first

Migration `000010_add_inquiry_retention_support` adds:

- `public.inquiry_submission_tombstones`, containing only a 32-byte SHA-256
  digest of an opaque Contact form key and its purge time; and
- `inquiries_archived_updated_at_id_idx`, a partial index over
  `(updated_at, id)` where `status = 'archived'`.

The tombstone contains no visitor name, email address, message, discipline, or
inquiry ID. It prevents an old browser POST from recreating personal data after
the original archived row has been purged. A tombstoned form key receives the
existing safe conflict response and a fresh key; a genuinely new Contact
submission can then be made normally.

Apply the migration with the schema-owner connection described in
[database.md](database.md):

```powershell
go run . migrate status
go run . migrate up
go run . migrate status
```

Do not use `migrate down` as a privacy or content-cleanup technique. Rolling
back migration 10 removes replay tombstones and their protection.

## Dedicated maintenance credential

Maintenance reads only this process variable:

```text
ZAFARMAND_MAINTENANCE_DATABASE_URL
```

It never falls back to `DATABASE_URL`. Do not give the long-running server the
maintenance URL, and do not give the maintenance process the migration owner
or administrator-bootstrap URL. Set the value interactively for only the
current PowerShell process by adapting the secret helper in
[admin-access.md](admin-access.md); never paste it into source, a command-line
argument, terminal transcript, screenshot, issue, or commit.

Remote maintenance must use the same fail-closed transport policy as the
server and migration commands:

```powershell
$env:ZAFARMAND_REQUIRE_DATABASE_TLS = 'true'
```

Only exact `true` and `false` are accepted. `true` rejects a parsed PostgreSQL
connection plan if its primary target or any fallback can use plaintext. A
trusted local PostgreSQL development server may leave the variable absent or
set exact `false`.

### Least-privilege grants

Use provider configuration or deployment automation to create roles; never
put real role names, passwords, or role creation in a schema migration. The
maintenance role needs only the following current data privileges in addition
to database `CONNECT` and schema `USAGE`:

```sql
GRANT SELECT (expires_at), DELETE
    ON TABLE public.admin_sessions
    TO chosen_maintenance_role;

GRANT SELECT (id, submission_key, status, updated_at), DELETE
    ON TABLE public.inquiries
    TO chosen_maintenance_role;

GRANT UPDATE (status)
    ON TABLE public.inquiries
    TO chosen_maintenance_role;

GRANT SELECT (submission_key_hash), INSERT (submission_key_hash)
    ON TABLE public.inquiry_submission_tombstones
    TO chosen_maintenance_role;
```

The narrow `UPDATE (status)` grant is required by PostgreSQL for the retention
query's `SELECT ... FOR UPDATE` row lock; the command never issues an inquiry
update. The role must not receive access to inquiry name, email, message, or
discipline columns beyond the listed key/workflow fields. It also needs no
admin-user, content, media, sequence, schema, migration-ledger, ownership, role,
or grant authority.

The server runtime role remains without `DELETE`. Its Stage 25 table additions
are only:

```sql
GRANT SELECT (submission_key_hash)
    ON TABLE public.inquiry_submission_tombstones
    TO chosen_runtime_role;

GRANT SELECT (version, name, checksum)
    ON TABLE public.schema_migrations
    TO chosen_runtime_role;
```

The first grant protects Contact replay after a purge; the second supports the
readiness ledger check. PostgreSQL normally permits use of its built-in advisory
lock and SHA-256 functions through `PUBLIC`. If the deployment deliberately
revokes that default, grant only the functions each role actually calls:

```sql
GRANT EXECUTE
    ON FUNCTION pg_catalog.pg_advisory_xact_lock_shared(bigint)
    TO chosen_runtime_role;

GRANT EXECUTE
    ON FUNCTION pg_catalog.pg_advisory_xact_lock(bigint),
                pg_catalog.sha256(bytea)
    TO chosen_maintenance_role;
```

Test the exact grants in a non-production environment. A schema owner can hide
an accidentally excessive grant during development.

## Retention commands

The cutoff is an exact, whole-second UTC RFC3339 timestamp ending in `Z` and
must be in the past. Offsets, fractional seconds, whitespace, duplicate flags,
alternate ID spellings, and reordered flags fail before a connection opens.

Preview aggregate counts without changing data:

```powershell
go run . maintenance retention status `
    --inquiries-before 2025-08-22T00:00:00Z
```

The preview reports only the number of expired sessions and eligible archived
inquiries. It does not display a database name, inquiry ID, visitor value,
submission key, digest, SQL statement, or driver diagnostic.

### Stop the web server before every purge

**Stop every Zafarmand web-server instance before running `retention apply` or
`purge-inquiry`, and keep them stopped until the command finishes.** Run only
one destructive maintenance command intentionally at a time.

Stage 25 also uses one transaction-scoped PostgreSQL advisory read/write lock.
A current-version Contact transaction uses explicit `READ COMMITTED`, takes the
shared lock in its own statement, and only then performs its insert and replay
decision. A Contact request that waited for a purge therefore receives a fresh
statement snapshot that can see the committed tombstone. `retention apply` and
`purge-inquiry` also use `READ COMMITTED`; they take the exclusive lock after
database confirmation and before any delete or eligibility query. The
exclusive lock serializes destructive commands and waits for current-version
Contact writes to commit or roll back. `FOR UPDATE` separately prevents a
selected archived row from changing workflow state while it is locked. Every
lock is released automatically with its transaction.

That database lock is defense in depth, not permission to purge during a
rolling upgrade. An older server or any writer that bypasses the repository
does not take the shared lock. Keeping every web instance stopped therefore
remains the Stage 25 operational contract. `retention status` is read-only,
takes no advisory lock, and may be run without stopping the server.

After stopping writes, confirm the exact database shown by a separate safe
connection check and apply retention:

```powershell
$env:PGDATABASE = $env:ZAFARMAND_MAINTENANCE_DATABASE_URL
psql -X --set ON_ERROR_STOP=on --command 'SELECT current_database();'
Remove-Item Env:PGDATABASE -ErrorAction SilentlyContinue

go run . maintenance retention apply `
    --inquiries-before 2025-08-22T00:00:00Z `
    --confirm-database zafarmand_production
```

PostgreSQL supplies `current_database()` inside the same transaction. A value
different from `--confirm-database` aborts before deletion. The command then:

1. deletes sessions with `expires_at <= CURRENT_TIMESTAMP`;
2. selects only inquiries whose status is exactly `archived` and whose
   `updated_at` is strictly earlier than the cutoff;
3. records a SHA-256 submission-key tombstone; and
4. deletes the corresponding inquiry rows.

All steps commit together. If tombstone creation or either deletion fails, the
transaction rolls back, including any session deletion, and releases its
advisory lock. Repeating a completed command is an idempotent zero-row success.
An archived inquiry exactly equal to the cutoff is retained.

`updated_at` is the last real inquiry workflow transition: repeating the same
status preserves it. Moving an inquiry to `archived` therefore starts its
reviewed retention age. Moving it back to `new` or `reviewed` makes it
ineligible regardless of age.

For one reviewed erasure request, first verify and archive the correct inquiry
through the protected interface, stop every server instance, and run:

```powershell
go run . maintenance purge-inquiry `
    --id 123 `
    --confirm-database zafarmand_production
```

The numeric ID is not personal data, but output still does not repeat it. A
missing or non-archived ID is the same neutral no-op. The command cannot purge
a `new` or `reviewed` inquiry.

Remove process secrets after maintenance:

```powershell
Remove-Item Env:ZAFARMAND_MAINTENANCE_DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:ZAFARMAND_REQUIRE_DATABASE_TLS -ErrorAction SilentlyContinue
```

## What deletion does and does not mean

Successful inquiry cleanup removes the personal row from the live logical
database and leaves only an unlinkable high-entropy key digest. It does not
promise immediate physical erasure from PostgreSQL MVCC pages, write-ahead
logs, replicas, provider point-in-time recovery, filesystem snapshots, or
older logical backups. Autovacuum and the provider's WAL/snapshot lifecycle
control those copies; do not run `VACUUM FULL` from the application or a
migration.

Backup, WAL, replica, and log expiration must align with the approved privacy
policy. A restore from an older snapshot must reapply the then-current
retention cutoff before the restored database is exposed. Do not create a new
long-lived ad hoc backup solely to preserve data that a reviewed erasure is
intended to remove.

Administrator emails and password verifiers have a separate access/employment
lifecycle. Product, Interior, Architecture, Homepage, Contact business details,
and reviewed media are editorial records rather than inquiry-retention targets.
Stage 25 does not silently delete them.

## Backup requirements

Integration-test cleanup and `migrate down` are safety checks, not backups. A
production plan must define an approved recovery-point objective, recovery-time
objective, encrypted storage location, access owners, retention period,
regional policy, monitoring, and restore-drill schedule.

For a portable logical archive, use a dedicated read-only backup credential
through process environment and a `pg_dump` client that is compatible with the
server. A reviewed invocation should use the equivalent of:

```text
pg_dump
  --format=custom
  --no-owner
  --no-privileges
  --exclude-table-data=public.admin_sessions
  --file <new-temporary-path>
```

Do not put a credential-bearing URL in the argument list. Write to a new
temporary file outside the repository, require restrictive filesystem/storage
permissions, run `pg_restore --list`, calculate a SHA-256 checksum, and rename
the archive atomically only after both commands succeed. Never overwrite the
last known good archive. `/backups/`, `*.pgdump`, `*.pgdump.sha256`,
`*.pgdump.tmp`, and `*.sha256.tmp` are ignored as a secondary guard, not as
permission to store production data in the checkout.

Session table structure belongs in the archive, but its rows are deliberately
excluded. Sessions are ephemeral and restoring them can resurrect a bearer
session that was logged out after the snapshot. The archive still contains
inquiry personal data, administrator emails and password verifiers, business
contact details, and media bytes, so it remains highly confidential despite
the session exclusion.

`pg_dump` does not provide the complete external role, secret, network, TLS,
scheduled-job, or provider configuration. Keep reviewed role/grant definitions
in deployment automation and reapply them during recovery.

## Restore and restore rehearsal

A file listing and checksum cannot prove recoverability. Regularly restore into
a new isolated database and test the result. For a rehearsal:

1. Verify the archive checksum before connecting.
2. Create one empty database with an unmistakable `_restore_test` suffix; never
   reuse or clean an existing database.
3. Restore with the schema-owner credential and the equivalent of:

   ```text
   pg_restore
     --exit-on-error
     --single-transaction
     --no-owner
     --no-privileges
     --dbname <fresh-target>
     <verified-archive>
   ```

   Supply the credential through a protected environment, service definition,
   or password file; never put a credential-bearing URL in `--dbname`.

4. Run the matching application binary's `migrate status`. Verify the exact
   migration checksums before applying a later application release.
5. Reapply and test reviewed runtime, maintenance, backup, and bootstrap grants.
6. Confirm `admin_sessions` is empty for a logical restore.
7. Run schema and aggregate-count smoke checks using synthetic data; never copy
   inquiry values, password hashes, session digests, or media into transcripts.
8. Run retention against the restored copy before any possible promotion.
9. Remove only the exact rehearsal database and synthetic archive created by
   the exercise.

A provider physical or point-in-time restore includes session rows. Before
exposure, an authorized recovery operator must invalidate all restored
sessions—not merely expired ones—so a rolled-back logout cannot regain access.
Keep the application stopped until that invalidation, retention, grant checks,
and smoke tests succeed. Promote a verified fresh database or switch the
service connection; do not use broad `--clean` restore over the live database.

## Verification

Database-independent checks:

```powershell
go fmt ./...
go test -count=1 ./...
go vet ./...
```

The ordinary tests cover strict command grammar, isolated configuration and TLS
policy, canonical cutoff/ID validation, the fixed 2-minute command context,
aggregate output,
database confirmation, explicit `READ COMMITTED` transactions, shared/exclusive
advisory-lock ordering and failure redaction, transaction rollback, SQL
parameters, archived-only selection, tombstone-aware Contact replay, and
migration-10 SQL safety.

The separately confirmed `ZAFARMAND_TEST_DATABASE_URL` suite additionally
checks the migration-10 table/index lifecycle, real PostgreSQL expiration and
cutoff boundaries, wrong-database refusal, archived-only bulk and targeted
purges, retained active/revoked sessions, tombstone digests, idempotent repeats,
lock serialization against a concurrent Contact write, and the rule that a
purged Contact key cannot recreate personal data. It uses only synthetic
reserved-domain values and the disposable guard from
[database.md](database.md); never point it at a development or production
database.

CI separately enables the least-privilege grant smoke test with an exact
disposable-cluster confirmation because its temporary `NOLOGIN` roles are
cluster-global. The database-only confirmation never authorizes that role DDL;
do not enable the cluster gate on a normal local, shared, hosted, or production
PostgreSQL cluster.
