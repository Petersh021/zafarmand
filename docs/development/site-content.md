# Stage 24 managed Homepage and Contact content

Stage 24 makes the small, global public-site content set durable without
turning Product, Interior, and Architecture records into one generic content
model. PostgreSQL owns the Homepage identity, one optional reviewed hero,
three fixed featured-work selections, Contact presentation, and exact Homepage
and Contact SEO metadata. The existing inquiry protocol and each discipline's
publication workflow remain separate.

Public handlers receive public-only projections. Protected readers expose the
current singleton revisions and editorial availability, while a separate
writer owns the three supported mutations. This keeps selected private IDs,
draft lifecycle data, optimistic versions, timestamps, and image bytes out of
ordinary public HTML reads.

## Included scope

This stage includes:

- migration 9 with independent Homepage, Homepage-hero, and Contact tables;
- truthful singleton seed copy with no personal contact details or portfolio
  records;
- managed Homepage identity, complete SEO metadata, and static-or-managed hero
  choice;
- one optional featured Product, Interior project, and Architecture project;
- managed Contact introduction, optional email, paired telephone values,
  optional multiline address, and complete SEO metadata;
- public publication and current-cover rechecks for every featured item;
- protected Owner/Editor detail, edit, and hero-management routes;
- strict forms, CSRF protection, optimistic concurrency, and Post/Redirect/Get;
- reviewed JPEG/PNG hero normalization and exact-revision delivery; and
- repository, HTTP, migration, and guarded PostgreSQL verification.

The stage does not change the Contact inquiry fields, persistence notice,
idempotency key, one-time success receipt, or privacy behavior. Those remain
application-owned security protocol rather than editable marketing copy.

## Migration 9

The immutable migration pair is:

```text
migrations/000009_create_homepage_contact_content.up.sql
migrations/000009_create_homepage_contact_content.down.sql
```

`public.homepage_content` is a singleton whose primary key is constrained to
`1`. It stores:

- required studio name and descriptor;
- a Boolean choice between the checked-in fallback and managed hero;
- nullable foreign keys for one Product, one Interior project, and one
  Architecture project;
- exact complete SEO title and description;
- a positive optimistic version; and
- ordered creation and update timestamps.

The feature foreign keys use `ON DELETE RESTRICT`. A referenced record cannot
be silently removed while it remains an editorial selection. Publication and
cover eligibility are deliberately not database constraints: lifecycle and
cover state can change later, so readers and the Homepage writer recheck them
at the point where they matter.

`public.homepage_hero_images` is a singleton child whose primary key is also
the Homepage foreign key. It stores one current normalized image: positive
media revision, decoder-derived JPEG/PNG type and dimensions, bytes and exact
byte size, SHA-256 digest, required alt text, and ordered timestamps. The
foreign key uses `ON DELETE CASCADE`; Stage 24 exposes replacement but no
deletion operation.

`public.contact_content` is a separate singleton constrained to primary key
`1`. It stores required eyebrow, heading, and introduction copy; optional
normalized email; an all-present-or-all-absent display/E.164 telephone pair;
optional address; exact complete SEO title and description; positive optimistic
version; and ordered timestamps. It does not store Contact inquiry rows.

The migration seeds only current structural copy:

```text
Homepage: Zafarmand / Design Studio
Homepage SEO: Home | Zafarmand / Zafarmand design studio
Contact: Contact / Begin a conversation
Contact introduction: Choose a discipline and share the context Zafarmand should review.
Contact SEO: Contact | Zafarmand / Zafarmand design studio
```

The three featured IDs are `NULL`, managed hero is disabled, no hero bytes are
inserted, and email, telephone, and address are empty. The migration therefore
does not invent studio contact information, portfolio records, or personal
data.

The down migration strictly removes the child and independent singleton before
the Homepage parent:

```sql
DROP TABLE public.homepage_hero_images;
DROP TABLE public.contact_content;
DROP TABLE public.homepage_content;
```

It intentionally omits `IF EXISTS` and `CASCADE`, so dependency drift remains
visible. Rolling migration 9 down permanently removes managed Homepage and
Contact settings and hero bytes. Do that only in a confirmed disposable
database. Never edit migration 9 after it has been applied; add a later
migration for a correction.

## Public behavior, SEO, and caching

The Stage 24 public contract is:

```text
GET /                         managed Homepage HTML
GET /homepage/hero/{version} exact enabled managed hero
GET /contact                  managed Contact copy plus inquiry form
POST /contact                 existing secure inquiry submission flow
```

`GET /` requires a valid Homepage singleton. It renders the checked-in reviewed
hero while managed mode is disabled. A successful hero upload stores normalized
media and enables managed mode atomically; a later Homepage edit can explicitly
return to the checked-in fallback without deleting the stored hero. If managed
mode is enabled but its required current hero cannot be read and validated, the
request fails closed instead of silently substituting different media.

The Homepage has exactly three optional feature slots. Candidates must be
Published and own a current reviewed cover when selected. The writer repeats
that check inside the update statement. Public reads recheck it again, so a
selected item that later becomes Draft or Archived, loses its cover, or is
otherwise unavailable is simply omitted. Public order is fixed as Interior,
Architecture, then Product; an absent slot does not create an empty card or
visible numbering gap. Paths and discipline labels are built by Go rather than
stored as administrator-authored URLs.

Successful Homepage HTML and managed hero responses use:

```text
Cache-Control: public, max-age=0, must-revalidate
```

The hero route accepts only the canonical positive decimal revision, exact
escaped path, and no query. It returns bytes only when that revision is current
and managed mode remains enabled. Success also supplies a digest-backed ETag,
`X-Content-Type-Options: nosniff`, and
`Cross-Origin-Resource-Policy: same-origin`. A matching conditional request may
receive `304 Not Modified`. Disabled, missing, and stale revisions are the same
public `404 Not Found`; all failure paths begin with `Cache-Control: no-store`.

`GET /contact` and every Contact POST result remain `Cache-Control: no-store`
because validation or persistence responses may contain visitor-supplied
personal information. Optional studio details are omitted when empty. The
application percent-escapes the reviewed mailbox as an address-only component
before the template adds its literal `mailto:` scheme, so mailbox punctuation
cannot become a header query or fragment. The grammar-constrained E.164 value
stays after a literal `tel:` scheme. `html/template` still applies contextual
escaping, and no administrator-authored trusted URL enters the template.

A missing or malformed content singleton and a database failure become the same
generic `503 Service Unavailable`. Fixed log categories omit SQL diagnostics,
stored editorial copy, feature IDs, image data, and visitor personal values.

Homepage and Contact each store one complete browser title and meta description
rather than a fragment that the template decorates again. Their canonical
paths are fixed application-owned relative paths (`/` and `/contact`), never a
request Host or managed URL. Per-record Product and project SEO is not part of
this stage.

## Protected routes and authorization

The protected contract is:

```text
GET  /admin/site-content
GET  /admin/site-content/homepage
GET  /admin/site-content/homepage/edit
POST /admin/site-content/homepage
GET  /admin/site-content/homepage/hero
POST /admin/site-content/homepage/hero
GET  /admin/site-content/homepage/hero/{version}
GET  /admin/site-content/contact
GET  /admin/site-content/contact/edit
POST /admin/site-content/contact
```

Every route first requires one active administrator session. `owner` and
`editor` are named explicitly in separate site-content read and mutation
allowlists. A future role receives neither permission until it is deliberately
added to the relevant route boundary. Both current roles can review and change
Homepage, hero, and Contact content; this does not create user-management,
deletion, or unrelated discipline authority.

All `/admin` responses retain the shared `Cache-Control: no-store`, restrictive
Content Security Policy, no-referrer, anti-framing, MIME, same-origin, robots,
and permissions headers. The exact protected hero preview remains available
while the stored hero exists, even when public Homepage settings use the static
fallback.

## Strict forms, validation, and concurrency

Homepage and Contact text mutations accept only
`application/x-www-form-urlencoded` bodies, exact known fields once each, no
query, and no content encoding. The complete encoded body is capped at 64 KiB.
The session-bound CSRF token is checked before mutation. Browser controls are
helpful hints; Go validation and database constraints jointly enforce the
durable bounds:

- studio name: 1–120 trimmed, single-line Unicode characters;
- descriptor: 1–160 trimmed, single-line characters;
- SEO title: 1–160 trimmed, single-line characters;
- SEO description: 1–320 trimmed, single-line characters;
- Contact eyebrow: 1–80 trimmed, single-line characters;
- Contact heading: 1–160 trimmed, single-line characters;
- Contact introduction: 1–1,200 trimmed reviewed characters;
- contact email: empty or one lowercase address with a dotted domain, at most
  254 characters;
- display phone: empty or at most 60 trimmed single-line characters;
- E.164 phone: paired with the display value and shaped like
  `+442071234567`; and
- address: empty or at most 500 trimmed reviewed characters with supported line
  breaks.

Feature selectors accept only empty or one canonical positive ID from the
matching eligible discipline set. An existing selection that later becomes
ineligible is shown as unavailable so an editor can understand and clear it;
arbitrary submitted IDs are never invented as options. Selecting managed hero
mode requires an already stored hero.

Homepage and Contact use independent positive versions. The hidden revision is
server-issued and is concurrency control, not editable content. Each writer
increments only when the expected version still matches. Homepage selection
eligibility and managed-hero existence are rechecked in the same SQL statement
as the update. Hero upload atomically advances the Homepage version, inserts or
increments the hero revision, stores the normalized bytes, and enables managed
mode. A concurrent stale form receives a fixed `409 Conflict` recovery page
without echoing an unpersisted file or overwriting newer work.

Semantic field errors return an accessible `422 Unprocessable Entity` form.
Malformed, oversized, unsupported-media, and forbidden requests use their
corresponding generic 4xx response without reflecting protected values.
Successful mutations redirect with `303 See Other` to a canonical detail GET,
so refresh does not replay a write.

## Reviewed hero boundary

The hero form accepts exactly one `image` file part and one each of `alt_text`,
`csrf_token`, and Homepage `version`. Unknown, duplicate, missing, encoded, or
malformed multipart parts are rejected. The complete request is capped at the
shared 8 MiB image limit plus 64 KiB of multipart allowance; individual text
parts are capped before character validation.

The source must decode as JPEG or PNG, be at most 8 MiB, at most 10,000 pixels
on either axis, and at most 25 megapixels. The server decodes and re-encodes the
pixels before persistence, stripping EXIF, XMP, text chunks, hidden thumbnails,
and color profiles instead of serving original upload bytes. JPEG uses the
reviewed quality setting of 90; PNG remains lossless. Browser filenames and
claimed MIME types are not persisted.

Go's JPEG decoder does not apply EXIF orientation. Export or rotate orientation
into the pixels before upload, then inspect the protected preview. Alt text is
required, trimmed, single-line reviewed copy from 1 to 300 Unicode code points.
The stage adds no hero caption, crop, focal point, rendition, or media deletion.

## Database roles and least privilege

Apply migration 9 with a migration role that can create tables, constraints,
foreign keys, and seed rows. Do not run the HTTP server with that schema owner.
The runtime role must retain the narrow Product, Interior, Architecture, cover,
inquiry, and administrator grants documented by their own guides. Homepage
feature reads need the already-documented discipline ID, slug, title/name,
typology/category, sort order, publication status, and cover version/dimensions/
alt-text columns.

The new Stage 24 runtime grants can remain column-level:

```sql
GRANT SELECT (
    id, studio_name, descriptor, managed_hero_enabled,
    featured_product_id, featured_interior_project_id,
    featured_architecture_project_id, seo_title, seo_description,
    version, created_at, updated_at
) ON public.homepage_content TO chosen_runtime_role;

GRANT UPDATE (
    studio_name, descriptor, managed_hero_enabled,
    featured_product_id, featured_interior_project_id,
    featured_architecture_project_id, seo_title, seo_description,
    version, updated_at
) ON public.homepage_content TO chosen_runtime_role;

GRANT SELECT (
    homepage_content_id, version, content_type, content, byte_size,
    width, height, sha256, alt_text, created_at, updated_at
) ON public.homepage_hero_images TO chosen_runtime_role;

GRANT INSERT (
    homepage_content_id, version, content_type, content, byte_size,
    width, height, sha256, alt_text
) ON public.homepage_hero_images TO chosen_runtime_role;

GRANT UPDATE (
    version, content_type, content, byte_size, width, height,
    sha256, alt_text, updated_at
) ON public.homepage_hero_images TO chosen_runtime_role;

GRANT SELECT (
    id, eyebrow, heading, introduction, contact_email,
    phone_display, phone_e164, address, seo_title, seo_description,
    version, created_at, updated_at
) ON public.contact_content TO chosen_runtime_role;

GRANT UPDATE (
    eyebrow, heading, introduction, contact_email,
    phone_display, phone_e164, address, seo_title, seo_description,
    version, updated_at
) ON public.contact_content TO chosen_runtime_role;
```

Replace `chosen_runtime_role` through operator-managed deployment
configuration. Stage 24 needs no new sequence, `DELETE`, `TRUNCATE`, schema
ownership, migration-ledger access, or administrator-user mutation. Review the
fixed SQL against these grants in a non-production environment before
deployment; do not replace the lists with a broad schema grant merely for
convenience.

## Local setup and manual verification

First put the migration-role URL in the current PowerShell process using the
history-safe helper from [admin-access.md](admin-access.md), then apply and
inspect the catalog without printing the value:

```powershell
Set-SecretProcessVariable `
    -Name 'DATABASE_URL' `
    -Prompt 'Enter the PostgreSQL URL for the migration role'

go run . migrate status
go run . migrate up
go run . migrate status
```

Versions 1 through 10 should be Applied; migration 10's inquiry-retention
support does not alter Site-content behavior. A second `migrate up` should be a
truthful no-op. Replace `DATABASE_URL` with the reviewed least-privilege runtime
URL, start the application, and use fictional values only:

```powershell
Set-SecretProcessVariable `
    -Name 'DATABASE_URL' `
    -Prompt 'Enter the PostgreSQL URL for the runtime role'

go run .
```

Then verify:

1. Open `http://localhost:8080/`. Confirm the seeded identity, exact title and
   description, static fallback hero, and absence of a featured-work section.
2. Open `http://localhost:8080/contact`. Confirm the seeded introduction, empty
   direct-contact region, exact title and description, existing storage notice,
   and working inquiry form.
3. Sign in at `http://localhost:8080/admin/login`, then open
   `http://localhost:8080/admin/site-content`. Repeat protected read/write
   checks with one fictional local Owner and one fictional local Editor.
4. Edit Homepage identity and SEO with fictional copy. Open the edit form in
   two tabs, save tab A, then submit tab B; tab B must receive `409 Conflict`
   and must not overwrite tab A.
5. Create or reuse fictional Published Product, Interior, and Architecture
   records with reviewed covers. Select one per slot and confirm public order is
   Interior, Architecture, Product. Archive one selected record through its
   existing workflow; its public feature must disappear while the stored admin
   selection remains visible as unavailable.
6. Upload a small fictional JPEG or PNG with meaningful fictional alt text.
   Confirm the protected normalized preview, managed hero on `/`, the exact
   revisioned public URL, ETag revalidation, and the old revision returning 404
   after replacement. Switch Homepage settings back to fallback and confirm the
   managed URL becomes 404 while the stored protected preview remains.
7. Edit Contact using a two-line fictional address,
   `studio@example.invalid`, and the fictional phone pair `+1 555 010 0200` /
   `+15550100200`. Confirm link
   destinations, contextual escaping, multiline address presentation, and
   unchanged inquiry security and persistence copy. Clear all optional details
   and confirm the direct-contact region is omitted again.
8. Open Contact edit in two tabs and repeat the stale-form check. Confirm every
   successful POST redirects with 303 and every admin response is `no-store`.
9. Sign out and confirm every `/admin/site-content` route returns to the login
   boundary. Inspect cookie attributes and response headers, never their values.

At 375, 430, 768, 960, 1024, 1280, and 1920 CSS pixels, check for horizontal
overflow, readable wrapping, visible focus, logical keyboard order, meaningful
image alternatives, 200% zoom, and the expected reduced-motion behavior.

## Automated and guarded PostgreSQL verification

The normal suite is database-independent:

```powershell
go fmt ./...
go test -count=1 ./...
go vet ./...
git diff --check
```

It covers singleton validation, public projection filtering, managed SEO and
hero mapping, Contact privacy and omission, exact media behavior, route method
and role authorization, strict forms, CSRF, feature eligibility, image
normalization, optimistic conflicts, generic failure mapping, and dependency
composition.

The opt-in PostgreSQL tests are destructive. Use only a dedicated empty
database whose configured and server-reported names end in `_test`. It must
contain none of the reserved application relations, including
`homepage_content`, `homepage_hero_images`, and `contact_content`. The test
never falls back to `DATABASE_URL`.

```powershell
Set-SecretProcessVariable `
    -Name 'ZAFARMAND_TEST_DATABASE_URL' `
    -Prompt 'Enter the disposable PostgreSQL test URL'

$env:ZAFARMAND_TEST_DATABASE_CONFIRM = 'stage13-disposable-database'
go test -count=1 -run 'Postgres' ./...

Remove-Item Env:ZAFARMAND_TEST_DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:ZAFARMAND_TEST_DATABASE_CONFIRM -ErrorAction SilentlyContinue
```

The guard verifies the literal confirmation, both database names, and every
reserved relation before mutation. Once opted in, an unreachable PostgreSQL
server is a failure rather than a skip. The live suite exercises the complete
v1-to-v10 migration lifecycle, migration-9 constraints and seeds, strict
rollback/reapplication, public singleton and eligibility reads, enabled/disabled
hero visibility, and real protected updates. The `Postgres` selection includes
`TestPostgresSiteContentReaderIntegration` and
`TestPostgresAdminSiteContentPersistenceIntegration`; cleanup removes only its
exact relations in dependency order. The confirmation is a safety gate, not a
backup.

## Deferred after Stage 24

- Product-, Interior-, and Architecture-record SEO fields;
- social links, analytics, and administrator-authored external URLs;
- galleries, drag ordering, crop, focal point, and generated renditions;
- object storage, CDN, and media deletion/retention operations;
- contact email delivery, availability, and response-time promises;
- preview-before-publishing links;
- durable audit history and actor attribution;
- administrator account management, password recovery, and multi-factor
  authentication;
- deployment, backup, observability, and retention hardening from Stage 25; and
- final accessibility, performance, end-to-end acceptance, and deployment work
  from Stage 26.
