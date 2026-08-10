# Stages 19–20 protected Product management

Stage 19 established protected, read-only Product list and detail pages. Stage
20 keeps those GET routes read-only and adds the first bounded Product mutation
workflow: authenticated Owners and Editors can create a Product or edit its five
existing catalogue fields.

The stage intentionally does not add deletion, media, descriptions, SEO,
featured placement, preview links, bulk actions, or an audit history. Those
features need separate data and security decisions.

## Routes and authorization

The private Product routes are:

```text
GET  /admin/products            list every lifecycle state
GET  /admin/products/new        render an empty create form
POST /admin/products            create one Product
GET  /admin/products/{id}       show one protected Product
GET  /admin/products/{id}/edit  render the current Product revision
POST /admin/products/{id}       save one version-guarded edit
```

All six routes pass through administrator session authentication. Separate
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
- Product POST bodies must be `application/x-www-form-urlencoded` with no
  content coding;
- the create form contains exactly six fields including CSRF;
- the edit form contains exactly those fields plus `version`; and
- missing, duplicate, or extra form fields return `400 Bad Request`.

## Migration 5: Product revision

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

Do not edit migration 4 after it has been applied. On a complete schema,
`go run . migrate down --confirm` first removes only version 5's revision
column. A second rollback removes migration 4's entire Product table and all its
rows. Use rollback only in a confirmed disposable database.

## Separate read and write dependencies

The protected reader remains independent:

```text
List(ctx)
FindByID(ctx, id)
```

Its migration-5 projection now includes `version` in addition to ID, slug, name,
category, sort order, publication state, and timestamps. The public reader is
unchanged and still returns Published rows only.

The writer has only two methods:

```text
Create(ctx, input)                    -> ID and version
Update(ctx, id, expectedVersion, input) -> ID and new version
```

The editable input contains exactly:

```text
slug
name
category
sort_order
publication_status
```

IDs, revisions, and timestamps are database-owned. There is deliberately no
delete method on this interface.

All SQL is fixed and parameterized. Create returns the generated ID and initial
version. Update changes the five editable values, assigns PostgreSQL
`CURRENT_TIMESTAMP`, and increments `version` only when both ID and expected
version match.

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
- publication status is exactly `draft`, `published`, or `archived`.

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

The create and edit pages use native labels, inputs, a select, hidden CSRF, and
a native submit button. Server-rendered field errors use `aria-invalid` and
`aria-describedby`. The lifecycle state is always written as text rather than
communicated by color alone.

Controls have visible keyboard focus and at least a 44-pixel target. At narrow
widths, actions stack without changing source order. Forced-colors mode retains
control and panel boundaries. JavaScript is not required.

The authenticated shell keeps `Cache-Control: no-store`, a restrictive CSP,
`no-referrer`, framing protection, and `X-Robots-Tag` on successes and errors.

## Least-privilege database access

The runtime role needs the protected SELECT columns plus narrow insert/update
authority. An operator-managed grant can use:

```sql
GRANT SELECT (
    id, slug, name, category, sort_order, publication_status,
    version, created_at, updated_at
) ON public.products TO chosen_runtime_role;

GRANT INSERT (
    slug, name, category, sort_order, publication_status
) ON public.products TO chosen_runtime_role;

GRANT UPDATE (
    slug, name, category, sort_order, publication_status,
    version, updated_at
) ON public.products TO chosen_runtime_role;

GRANT USAGE, SELECT ON SEQUENCE public.products_id_seq
TO chosen_runtime_role;
```

The actual generated identity-sequence name must be confirmed in the target
PostgreSQL schema before deployment. Do not grant DELETE, TRUNCATE, table/schema
ownership, migration-ledger access, or broad database privileges for Stage 20.

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

   Versions 1 through 5 should be applied.

2. Switch `DATABASE_URL` to the runtime role, start `go run .`, sign in, and open
   `http://localhost:8080/admin/products`.

3. Select Create Product. With JavaScript disabled, create a fictional Draft.
   Confirm the response redirects to its canonical detail and revision is 1.

4. Open Edit Product, change it to Published, and save. Confirm revision becomes
   2 and the public `/products/{slug}` route is available.

5. Open the same edit page in two tabs. Save tab A, then save tab B. Tab B must
   show the 409 conflict page and must not overwrite tab A. Open the fresh edit
   link before saving again.

6. Test keyboard-only use, 200% zoom, Windows forced colors, and widths 375,
   430, 768, 960, 1024, and 1280 pixels. Confirm visible focus, readable errors,
   no horizontal scrollbar, and usable logout.

7. Sign out and confirm direct GET/POST Product-management requests redirect to
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

The tests cover migration 5 catalog/up/down behavior, reader version mapping,
writer SQL and parameters, field validation, slug conflict redaction, missing
rows, stale versions, owner/editor routes, strict form and CSRF boundaries,
422/409/503 responses, contextual escaping, PRG, and no-mutation GET requests.

The opt-in disposable PostgreSQL suite additionally proves real identity and
version defaults, unique slug handling, publication visibility, revision
increments, stale-write rejection, and the full v1-to-v5 migration cycle:

```powershell
go test -count=1 -run 'Postgres' ./...
```

Follow the two-variable `_test` database guard in [database.md](database.md).
The suite never falls back to the development `DATABASE_URL`.

## Deferred after Stage 20

Deferred work includes deletion/retention, audit history and actor attribution,
preview tokens, rich descriptions/materials/dimensions, media uploads and image
metadata, featured placement, SEO, search/filtering, bulk actions, and final
Zafarmand content. Stage 21 can design the rich Product content/media slice on
top of this completed concurrency-aware mutation boundary.
