# Stage 23 PostgreSQL-backed Architecture projects

Stage 23 replaces the temporary Architecture studies with one complete,
server-rendered vertical slice. PostgreSQL owns project facts, publication
state, optimistic revision, and one reviewed cover. Public routes read only
Published records; protected routes let authenticated Owners and Editors manage
every lifecycle state.

Architecture remains a separate discipline model. The implementation reuses
the reviewed-image validation mechanics established in Stages 21 and 22, but it
does not merge Product, Interior, and Architecture records into a generic table.

## Included scope

This stage includes:

- an unseeded Architecture project schema and one-current-cover relation;
- published-only public list, detail, and exact-revision cover reads;
- protected all-state list and detail pages;
- protected create and optimistic edit forms;
- explicit Draft, Published, and Archived lifecycle selection;
- reviewed JPEG or PNG upload and replacement;
- responsive image/fallback presentation; and
- ordinary, repository, HTTP, migration, and guarded PostgreSQL tests.

This stage does not add deletion, galleries, drag ordering, cropping, focal
points, client or site-area fields, featured records, SEO fields, public preview
tokens, or real studio content.

## Migration 8

The immutable migration pair is:

```text
migrations/000008_create_architecture_projects.up.sql
migrations/000008_create_architecture_projects.down.sql
```

`public.architecture_projects` stores:

- a database-owned positive identity;
- a unique canonical slug;
- required title, typology, and real-world project status;
- optional location, year, and description;
- positive editorial order;
- the closed `draft`, `published`, or `archived` lifecycle;
- a positive optimistic version; and
- ordered creation and update timestamps.

`public.architecture_project_cover_images` stores one current cover per project.
Its primary key is also a foreign key to the parent with `ON DELETE CASCADE`.
The row records normalized bytes, decoder-derived type and dimensions, a SHA-256
digest, required alt text, optional caption, a positive cover revision, and
timestamps. The application currently exposes no delete operation.

The partial index
`architecture_projects_published_order_idx` contains only Published rows in
`(sort_order, id)` order. Public portfolio numbers are derived after filtering,
so private records never create visible numbering gaps.

Migration 8 contains no seed records. A new database therefore renders the
truthful Architecture empty state until an administrator creates and publishes
approved content.

The down migration strictly removes the cover child before its parent:

```sql
DROP TABLE public.architecture_project_cover_images;
DROP TABLE public.architecture_projects;
```

It intentionally omits `IF EXISTS` and `CASCADE`, so drift remains visible.
Rollback permanently removes every Architecture project and cover and is safe
only in a confirmed disposable database. Migration 8 is the latest member of
the current contiguous 1-through-8 catalog; never edit it after it has been
applied anywhere.

## Public routes

The public contract is:

```text
GET /architecture-design
GET /architecture-design/{slug}
GET /architecture-design/{slug}/cover/{version}
```

The list and detail queries select only Published projects. Invalid, unknown,
Draft, and Archived slugs all receive the same `404 Not Found` response. A
repository or stored-contract failure becomes a generic
`503 Service Unavailable` and a fixed-value log entry.

The cover route accepts only a canonical slug, canonical positive decimal
revision, exact escaped path, and no query. It rechecks both current revision
and parent publication state before returning bytes. Success responses include:

```text
Cache-Control: public, max-age=0, must-revalidate
ETag: "<sha256>"
X-Content-Type-Options: nosniff
Cross-Origin-Resource-Policy: same-origin
```

Failures start with `Cache-Control: no-store`. Revalidation is intentional: an
Archive action must hide a previously viewed image on the next request.

## Protected routes and authorization

The protected contract is:

```text
GET  /admin/architecture-projects
GET  /admin/architecture-projects/new
POST /admin/architecture-projects
GET  /admin/architecture-projects/{id}
GET  /admin/architecture-projects/{id}/edit
POST /admin/architecture-projects/{id}
GET  /admin/architecture-projects/{id}/cover
POST /admin/architecture-projects/{id}/cover
GET  /admin/architecture-projects/{id}/cover/{version}
```

Every route first requires one active administrator session and then passes
through an explicit Architecture Owner/Editor allowlist. Read and mutation
allowlists are declared separately so a future role does not acquire write
authority merely because it can inspect records.

Protected responses retain the shared `no-store`, Content Security Policy,
referrer, frame, content-type, and robots headers. The cover preview remains
protected for Draft and Archived records so editors can review normalized media
without making it public.

## Forms, CSRF, and concurrency

Create and edit requests accept only exact URL-encoded field sets. Server-side
validation is authoritative even when browser `maxlength`, numeric, and select
controls provide earlier hints. The accepted project values are:

- canonical slug, title, typology, and project status;
- optional location, year, and description;
- positive sort order;
- one closed publication status; and
- the session-bound CSRF token.

Edit and cover forms also carry the server-issued project version. PostgreSQL
updates only when the submitted expected version still matches. A stale edit or
cover form receives a generic `409 Conflict` recovery page instead of silently
overwriting newer work. Successful mutations use `303 See Other` to return to a
canonical GET, so refresh does not repeat the POST.

The version is concurrency control, not visitor-editable project content. A
missing, noncanonical, nonpositive, or overflowing value is malformed request
syntax rather than an ordinary field-validation error.

## Reviewed cover boundary

The upload parser accepts one exact multipart file part plus required alt text,
optional caption, CSRF token, and expected project version. It rejects unknown
or duplicate parts, transfer/content codings, malformed media types, oversized
requests, and noncanonical route coordinates before persistence.

Accepted source images are JPEG or PNG, at most 8 MiB, at most 10,000 pixels on
either axis, and at most 25 megapixels. The server decodes and re-encodes the
image before storage. This strips EXIF, XMP, text chunks, hidden thumbnails, and
color profiles rather than publishing the original upload bytes. JPEG uses the
reviewed quality setting of 90; PNG remains lossless.

Go's JPEG decoder does not apply EXIF orientation. Export or rotate orientation
into the pixels before upload, then inspect the protected preview while the
project is Draft. Replacing a cover on an already Published project makes the
new revision public immediately after the successful transaction.

Alt text is required, trimmed, single-line reviewed copy up to 300 Unicode code
points. Caption is optional, trimmed, single-line reviewed copy up to 500 code
points. Neither value is trusted as HTML; `html/template` performs contextual
escaping.

## Database privileges

Use a migration role for schema changes. A least-privilege runtime role needs
only the columns referenced by the fixed statements plus identity-sequence
usage and the narrow insert/update columns needed by protected writers. It does
not need schema ownership, `DELETE`, `TRUNCATE`, gallery storage, or access to
administrator credential columns.

An operator-managed example is:

```sql
GRANT SELECT (
    id, slug, title, typology, location, project_year,
    project_status, description, sort_order, publication_status,
    version, created_at, updated_at
) ON public.architecture_projects TO chosen_runtime_role;

GRANT INSERT (
    slug, title, typology, location, project_year,
    project_status, description, sort_order, publication_status
) ON public.architecture_projects TO chosen_runtime_role;

GRANT UPDATE (
    slug, title, typology, location, project_year,
    project_status, description, sort_order, publication_status,
    version, updated_at
) ON public.architecture_projects TO chosen_runtime_role;

GRANT SELECT (
    architecture_project_id, version, content_type, content, byte_size,
    width, height, sha256, alt_text, caption, created_at, updated_at
) ON public.architecture_project_cover_images TO chosen_runtime_role;

GRANT INSERT (
    architecture_project_id, version, content_type, content, byte_size,
    width, height, sha256, alt_text, caption
) ON public.architecture_project_cover_images TO chosen_runtime_role;

GRANT UPDATE (
    version, content_type, content, byte_size, width, height,
    sha256, alt_text, caption, updated_at
) ON public.architecture_project_cover_images TO chosen_runtime_role;

GRANT USAGE ON SEQUENCE public.architecture_projects_id_seq
TO chosen_runtime_role;
```

Replace `chosen_runtime_role` with the reviewed deployment role and confirm the
generated sequence name in the target schema. Review the fixed SQL before
granting permissions. Column-level grants deliberately prevent a later schema
change from exposing a new Architecture field automatically. Product and
Interior grants remain separate; do not replace these lists with a broad table
or schema grant merely because the workflows share mechanics.

## Local verification

First place the migration-role URL in the current PowerShell process, then apply
and inspect the complete migration history without printing that URL:

```powershell
go run . migrate status
go run . migrate up
go run . migrate status
```

Versions 1 through 8 should be Applied, and a second `migrate up` should be a
truthful no-op. Replace `DATABASE_URL` with the least-privilege runtime-role URL
before starting the application:

```powershell
go run .
```

Then use fictional records only:

1. Sign in at `http://localhost:8080/admin/login`.
2. Open `http://localhost:8080/admin/architecture-projects`.
3. Create a Draft with a unique fictional slug and confirm revision 1 appears on
   its protected detail. Leave location, year, and description empty once to
   verify that optional data remains genuinely absent.
4. Confirm its public detail and guessed cover URL return 404 and that it is not
   present on the public Architecture list.
5. Open Edit in two tabs. Save tab A, then save tab B. Tab B must receive the
   fixed `409 Conflict` recovery page and must not overwrite tab A.
6. Upload a small fictional JPEG or PNG with orientation baked into its pixels
   and meaningful fictional alt text. Inspect the normalized protected preview,
   keyboard order, focus, and 200% zoom.
7. Open the cover form in two tabs. Upload from tab A, then submit tab B. The
   stale second form must receive `409 Conflict`; it must not replace tab A's
   current cover or project revision.
8. Publish the project and verify the list, detail, and exact current cover URLs.
   Draft and Archived records must remain absent from the same public reads.
9. Replace the cover and verify the old revision is 404 and the new one loads.
   Revalidate the new URL with its ETag and confirm an unchanged request can
   receive `304 Not Modified` without weakening the publication check.
10. Archive the project and verify its public list entry, detail, and cover are
    all unavailable while protected detail and cover preview remain available.
11. Repeat the protected read/write checks with one fictional local Owner and
    one fictional local Editor. Sign out and confirm the login boundary protects
    every administrator route.

At 375, 430, 768, 960, 1024, 1280, and 1920 CSS pixels, verify there is no
horizontal overflow; long reviewed content wraps; meaningful images retain alt
text; fallbacks remain decorative; and the admin forms remain keyboard usable.

Run normal checks:

```powershell
go fmt ./...
go test -count=1 ./...
go vet ./...
git diff --check
```

The opt-in PostgreSQL tests require a disposable database whose name ends in
`_test`, contains none of the reserved project relations, and uses the existing
confirmation phrase. Never point them at a development, shared, staging, or
production database. Use the history-safe helper from
[admin-access.md](admin-access.md), then run:

```powershell
Set-SecretProcessVariable `
    -Name 'ZAFARMAND_TEST_DATABASE_URL' `
    -Prompt 'Enter the disposable PostgreSQL test URL'

$env:ZAFARMAND_TEST_DATABASE_CONFIRM = 'stage13-disposable-database'
go test -count=1 -run 'Postgres' ./...

Remove-Item Env:ZAFARMAND_TEST_DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:ZAFARMAND_TEST_DATABASE_CONFIRM -ErrorAction SilentlyContinue
```

The guard never falls back to `DATABASE_URL`. It verifies the configured and
server-reported `_test` database name, refuses existing reserved relations,
exercises the full 1-through-8 migration lifecycle and real Architecture
reader/writer/cover behavior, and cleans only its exact relations. Once opted
in, an unreachable PostgreSQL server is a test failure rather than a skip.

## Deferred after Stage 23

- Architecture deletion and retention policy;
- multiple-image galleries and drag ordering;
- crop, focal-point, and rendition management;
- client, site area, credits, and richer project facts;
- featured Architecture selection and homepage placement;
- preview-before-publishing links;
- durable audit history and actor attribution;
- SEO metadata; and
- object storage or CDN operations.
