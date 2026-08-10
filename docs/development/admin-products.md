# Stage 19 protected Product review

Stage 19 adds the first protected Product-management view. It is deliberately
read-only: authenticated Owners and Editors can inspect every Product row and
open one canonical detail page, but no HTTP route can create, edit, publish,
archive, reorder, or delete a Product.

This order matters for a learning project. The stage first establishes:

- which stored fields administrators need to understand;
- how Draft, Published, and Archived differ from public visibility;
- the authorization boundary around unpublished catalogue information;
- the read repository and failure contract; and
- the responsive, keyboard-usable interface that later mutation forms will
  extend.

Mutation validation, CSRF behavior, concurrent-edit handling, and publishing
confirmation can then be designed explicitly in a later stage instead of being
hidden inside this read slice.

## Routes and authorization

Stage 19 registers two server-rendered routes:

```text
GET /admin/products       list every Product lifecycle state
GET /admin/products/{id}  show one protected Product record
```

Both routes pass through the existing session middleware and a Product-reader
allowlist that names `owner` and `editor` independently from the inquiry
allowlists. A future role receives no access merely because it can authenticate.

The route contract is intentionally strict:

- list and detail accept GET or its automatic HEAD equivalent only;
- the list accepts no query string, including a bare trailing `?`;
- detail accepts one canonical positive base-10 `int64` identity;
- zero, a sign, leading zeroes, non-decimal text, overflow, encoded-equivalent
  digits, an extra segment, or any query returns an error before a Product read;
- anonymous requests redirect to `/admin/login` before repository access; and
- a valid missing identity returns the ordinary protected `404 Not Found`.

Canonical IDs are suitable for this private route because administrators need
a stable internal reference even when a Draft slug has never been public. The
browser URL never contains a Product name, category, or publication status.

## No migration 5

Stage 19 reuses `public.products` from migration 4. That table already owns the
complete data needed by the interface:

```text
id
slug
name
category
sort_order
publication_status
created_at
updated_at
```

The existing primary key supports exact detail lookup, and the current small
catalogue can be listed in `(sort_order, id)` order. This stage adds no filter,
count, pagination, mutation, or new query pattern that justifies another index.
Adding an empty schema version merely to match a stage number would confuse
application milestones with database history.

Migration 4 remains schema-only. A newly migrated database therefore renders a
truthful empty protected catalogue as well as an empty public catalogue.

## Narrow protected repository

The application depends on a separate interface:

```text
List(ctx)          return every Product in (sort_order, id) order
FindByID(ctx, id)  return one Product or a safe not-found category
```

This interface is separate from the public `productCatalogueReader`:

| Boundary | Visible lifecycle states | Lookup | Result fields |
| --- | --- | --- | --- |
| Public Product reader | Published only | Public slug | Public catalogue projection |
| Protected Product reader | Draft, Published, Archived | Positive internal ID | All migration-4 fields |

The separation makes the privacy rule structural. A public handler cannot ask
its dependency for a Draft record, and an admin template does not need to infer
whether a public-only result has hidden siblings.

The list SQL is fixed and parameter-free:

```sql
SELECT
    id,
    slug,
    name,
    category,
    sort_order,
    publication_status,
    created_at,
    updated_at
FROM public.products
ORDER BY sort_order ASC, id ASC;
```

Detail uses the same projection with `WHERE id = $1`. The positive identity is
bound as a parameter; no URL or stored value is interpolated into SQL.

Both methods receive the request context under the existing five-second admin
repository deadline. The process-owned `*sql.DB` pool remains open until the
server stops; row iterators are closed by the method that acquires them.

## Defensive result validation

Database constraints remain the first durable boundary, but Go independently
rechecks substituted-reader and decoding results before templates receive them:

- ID and sort order are positive;
- sort order fits PostgreSQL `integer`;
- slug matches the canonical lowercase-hyphen grammar;
- name and category are valid, trimmed, bounded UTF-8 text;
- publication status is exactly `draft`, `published`, or `archived`;
- timestamps are nonzero and `updated_at` does not predate `created_at`;
- list order is strictly increasing by `(sort_order, id)`; and
- identities and slugs do not repeat.

A malformed stored or substituted result is a dependency-contract failure. The
handler returns a generic `503 Service Unavailable`; it does not render a
partially trusted catalogue.

Driver errors, SQL text, connection details, IDs, slugs, and stored Product text
are also absent from application logs. Logs contain only a fixed operation
category. A genuine no-row detail remains a separate generic 404.

## Presentation and public visibility

The protected list displays:

- administrative reference;
- name;
- canonical stored slug;
- category;
- sort order;
- visible lifecycle status; and
- last stored update time in UTC.

The detail adds the creation time and a plain-language visibility explanation.
Only a Published detail receives an `Open published Product` link. A Draft or
Archived row still shows its stored slug for review, but the template receives
no public path for it. This prevents protected state from becoming a public
discovery link by accident.

The document title remains the generic `Product detail | Zafarmand Admin`; it
does not place a possibly confidential Draft name in browser history. Stored
text remains ordinary Go strings and is escaped by `html/template`; no
`template.HTML` conversion is used.

The shared admin shell now includes Overview, Products, and Inquiries. The
Products item receives both `aria-current="page"` and a visible underline on
list and detail pages. The same authenticated header retains identity and the
POST-only logout form.

The interface uses semantic ordered lists, articles, headings, definition
lists, and `time` elements. Links have visible focus, status is always written
as text rather than conveyed by color alone, narrow layouts collapse facts into
one column, and forced-colors mode preserves borders. No JavaScript is required.

## Least-privilege database access

Stage 19 adds no write permission. The runtime Product reader now needs SELECT
on all migration-4 columns because protected pages display timestamps and
publication state:

```sql
GRANT SELECT (
    id,
    slug,
    name,
    category,
    sort_order,
    publication_status,
    created_at,
    updated_at
) ON TABLE public.products TO chosen_runtime_role;
```

Replace `chosen_runtime_role` in deployment automation with the actual role.
Do not grant Product `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, sequence usage,
schema ownership, or migration-ledger access for this stage.

## Step-by-step manual verification

Use a dedicated local development database and a fictional administrator. Do
not use unpublished real Zafarmand content merely to test the interface.

1. Apply migrations through version 4 with the schema-owner URL as described in
   [database.md](database.md):

   ```powershell
   go run . migrate status
   go run . migrate up
   go run . migrate status
   ```

2. If the database has no administrator, follow the history-safe bootstrap
   steps in [admin-access.md](admin-access.md). Use a fictional local email and
   a test-only password stored outside Git.

3. Switch the current process `DATABASE_URL` to the least-privilege runtime URL
   and start the server:

   ```powershell
   go run .
   ```

4. Open `http://localhost:8080/admin`, sign in, and select Products. A fresh
   database should show `No Products to display` without an error.

5. Optionally insert the five fictional Stage 18 rows from
   [products.md](products.md) using the local schema owner, then restart with the
   runtime role. Confirm the protected list shows all five rows, including Draft
   and Archived, in sort-order/ID order. The public `/products` route must still
   show only the three Published rows.

6. Open each protected detail. Confirm:

   - the Products navigation item remains active;
   - Draft says it is not publicly visible and has no public Product link;
   - Published has one canonical `/products/{slug}` link;
   - Archived says it remains stored but hidden and has no public Product link;
   - refreshing performs no database mutation; and
   - Back and Forward use normal server-rendered history.

7. Verify at 375, 430, 768, 960, 1024, and 1280 CSS pixels, at 200% zoom, with
   keyboard-only navigation, and with JavaScript disabled. There should be no
   horizontal scrollbar, overlap, lost focus indicator, or inaccessible logout.

8. Sign out. Direct requests to both protected Product routes must redirect to
   `/admin/login`. Clean up only the exact fictional slugs by following the
   bounded delete and environment-variable cleanup in [products.md](products.md).

Do not copy session cookies, CSRF values, passwords, database URLs, or real
Product drafts into screenshots, terminal output, issue trackers, or commits.

## Automated verification

The normal database-independent checks are:

```powershell
go fmt ./...
go test -count=1 ./...
go vet ./...
```

Stage 19 tests cover:

- constructor rejection of a missing protected reader;
- exact list/detail SQL, context forwarding, mapping, order, and row cleanup;
- all three lifecycle states and malformed-result rejection;
- driver-error redaction and distinct not-found behavior;
- anonymous redirect and explicit Owner/Editor access;
- method, canonical path, query, and positive-ID contracts;
- truthful empty state and contextual template escaping;
- status text, active navigation, shared logout, timestamps, and public-link
  separation; and
- generic 404 and 503 responses with the existing private security headers.

The guarded PostgreSQL suite additionally proves real migration-4 empty state,
all-state ordering, equal-sort ID tie-breaking, field/timestamp mapping, exact-ID
details, and missing-row behavior:

```powershell
go test -count=1 -run 'Postgres' ./...
```

Use only the two-variable disposable `_test` database opt-in documented in
[database.md](database.md). The live suite never falls back to `DATABASE_URL`.

## Deferred after Stage 19

The next focused Product stage may design protected create/edit/publish/archive
behavior, strict form parsing, CSRF, validation, Post/Redirect/Get, and explicit
concurrent-edit handling. It must not be inferred from this reader.

Also deferred are deletion/retention, bulk actions, preview tokens, audit
history, media and uploads, descriptions and richer metadata, SEO management,
homepage featuring, search/filtering, and finalized Zafarmand content.
