# Stage 18 database-backed public Products

Stage 18 replaces the temporary in-memory Product catalogue with the first
PostgreSQL-backed public content reader. It is intentionally one vertical,
read-only slice:

- migration 4 creates minimal durable identity, public text, ordering,
  publication-state, and timestamp storage;
- the public repository reads published products only;
- `GET /products` renders those rows in deterministic catalogue order;
- `GET /products/{slug}` renders one published row at the same catalogue
  number shown by the list; and
- a fresh database truthfully renders an empty catalogue because the migration
  inserts no sample business content.

Stages 19–20 add protected all-state review and a concurrency-aware Product
create/edit/publication workflow without changing this published-only public
contract. Their routes, authorization, validation, and verification steps are
documented in [admin-products.md](admin-products.md). Media and final Zafarmand
content remain future reviewed stages.

## Migration 4 schema

The paired migration is:

```text
migrations/000004_create_products.up.sql
migrations/000004_create_products.down.sql
```

The up migration creates `public.products` with these columns:

| Column | PostgreSQL type | Stage 18 responsibility |
| --- | --- | --- |
| `id` | `bigint GENERATED ALWAYS AS IDENTITY` | Internal primary key and deterministic ordering tie-breaker. |
| `slug` | `text` | Stable public route segment. |
| `name` | `text` | Public catalogue and detail heading. |
| `category` | `text` | Public product-family label. |
| `sort_order` | `integer` | Positive editorial position used before `id`. |
| `publication_status` | `text` | Closed `draft`, `published`, or `archived` state; defaults to `draft`. |
| `created_at` | `timestamptz` | Database creation time. |
| `updated_at` | `timestamptz` | Stored update time that cannot predate creation. Later writers must change it explicitly. |

Migration 5 later adds the positive `version bigint` used only by protected
optimistic editing. The public reader does not select or expose that revision;
see [admin-products.md](admin-products.md).

Named constraints protect stored rows. Go independently validates the smaller,
SQL-derived public projection before handlers receive it:

- slugs contain 1 through 120 lowercase ASCII letters or digits, with single
  hyphens only between nonempty components;
- every slug is unique;
- names are trimmed and contain 1 through 160 characters;
- categories are trimmed and contain 1 through 80 characters;
- `sort_order` is greater than zero;
- publication status is exactly `draft`, `published`, or `archived`; and
- `updated_at` is not earlier than `created_at`.

`products_published_order_idx` is a partial B-tree index over
`(sort_order, id)` for rows whose status is `published`. It is a compact
candidate access path for the public catalogue ordering while keeping draft and
archived entries out of that index. Correctness depends on explicit filtering
and ordering, not on which plan PostgreSQL chooses. The index does not duplicate
product names or categories.

The down migration contains one strict statement:

```sql
DROP TABLE public.products;
```

It intentionally omits `IF EXISTS` and `CASCADE`, so unexpected schema drift or
dependencies fail visibly. Rolling migration 4 down destroys every Product row
and its index. Run that operation only against a confirmed disposable database,
never as a content-cleanup command.

## No seed data

Schema migrations define storage; they do not publish portfolio content.
Migration 4 therefore contains no `INSERT`, backfill, sample Product, or copied
Stage 6 placeholder. Immediately after a fresh migration:

```sql
SELECT COUNT(*) FROM public.products;
```

must return zero. This is a feature, not a migration failure:

- `/products` returns `200 OK` and displays the existing honest empty message;
- any otherwise valid `/products/{slug}` has no matching published record and
  returns the ordinary `404 Not Found`; and
- no fictional item appears merely to make the page look populated.

Real content must wait for a reviewed Product management and publication
workflow. Automated and manual Stage 18 checks use unmistakably synthetic rows
in disposable or local development databases only.

## Narrow public repository

The handlers depend on a read-only `productCatalogueReader` rather than on
`*sql.DB`:

```text
ListPublished(ctx)                 return all published catalogue rows
FindPublishedBySlug(ctx, slug)     return one published row or not found
```

The repository result contains only:

```text
ID
CatalogueNumber
Slug
Name
Category
```

`sort_order` and `publication_status` never cross that boundary. They are used
inside parameterized SQL to decide ordering and public eligibility, not
rendered as database-controlled interface labels. The visible status text is a
trusted Go label because every returned row has already passed the published
filter.

Both list and detail queries bind `published` as a positional parameter. SQL is
fixed; neither a URL slug nor stored text is interpolated into a statement.
Draft and archived records do not enter the public numbering window.

### List ordering and numbering

The published list calculates:

```sql
ROW_NUMBER() OVER (ORDER BY sort_order ASC, id ASC)
```

after filtering to published rows. An outer `ORDER BY catalogue_number ASC`
makes iterator order explicit. The result has consecutive one-based numbers,
even when lower-sorted draft or archived rows exist. Go formats those numbers
with a minimum width of two digits for the current editorial interface; honest
catalogues above 99 are not truncated.

Stage 18 deliberately returns every published Product and introduces no public
pagination, caller-selected limit, sentinel row, or count query. That matches
the current small visual catalogue contract. Before the catalogue can grow
large enough to make an all-row read inappropriate, a later stage must design
pagination together with its visual and editorial requirements rather than
quietly changing ordering now.

### Detail numbering

The detail query first constructs the complete published window and applies
the slug predicate outside it. Filtering by slug inside the window would give
every detail row catalogue number 1, which would disagree with the list.

A canonical missing slug, a draft slug, and an archived slug all produce the
same repository not-found category and public 404 response. The public route
cannot be used to discover which non-public state a stored slug has.

## Validation and safe failures

The Go repository independently validates stored rows before handlers or
templates receive them:

- IDs and catalogue numbers are positive;
- list numbers are exactly `1, 2, 3, ...` in iterator order;
- list IDs and slugs are not duplicated;
- slugs match the same canonical lowercase grammar and bound as PostgreSQL;
- names and categories are valid, nonempty, trimmed UTF-8 within their schema
  limits; and
- a detail row's returned slug exactly matches the requested slug.

The list closes its row iterator on success and on every failure path. Nil
contexts, malformed slugs, missing dependencies, driver errors, scan failures,
iteration failures, close failures, and malformed stored rows are separated
into safe application categories. Lower-level diagnostics are not wrapped
because they can contain SQL, connection information, or rejected stored data.

Each public read receives a five-second request-derived timeout. Handler
behavior is deliberately small:

- a canonical published list or detail renders normally;
- the empty list renders the truthful empty state;
- a non-canonical, missing, draft, or archived detail slug returns ordinary
  `404 Not Found`; and
- a dependency, PostgreSQL, scan, or data-contract failure returns generic
  `503 Service Unavailable`.

Logs identify only the failed catalogue operation. They must not include a
slug, name, category, SQL statement, bound value, connection string, or driver
diagnostic. Templates continue to use ordinary strings, so `html/template`
performs contextual escaping. Product database values never become template
names, raw HTML, CSS classes, or unvalidated URLs.

## Application composition

The long-running server owns one shared PostgreSQL pool. At startup it creates
the Product reader alongside the existing Contact, inquiry-administration, and
authentication repositories, then injects each narrow interface into the
application. Handlers borrow those dependencies; they never open or close a
database pool.

Application construction rejects a nil Product reader. The request handlers
also fail safely if a manually assembled application bypasses normal
construction. Repository construction itself performs no query, so operators
must apply the current migrations through version 5 before serving traffic. If
the table or revision is absent, Product requests fail generically rather than
falling back to temporary records or disabling concurrency checks.

The existing template presentation boundary remains useful:

- repository records map to the smaller list/detail view models;
- catalogue paths are constructed from validated slugs in Go;
- published ordering determines the displayed number;
- the listing retains semantic ordered-list and native-link behavior;
- the detail retains one real server-rendered URL and its native back link; and
- media and descriptive areas remain honest structural placeholders.

No JavaScript is required for list or detail navigation. Browser Back,
Forward, copied URLs, new tabs, and HEAD behavior continue to use real routes.

## Runtime database permissions

Migration commands still use a schema-owner connection. The long-running
server should use a separate least-privilege role. Stage 18 adds only
column-level `SELECT` requirements on `public.products`:

```text
id
slug
name
category
sort_order
publication_status
```

The Stage 18 public reader does not need Product writes and does not select
timestamps or revision data. Stages 19–20 expand the shared application runtime
for protected reads and narrow create/edit authority; their least-privilege
grant is documented in [admin-products.md](admin-products.md). Define grants in
deployment automation rather than hard-coding a role name in application code.

An illustrative grant for a role chosen by the operator is:

```sql
GRANT SELECT (
    id,
    slug,
    name,
    category,
    sort_order,
    publication_status,
    version,
    created_at,
    updated_at
) ON TABLE public.products TO chosen_runtime_role;
```

Replace `chosen_runtime_role` with the real quoted or unquoted identifier used
by the deployment. Do not paste a database password or connection URL into SQL,
source code, documentation transcripts, screenshots, or commits.

## Manual verification

Use only a local development database with disposable synthetic Product rows.
Do not enter genuine Zafarmand content before a reviewed management and
publication workflow exists.

1. In one PowerShell process, set `DATABASE_URL` to the migration-role URL by
   following [database.md](database.md). Do not print the value.
2. Inspect and apply the current catalogue:

   ```powershell
   go run . migrate status
   go run . migrate up
   go run . migrate status
   ```

   Versions 1, 2, 3, and 4 should be applied.
3. Let `psql` read the same URL from `PGDATABASE`, verify the database identity,
   and inspect schema rather than content:

   ```powershell
   $env:PGDATABASE = $env:DATABASE_URL
   psql -X --set ON_ERROR_STOP=on --command '\conninfo'
   psql -X --set ON_ERROR_STOP=on --command '\d public.products'
   psql -X --set ON_ERROR_STOP=on --command '\di public.products_published_order_idx'
   psql -X --set ON_ERROR_STOP=on --command 'SELECT COUNT(*) FROM public.products;'
   ```

   A fresh database must report zero Product rows.
4. Switch `DATABASE_URL` to the least-privilege runtime URL and start the server:

   ```powershell
   go run .
   ```

5. Open `http://localhost:8080/products`. Confirm the route returns 200, keeps
   the Products navigation active, and displays the empty catalogue message.
   A made-up canonical detail such as `/products/stage18-missing-product` must
   return 404.

### Optional fictional browser data

To exercise populated list and detail pages manually, stop the server, switch
back to the local schema-owner connection, and insert only these unmistakably
synthetic rows:

```sql
INSERT INTO public.products (
    slug,
    name,
    category,
    sort_order,
    publication_status
)
VALUES
    ('stage18-test-chair', 'Stage 18 Test Chair', 'Test Furniture', 30, 'published'),
    ('stage18-test-draft', 'Stage 18 Test Draft', 'Test Lighting', 2, 'draft'),
    ('stage18-test-lamp', 'Stage 18 Test Lamp', 'Test Lighting', 10, 'published'),
    ('stage18-test-archive', 'Stage 18 Test Archive', 'Test Objects', 1, 'archived'),
    ('stage18-test-vessel', 'Stage 18 Test Vessel', 'Test Objects', 10, 'published');
```

Restart with the runtime role and verify:

1. `/products` shows only Test Lamp, Test Vessel, and Test Chair.
2. Their visible catalogue numbers are 01, 02, and 03. The equal sort order for
   Lamp and Vessel is resolved by their generated IDs.
3. `/products/stage18-test-lamp` shows number 01, matching the list.
4. `/products/stage18-test-draft` and
   `/products/stage18-test-archive` both return 404.
5. No public page reveals raw `sort_order`, the stored lowercase
   `publication_status`, timestamps, or internal ID. The visible `Published`
   label is trusted Go interface copy derived from the public-only query.
6. At 375, 430, 768, 960, 1024, and 1280 CSS pixels, the list/detail pages have
   no horizontal scrollbar or overlapping content.
7. With JavaScript disabled and keyboard-only navigation, catalogue links, the
   detail back link, skip link, shared navigation, browser Back, and browser
   Forward remain usable.

When finished, stop the server, reconnect with the local schema owner, and
remove exactly the five synthetic slugs:

```sql
DELETE FROM public.products
WHERE slug IN (
    'stage18-test-chair',
    'stage18-test-draft',
    'stage18-test-lamp',
    'stage18-test-archive',
    'stage18-test-vessel'
);
```

Confirm the affected-row count is exactly five. If it is not, stop and inspect
the dedicated local database instead of broadening the delete. Then remove the
copied connection variables from the PowerShell process without printing them:

```powershell
Remove-Item Env:PGDATABASE -ErrorAction SilentlyContinue
Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue
```

## Automated verification

The ordinary suite remains database-independent:

```powershell
go fmt ./...
go test -count=1 ./...
go vet ./...
```

It covers:

- exact migration-4 catalog identity, constraints, index, and strict reverse;
- Product repository construction and narrow interface behavior;
- exact list/detail SQL and positional parameters;
- canonical slug, UTF-8, field-length, identity, ordering, numbering, and
  duplicate validation;
- context forwarding and row closure;
- safe redaction of query, scan, iteration, close, and stored-data failures;
- empty, published, missing, draft, archived, malformed, and dependency-failure
  handler behavior;
- list/detail mapping, canonical paths, trusted published labels, and template
  escaping; and
- the rule that no temporary Product collection or unpublished state reaches a
  public response.

Stages 19–20 extend that suite with the all-state administrator reader, strict
Owner/Editor routes, version-guarded writer, Product forms, and public-link
separation described in [admin-products.md](admin-products.md).

The opt-in PostgreSQL suite additionally proves the real migration and window
queries against a guarded disposable database. It confirms that migration 4
seeds zero rows, inserts synthetic rows out of editorial order, produces
consecutive published-only numbers, uses ID to break equal sort positions,
keeps detail numbering consistent with the list, hides draft, archived, and
missing public slugs, and separately verifies protected all-state reads plus
real create, publication, and stale-version behavior.

Follow the two-variable destructive opt-in in [database.md](database.md), then
run:

```powershell
go test -count=1 -run 'Postgres' ./...
```

The live suite never falls back to `DATABASE_URL`. It must use a separately
confirmed database whose name ends in `_test`, and it cleans only its named
schema relations.

Before a focused commit, review the exact current Product-stage files and the
unchanged migration line endings:

```powershell
git status --short
git diff
git diff --check
git check-attr eol -- migrations/000004_create_products.up.sql
git check-attr eol -- migrations/000004_create_products.down.sql
git check-attr eol -- migrations/000005_add_product_version.up.sql
git check-attr eol -- migrations/000005_add_product_version.down.sql
```

## Explicitly deferred to future Product stages

Stages 18–20 do not implement:

- administrator Product deletion or retention;
- preview-before-publishing;
- durable change history or actor attribution;
- descriptions, materials, dimensions, designers, prices, purchasing, or stock;
- Product images, galleries, uploads, alt text, captions, ordering, cropping, or
  focal points;
- featured-homepage placement;
- Product SEO title or description;
- search, filtering, public pagination, or catalogue counts;
- real Zafarmand Product records; or
- Interior Design and Architecture database migration.

Those changes expand mutation authority, validation, media security, content
modeling, or public navigation. Future reviewed stages should design them from
the protected administrative workflow outward instead of adding hidden writes
to this public reader.
