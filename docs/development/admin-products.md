# Stages 19–21 protected Product management

Stage 19 established protected, read-only Product list and detail pages. Stage
20 kept those GET routes read-only and added the first concurrency-aware create
and edit workflow. Stage 21 extends that same reviewed boundary with optional
description, material, and dimensions fields plus one JPEG or PNG cover image.

Stage 21 intentionally stops at one current cover. It does not add a gallery,
cover deletion, crop/focal-point controls, object storage, SEO, featured
placement, bulk actions, or an audit history. Those features need separate data,
interface, retention, and security decisions.

Stages 22 and 23 do not change any Product route, table, repository contract,
form, or grant. They reuse reviewed mechanics for separate Interior-project and
Architecture-project domains without merging the three models. See
[interior-projects.md](interior-projects.md) and
[architecture-projects.md](architecture-projects.md) for those migrations and
workflows; keep using this guide for Product behavior.

## Routes and authorization

The private Product routes are:

```text
GET  /admin/products            list every lifecycle state
GET  /admin/products/new        render an empty create form
POST /admin/products            create one Product
GET  /admin/products/{id}       show one protected Product
GET  /admin/products/{id}/edit  render the current Product revision
POST /admin/products/{id}       save one version-guarded edit
GET  /admin/products/{id}/cover render the current cover form and preview
POST /admin/products/{id}/cover upload or replace one version-guarded cover
GET  /admin/products/{id}/cover/{version} serve one protected cover revision
```

All routes pass through administrator session authentication. Separate
read and write allowlists explicitly name `owner` and `editor`; a future role
does not inherit access merely because it can sign in.

GET requests never mutate data. Successful POST requests use HTTP `303 See
Other` to redirect to `/admin/products/{id}`, so refresh and browser Back/Forward
operate on a GET instead of resubmitting a form.

Canonical routing remains strict:

- IDs are positive base-10 `int64` values with no sign or leading zeroes;
- encoded-equivalent digits, overflow, extra path segments, and alternate path
  spellings return `404 Not Found`;
- queries and a bare trailing `?` are rejected;
- Product text POST bodies must be `application/x-www-form-urlencoded` with no
  content coding;
- the create form contains exactly nine fields including CSRF;
- the edit form contains exactly those fields plus `version`;
- the cover POST is an exact five-part `multipart/form-data` body containing
  CSRF, Product version, alt text, caption, and one image; and
- missing, duplicate, or extra form fields return `400 Bad Request`.

## Migrations 5–6: revision, rich content, and one cover

Migration 4 still owns the Product table. Stage 20 adds migration 5:

```text
migrations/000005_add_product_version.up.sql
migrations/000005_add_product_version.down.sql
```

The up migration adds:

```sql
version bigint NOT NULL DEFAULT 1
```

with the named `products_version_positive` check. Existing rows receive revision
1. Each successful edit increments the value by one. The down migration drops
only this column; migration 4 remains responsible for the table.

Stage 21 adds the next pair:

```text
migrations/000006_add_product_content_and_cover.up.sql
migrations/000006_add_product_content_and_cover.down.sql
```

Migration 6 adds trimmed, bounded `description`, `material`, and `dimensions`
columns with empty defaults, so existing Products gain no invented copy. It also
creates `public.product_cover_images`. `product_id` is both its primary key and
a cascading foreign key, which enforces one current cover per Product. The table
stores bounded decoded dimensions, trusted JPEG/PNG type, exact byte size,
SHA-256 digest, required alt text, optional caption, timestamps, and its own
positive cover revision. Ordinary Product queries join only the small metadata;
binary bytes use separate exact-revision queries.

Do not edit an applied migration. In the current Stage 24 catalog, migration 9
and then migrations 8 and 7 must be reversed first in a confirmed disposable
database; doing so permanently removes global site-content settings and the
separate Architecture and Interior tables. The next
`go run . migrate down --confirm` removes Product's cover table and three
content columns from version 6. Later rollback commands remove version 5's
Product revision and then migration 4's entire Product table and every Product
row. Never use rollback as a content-cleanup operation.

## Separate read and write dependencies

The protected reader remains independent:

```text
List(ctx)
FindByID(ctx, id)
FindCoverByProductID(ctx, id, coverVersion)
```

Its migration-6 Product projection includes version, rich content, and optional
cover metadata in addition to the original catalogue fields. The exact cover
method carries image bytes only for a protected preview. The public reader still
returns Published Products only and independently checks publication status when
serving a cover revision.

The writer has three methods:

```text
Create(ctx, input)                    -> ID and version
Update(ctx, id, expectedVersion, input) -> ID and new version
UpsertCover(ctx, id, expectedVersion, coverInput) -> Product and cover versions
```

The editable input contains exactly:

```text
slug
name
category
sort_order
publication_status
description
material
dimensions
```

IDs, revisions, timestamps, decoded image facts, and digests are server- or
database-owned. The separate cover input receives only decoder-verified bytes,
dimensions, type, digest, alt text, and caption. There is deliberately no delete
method on this interface.

All SQL is fixed and parameterized. Create returns the generated ID and initial
version. Update changes the eight editable values, assigns PostgreSQL
`CURRENT_TIMESTAMP`, and increments `version` only when both ID and expected
version match. Cover upsert increments the same Product version and inserts or
increments the cover revision in one PostgreSQL statement.

## Concurrent-edit behavior

The edit GET renders the row's current positive version in a hidden control.
That value is not authorization evidence; authentication, the role allowlist,
and session-bound CSRF validation still run independently.

If two administrators open revision 4:

1. the first successful save writes revision 5;
2. the second POST still expects revision 4;
3. PostgreSQL changes no row; and
4. the application returns a fixed `409 Conflict` page.

The conflict page does not echo the stale Product form or the newer database
row. It links to a fresh edit GET so the administrator can review revision 5
before trying again. There is no silent last-write-wins overwrite.

An explicit same-value save still increments the revision. That deliberate
choice means another tab holding the previous version becomes stale even when
the stored catalogue values happen to compare equal. Stage 20 does not yet
record a durable change history or actor identity.

## Validation and safe failures

Go validates the same editable boundaries before calling PostgreSQL:

- slug is 1–120 lowercase ASCII letters/digits with single internal hyphens;
- name is trimmed, valid UTF-8, and 1–160 characters;
- category is trimmed, valid UTF-8, and 1–80 characters;
- sort order is a canonical integer from 1 through 2147483647; and
- publication status is exactly `draft`, `published`, or `archived`;
- description is optional, trimmed valid UTF-8 up to 6000 characters; and
- material and dimensions are independently optional, trimmed valid UTF-8 up
  to 500 characters each.

A cover must fully decode as JPEG or PNG, be no larger than 8 MiB, have each
dimension between 1 and 10,000 pixels, and contain at most 25 million pixels.
The browser filename and claimed media type are ignored. Alt text is trimmed,
single-line, and 1–300 characters; caption is optional, single-line, and at most
500 characters. The stored digest, byte count, format, and dimensions are
rechecked before any public or protected binary response.

Before persistence, the server re-encodes decoded pixels. PNG uses Go's default
lossless compression; JPEG uses an explicit quality of 90. This strips EXIF,
XMP, GPS, author, hidden-thumbnail, PNG text, and color-profile metadata instead
of silently publishing the original file. Go's JPEG decoder does not apply EXIF
orientation, so rotate/export orientation into the pixels before upload and
verify the protected preview. Re-encoding can change JPEG bytes and color
rendering; the normalized result, not the selected source file, is the published
asset.

Visible-field errors return `422 Unprocessable Entity`, retain escaped form
values, and identify each field in an accessible error summary. The server does
not trim, case-fold, or otherwise turn an invalid value into a different valid
Product.

A unique `products_slug_unique` violation returns a correctable generic `409`
form error. PostgreSQL diagnostics, SQL, constraint detail, connection data, and
stored Product text never enter the response or logs. Other dependency failures
return a fixed `503 Service Unavailable`; missing update IDs return 404.

The session CSRF token is validated before Product semantics or repository
access. A present empty, malformed, non-canonical, or mismatched value returns
403. A structurally missing or duplicate CSRF field is a malformed form and
returns 400.

## Public visibility

Publishing is an explicit status choice in the Product form. The separate public
SQL still filters to `publication_status = 'published'`, so:

- Draft and Archived Products remain absent from `/products` and public detail;
- a Published save makes the Product eligible for the public catalogue; and
- changing Published to Draft or Archived removes it from public reads without
  deleting the row.

No public handler receives the protected revision or private lifecycle siblings.

## Accessible no-JavaScript interface

The create, edit, and cover pages use native labels, inputs, textarea, select,
file input, hidden CSRF/version controls, and native submit buttons.
Server-rendered field errors use `aria-invalid` and `aria-describedby`. The
lifecycle state and image requirements are written as text rather than
communicated by color alone. Existing cover alt text and caption are restored;
the browser-selected filename is never echoed after an error.

Controls have visible keyboard focus and at least a 44-pixel target. At narrow
widths, actions stack without changing source order. Forced-colors mode retains
control and panel boundaries. JavaScript is not required.

The authenticated shell keeps `Cache-Control: no-store`, a restrictive CSP that
allows styles and images only from the same origin, `no-referrer`, framing
protection, and `X-Robots-Tag` on successes and errors.

## Least-privilege database access

The runtime role needs the protected SELECT columns plus narrow insert/update
authority. An operator-managed grant can use:

```sql
GRANT SELECT (
    id, slug, name, category, sort_order, publication_status,
    description, material, dimensions, version, created_at, updated_at
) ON public.products TO chosen_runtime_role;

GRANT INSERT (
    slug, name, category, sort_order, publication_status,
    description, material, dimensions
) ON public.products TO chosen_runtime_role;

GRANT UPDATE (
    slug, name, category, sort_order, publication_status,
    description, material, dimensions, version, updated_at
) ON public.products TO chosen_runtime_role;

GRANT SELECT ON public.product_cover_images TO chosen_runtime_role;

GRANT INSERT (
    product_id, version, content_type, content, byte_size,
    width, height, sha256, alt_text, caption
) ON public.product_cover_images TO chosen_runtime_role;

GRANT UPDATE (
    version, content_type, content, byte_size, width, height,
    sha256, alt_text, caption, updated_at
) ON public.product_cover_images TO chosen_runtime_role;

GRANT USAGE, SELECT ON SEQUENCE public.products_id_seq
TO chosen_runtime_role;
```

The actual generated identity-sequence name must be confirmed in the target
PostgreSQL schema before deployment. Do not grant DELETE, TRUNCATE, table/schema
ownership, migration-ledger access, or broad database privileges.

Because the update statement reads ID and version and evaluates existing
columns, the runtime role also needs the listed SELECT access. Keep the migration
role separate from the HTTP runtime role.

## Manual verification

Use a local development database and fictional Product values only.

1. With the migration-role URL in the current PowerShell process, run:

   ```powershell
   go run . migrate status
   go run . migrate up
   go run . migrate status
   ```

   Versions 1 through 9 should be applied. Migrations 7 through 9 are the
   separate Interior-project, Architecture-project, and global site-content
   schemas and do not alter Product behavior.

2. Switch `DATABASE_URL` to the runtime role, start `go run .`, sign in, and open
   `http://localhost:8080/admin/products`.

3. Select Create Product. With JavaScript disabled, create a fictional Draft.
   Confirm the response redirects to its canonical detail and revision is 1.

4. While the Product is still Draft, open Manage cover. Export a small fictional JPEG
   or PNG with rotation baked into its pixels and add meaningful alt text.
   Confirm the Product revision increments, inspect the protected preview for
   orientation/color, then replace it and confirm the cover revision changes.
   Draft cover routes must remain unavailable publicly.

5. Open Edit Product, change it to Published, and save. Confirm the public
   `/products/{slug}` route and its current cover revision are available. A later
   successful cover replacement is public immediately while status is Published.

6. Open the same edit page in two tabs. Save tab A, then save tab B. Tab B must
   show the 409 conflict page and must not overwrite tab A. Open the fresh edit
   link before saving again.

7. Archive the Product. Its public detail and cover route must return 404. A
   previously loaded cover response uses ETag revalidation rather than a long
   immutable cache, so the next browser request rechecks publication state.

8. Test keyboard-only use, 200% zoom, Windows forced colors, and widths 375,
   430, 768, 960, 1024, and 1280 pixels. Confirm visible focus, readable errors,
   no horizontal scrollbar, and usable logout.

9. Sign out and confirm direct GET/POST Product-management requests redirect to
   login before a write.

Do not place real drafts, passwords, session/CSRF values, database URLs, or
private administrator information in screenshots, logs, issues, or commits.

## Automated verification

Run:

```powershell
go fmt ./...
go test -count=1 ./...
go vet ./...
```

The tests cover migrations 5–6 catalog/up/down behavior, reader projections,
writer SQL and parameters, rich-text and image validation, digest/decoder
checks, slug conflict redaction, missing rows, stale versions, owner/editor
routes, strict URL-encoded and multipart forms, CSRF ordering, 304 revalidation,
422/409/503 responses, contextual escaping, PRG, and no-mutation GET requests.

The opt-in disposable PostgreSQL suite additionally proves real identity and
version defaults, unique slug handling, publication visibility, revision
increments, stale-write rejection, real cover replacement, hidden-public-state
behavior, and the full v1-to-v9 migration cycle. The separate Interior,
Architecture, and site-content lifecycles are verified without weakening these
Product assertions:

```powershell
go test -count=1 -run 'Postgres' ./...
```

Follow the two-variable `_test` database guard in [database.md](database.md).
The suite never falls back to the development `DATABASE_URL`.

## Deferred after Stage 21

Deferred work includes deletion/retention, cover removal, galleries and ordering,
crop/focal-point controls, object-storage migration, audit history and actor
attribution, preview tokens, designer/price/purchasing fields, per-Product SEO,
search/filtering, bulk actions, and final Zafarmand content. Stage 24 supplies
one fixed cover-backed featured Product selector without broadening the Product
writer.
