# Stage 22 PostgreSQL-backed Interior projects

Stage 22 replaces the temporary Interior Design studies with one complete,
database-backed publishing slice. Public list, detail, and cover routes can read
only Published records. Authenticated owners and editors can review every
lifecycle state, create and edit project content, and upload or replace one
reviewed cover through separate protected dependencies.

This stage deliberately keeps Interior Design independent from Architecture.
Sharing validation mechanics does not require one generic database table or one
generic administration model. Stage 23 now provides the parallel Architecture
workflow through its own tables and repositories without changing this Stage 22
contract; see [architecture-projects.md](architecture-projects.md).

## Delivered boundary

Stage 22 includes:

- an unseeded `public.interior_projects` table;
- an unseeded one-current-cover `public.interior_project_cover_images` table;
- Draft, Published, and Archived lifecycle states;
- deterministic public and protected ordering;
- server-rendered public listing and detail pages;
- revision-specific public and protected cover responses;
- protected all-state list and detail pages;
- protected create and edit forms;
- optimistic stale-edit protection;
- one reviewed JPEG or PNG cover upload/replacement workflow;
- metadata-stripping image normalization; and
- database-independent unit/HTTP tests plus explicitly guarded live PostgreSQL
  integration tests.

It does not include project deletion, cover deletion, galleries, gallery order,
crop/focal-point controls, object storage, featured homepage placement, SEO,
preview tokens, cross-discipline records, or real Zafarmand content. Stage 23
adds Architecture through an independent model; the other omissions still
require separate interface, retention, and operational decisions.

## Public and protected routes

The public routes are real server-rendered URLs:

```text
GET /interior-design
GET /interior-design/{slug}
GET /interior-design/{slug}/cover/{version}
```

The protected management routes are:

```text
GET  /admin/interior-projects
GET  /admin/interior-projects/new
POST /admin/interior-projects
GET  /admin/interior-projects/{id}
GET  /admin/interior-projects/{id}/edit
POST /admin/interior-projects/{id}
GET  /admin/interior-projects/{id}/cover
POST /admin/interior-projects/{id}/cover
GET  /admin/interior-projects/{id}/cover/{version}
```

Every protected route first resolves a valid administrator session and then an
explicit `owner`/`editor` role allowlist. Read and write allowlists are separate
decisions even though both current roles appear in both. A future role receives
no Interior permission automatically.

Successful mutations use `303 See Other` to a canonical protected GET. Refresh,
Back, and Forward therefore do not resubmit a write. GET requests never create,
edit, publish, archive, or replace media.

## Migration 7

Stage 22 adds one immutable migration pair:

```text
migrations/000007_create_interior_projects.up.sql
migrations/000007_create_interior_projects.down.sql
```

The parent table stores:

```text
id
slug
title
typology
location
project_year
project_status
description
sort_order
publication_status
version
created_at
updated_at
```

`project_status` is required reviewed public content such as `Completed` or
`Ongoing`. It is different from `publication_status`, which controls whether the
record is publicly eligible. `location` and `description` use honest empty
defaults for an incomplete Draft. `project_year` is SQL `NULL` when unknown and
otherwise a four-digit integer from 1000 through 9999; the database does not
store zero as a fake year.

The schema applies these boundaries:

- slug: 1–120 lowercase ASCII letters/digits with single internal hyphens;
- title: trimmed, 1–160 Unicode characters;
- typology: trimmed, 1–80 characters;
- location: trimmed and optional, at most 160 characters;
- project year: absent or 1000–9999;
- project status: trimmed, 1–80 characters;
- description: trimmed and optional, at most 6000 characters;
- sort order: positive PostgreSQL `integer`;
- publication status: exactly `draft`, `published`, or `archived`;
- revision: positive PostgreSQL `bigint`; and
- update timestamp: never earlier than creation.

`interior_projects_published_order_idx` contains only Published rows in
`(sort_order, id)` order. The unique slug index supports canonical lookups. The
primary key supports protected positive-ID reads. No additional speculative
index is introduced for the expected small protected catalogue.

The cover table uses `interior_project_id` as both its primary key and a named
foreign key to `interior_projects.id`. This enforces one current cover. The
foreign key uses `ON DELETE CASCADE` so a future deliberate parent removal could
not orphan bytes; Stage 22 exposes no deletion route or repository method.

Each cover stores:

```text
interior_project_id
version
content_type
content
byte_size
width
height
sha256
alt_text
caption
created_at
updated_at
```

The constraints permit only JPEG/PNG, 1 byte through 8 MiB, dimensions from 1
through 10,000 pixels per axis, at most 25 million decoded pixels, an exact
32-byte SHA-256 digest, required trimmed alt text up to 300 characters, and an
optional trimmed caption up to 500 characters.

Migration 7 inserts no project or image rows. A fresh database must show the
truthful public empty state until an administrator publishes reviewed content.

## Repository and visibility boundaries

The public reader owns only:

```text
ListPublished(ctx)
FindPublishedBySlug(ctx, slug)
FindPublishedCover(ctx, slug, coverVersion)
```

Its list query filters to Published before calculating `ROW_NUMBER()` over
`(sort_order, id)`. Its detail predicate is outside the same published window,
so a detail page retains the number shown in the listing. Unknown, Draft, and
Archived slugs share one not-found result and cannot be distinguished publicly.

Ordinary list/detail SQL joins only cover metadata. Image bytes travel through
an exact-revision method. The public cover query independently joins the parent
and rechecks Published state; knowing a former cover URL cannot bypass an
archive action.

The protected reader owns only:

```text
List(ctx)
FindByID(ctx, id)
FindCoverByProjectID(ctx, id, coverVersion)
```

It returns every lifecycle state in `(sort_order, id)` order. Binary bytes are
absent from ordinary HTML queries and appear only on the exact protected preview
path.

The protected writer owns only:

```text
Create(ctx, input)
Update(ctx, id, expectedVersion, input)
UpsertCover(ctx, id, expectedVersion, coverInput)
```

There is no delete method. All SQL text is fixed and all managed values are
positional parameters. Driver diagnostics, SQL, credentials, stored content,
and image bytes never enter public errors or logs.

Application construction requires the public Interior reader plus separate
protected reader and writer. A missing dependency prevents server startup; the
application cannot silently fall back to the removed temporary studies.

## Forms, CSRF, and optimistic concurrency

The project create/edit controls are:

```text
slug
title
typology
location
project_year
project_status
description
sort_order
publication_status
```

Create also submits the authenticated session's CSRF value. Edit submits CSRF
plus the positive project `version` rendered by its GET. The server accepts only
the expected URL-encoded form shape, rejects duplicate/extra controls, validates
CSRF before project semantics, and never treats a hidden version as
authorization evidence.

When two administrators open version 4:

1. the first successful edit writes version 5;
2. the second still submits expected version 4;
3. PostgreSQL updates no row; and
4. the application returns a fixed `409 Conflict` response with a link to load
   the current edit page.

A deliberate same-value save still increments the revision. Cover upload or
replacement advances the parent revision and the cover revision atomically.
This prevents a stale text form and a newer cover form from silently overwriting
one another.

The cover multipart form contains exactly:

```text
csrf_token
version
alt_text
caption
image
```

The request has a global byte limit and each text part has its own small limit.
Missing, duplicate, or extra parts are malformed. The browser filename and
claimed media type are ignored.

Invalid visible project or media content returns escaped, accessible field
errors without changing the database. A duplicate canonical slug becomes a
safe correctable conflict only when PostgreSQL reports the named
`interior_projects_slug_unique` constraint. Other dependency failures remain a
fixed service error.

## Image normalization and delivery

Interior covers reuse the reviewed image mechanics already proven for Products:

1. bound the complete request and file bytes;
2. decode configuration using Go's standard library;
3. accept only JPEG or PNG;
4. enforce axis and total-pixel limits before allocating a decoded image;
5. fully decode the image;
6. re-encode only decoded pixels; and
7. compute size, dimensions, type, and SHA-256 from the normalized result.

PNG uses Go's default lossless compression. JPEG uses explicit quality 90.
Re-encoding strips EXIF, XMP, GPS, author, hidden-thumbnail, PNG text, and color
profile metadata rather than storing the selected source file. Go's JPEG decoder
does not apply EXIF orientation, so rotation must be baked into the pixels before
upload. Always review a fictional/local Draft through the protected preview
before publishing.

A successful public cover response supplies a strong SHA-256 ETag and:

```text
Cache-Control: public, max-age=0, must-revalidate
X-Content-Type-Options: nosniff
Cross-Origin-Resource-Policy: same-origin
```

Revalidation avoids retransmitting unchanged bytes but still makes the next
request recheck publication after Draft/Archive changes. Error responses use
`no-store`. A long immutable cache would violate that publication boundary.

Protected HTML and preview responses remain `no-store`, same-origin, no-referrer,
framing-protected, and excluded from indexing by the established admin shell.
Native labels, controls, summaries, focus styles, and server-rendered errors keep
the workflow usable without JavaScript, with keyboard navigation, zoom, and
forced colors.

## Least-privilege runtime role

Keep the schema-owning migration URL separate from the long-running server URL.
Migration-role credentials apply migration 7. The runtime role needs only the
columns used by fixed application statements. An operator-managed example is:

```sql
GRANT SELECT (
    id, slug, title, typology, location, project_year,
    project_status, description, sort_order, publication_status,
    version, created_at, updated_at
) ON public.interior_projects TO chosen_runtime_role;

GRANT INSERT (
    slug, title, typology, location, project_year,
    project_status, description, sort_order, publication_status
) ON public.interior_projects TO chosen_runtime_role;

GRANT UPDATE (
    slug, title, typology, location, project_year,
    project_status, description, sort_order, publication_status,
    version, updated_at
) ON public.interior_projects TO chosen_runtime_role;

GRANT SELECT (
    interior_project_id, version, content_type, content, byte_size,
    width, height, sha256, alt_text, caption, created_at, updated_at
) ON public.interior_project_cover_images TO chosen_runtime_role;

GRANT INSERT (
    interior_project_id, version, content_type, content, byte_size,
    width, height, sha256, alt_text, caption
) ON public.interior_project_cover_images TO chosen_runtime_role;

GRANT UPDATE (
    version, content_type, content, byte_size, width, height,
    sha256, alt_text, caption, updated_at
) ON public.interior_project_cover_images TO chosen_runtime_role;

GRANT USAGE ON SEQUENCE public.interior_projects_id_seq
TO chosen_runtime_role;
```

Confirm the generated sequence name in the target schema. Do not grant DELETE,
TRUNCATE, schema ownership, migration-ledger access, access to Architecture
tables merely because this Interior workflow needs similar columns, or a broad
table/schema privilege merely to avoid listing required columns. Stage 23's
separate grants are documented in
[architecture-projects.md](architecture-projects.md).

## Manual verification

Use only a local database and fictional content. Never paste a database URL,
administrator password, session/CSRF value, private draft, or real client data
into terminal output, screenshots, issues, or commits.

1. In one PowerShell process, set `DATABASE_URL` to the migration role using the
   secret helper documented in [admin-access.md](admin-access.md), then run:

   ```powershell
   go run . migrate status
   go run . migrate up
   go run . migrate status
   ```

   Versions 1 through 8 should be Applied. Migration 8 owns the separate
   Architecture schema and does not alter these Interior checks. A second
   `migrate up` should be a truthful no-op.

2. Switch `DATABASE_URL` to the least-privilege runtime role in that same shell
   and start the server:

   ```powershell
   go run .
   ```

3. Before creating content, open `http://localhost:8080/interior-design`. Confirm
   the public page shows its honest empty state rather than the former four
   temporary studies.

4. Sign in, open `http://localhost:8080/admin/interior-projects`, and create a
   fictional Draft. Leave optional location, year, and description blank once;
   confirm the protected detail renders without inventing those facts.

5. Open the edit page in two tabs. Save tab A, then save tab B. Tab B must return
   the stale `409 Conflict` response and must not overwrite tab A.

6. While the record remains Draft, upload a small fictional JPEG or PNG with
   meaningful alt text. Confirm the project revision increments, the protected
   preview displays the normalized orientation/color, and its public detail and
   cover routes remain 404.

7. Publish the project. Confirm it appears at `/interior-design`, its detail
   number matches its list number, and its exact current cover is available.
   Replace the cover and confirm the old revision URL becomes 404 while the new
   revision is served.

8. Archive the project. Confirm public detail and cover return 404 while the
   authenticated protected detail and current cover preview remain available.

9. Test keyboard-only operation, JavaScript disabled, 200% zoom, Windows forced
   colors, and widths 375, 430, 768, 960, 1024, and 1280 pixels. Confirm visible
   focus, associated error messages, readable cover text, and no horizontal
   scrollbar.

10. Sign out and request each protected GET and POST directly. Authentication
    must run before repository access or mutation.

## Guarded live PostgreSQL tests

Ordinary tests never consult `DATABASE_URL`. Live repository and migration
tests require a separately confirmed disposable database whose name ends in
`_test`:

```powershell
Set-SecretProcessVariable `
    -Name 'ZAFARMAND_TEST_DATABASE_URL' `
    -Prompt 'Enter the disposable PostgreSQL test URL'

$env:ZAFARMAND_TEST_DATABASE_CONFIRM = 'stage13-disposable-database'

go test -count=1 -run Postgres ./...

Remove-Item Env:ZAFARMAND_TEST_DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:ZAFARMAND_TEST_DATABASE_CONFIRM -ErrorAction SilentlyContinue
```

The guarded suite refuses an unconfirmed database or a name without `_test`,
requires every reserved Stage 13–23 relation to be absent before starting, and
cleans only those exact relations afterward. It proves migration 7 defaults and
named constraints, published-only numbering/visibility, protected all-state
reads, create/edit and stale revision behavior, cover replacement, archive
hiding, one-cover ownership, foreign-key cascade, rollback isolation,
reapplication, and transaction atomicity. The complete v1-to-v8 cycle also
checks the independent Architecture relations without weakening these Interior
assertions.

## Rollback and reapplication

`go run . migrate down --confirm` is destructive and belongs only on a confirmed
disposable database. At schema version 7 it runs the strict child-first reverse:

```sql
DROP TABLE public.interior_project_cover_images;
DROP TABLE public.interior_projects;
```

All Interior rows and covers are permanently removed. Product, inquiry, and
administrator tables remain at migration 6. Reapplying migration 7 creates empty
Interior tables; it cannot restore removed content. Never use rollback as a
cleanup strategy for production or shared development data.

After Stage 23 is applied, migration 7 is no longer the latest version; the
Architecture migration must be reversed first in a disposable environment.
Never edit or rename migration 7 after it has been applied anywhere, because
the migration ledger will correctly report checksum drift.

## Automated verification

Run before a focused commit:

```powershell
go fmt ./...
go test -count=1 ./...
go vet ./...
git diff --check
git check-attr eol -- migrations/000007_create_interior_projects.up.sql
git check-attr eol -- migrations/000007_create_interior_projects.down.sql
```

The two migration files must report `eol: lf`. Review `git status` and `git diff`
before staging only the intended Stage 22 files.

## Deferred after Stage 22

Stage 23 reuses the proven mechanics for Architecture without silently
broadening Interior behavior. Still deferred for Interior are:

- Interior deletion and retention confirmation;
- multiple images or gallery ordering;
- cover removal, crop, focal point, and object storage;
- featured homepage selection;
- SEO title/description controls;
- durable audit history and actor attribution;
- public preview tokens for Draft records; and
- final approved Zafarmand content and photography.
