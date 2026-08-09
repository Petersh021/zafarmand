# Stage 16 read-only administrator inquiries

Stage 16 turns the authenticated administrator shell into one small, useful
vertical feature: an owner or editor can review inquiries already saved by the
public Contact form. The feature is deliberately read-only. It proves the
authorization, privacy, repository, pagination, and server-rendered interface
boundaries before a later stage adds any workflow that changes stored data.

This guide supplements the [administrator access guide](admin-access.md) and
the [database development guide](database.md). Stage 15 authentication must be
working, and migrations 1 through 3 must already be applied.

## Exact Stage 16 scope

The stage adds two protected, server-rendered routes:

```text
GET /admin/inquiries       show one page of inquiry summaries
GET /admin/inquiries/{id}  show one complete inquiry
```

A GET route also answers HEAD according to Go's `http.ServeMux` method rules.
There are no inquiry POST, PUT, PATCH, or DELETE routes in this stage. Opening
an inquiry does not silently change its status.

Both current roles may read the two pages:

- `owner`
- `editor`

The routes first use the existing session middleware and then a separate,
explicit role allowlist. This separation matters: authentication establishes
who made the request, while authorization decides whether that identity may
perform this operation. A future role does not inherit inquiry access merely
because it can authenticate. Missing or invalid sessions follow the established
login redirect; an authenticated but unlisted role fails closed with `403
Forbidden`.

This follows the deny-by-default and per-request authorization principles in
the OWASP [Authorization Cheat
Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html).
The project still needs tests for its own exact rules because a general
security reference cannot prove an application's route composition.

## Personal-data boundary

The inbox query and template receive only the fields needed to choose an
inquiry:

- internal inquiry reference;
- visitor name;
- discipline;
- current status; and
- received timestamp.

Email address and full message are omitted from every list query and list view.
They are selected only when an authorized administrator opens one detail page.
The detail template receives ordinary Go strings, so `html/template` escapes
visitor text. CSS preserves message line breaks without treating the message as
HTML. The email is displayed as text; Stage 16 does not open an external mail
client.

The 32-byte `submission_key` is an idempotency implementation detail. No Stage
16 SQL statement selects it, no page model contains it, and no interface
displays it. Password verifiers, session digests, CSRF digests, and raw browser
tokens remain outside the inquiry reader for the same least-data reason.

Every `/admin` response continues to receive `Cache-Control: no-store` plus the
existing CSP, no-referrer, anti-framing, no-sniff, no-index, opener, resource,
and permissions headers. Do not add an ETag, browser storage, client-side cache,
or public cache around these pages.

Repository and handler failures cross the boundary as data-free categories.
Application log messages identify only the failed operation. They must not
include a name, email address, message, `submission_key`, URL cursor, session
value, SQL statement, or lower-level database diagnostic. This is consistent
with the data-exclusion guidance in the OWASP [Logging Cheat
Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html).

## Separate read repository

The public Contact handler still depends on its narrow write-only
`inquiryRepository`. Stage 16 injects a separate `adminInquiryReader` into the
application. Keeping the interfaces separate means an admin read feature does
not silently widen what the unauthenticated Contact handler can do.

The reader exposes only two operations:

```text
List(ctx, beforeID)  return one bounded page of summaries
FindByID(ctx, id)    return one complete inquiry or not found
```

Both operations use a request context with the existing bounded administrator
repository timeout. All visitor-controlled or URL-derived values travel as SQL
parameters. Rows are checked again in Go before they become trusted view data:
IDs must be positive, stored strings must retain their expected shape, the
discipline and status must belong to closed vocabularies, and timestamps must
be internally consistent. A malformed stored row becomes a generic service
failure rather than partially rendered personal data.

## Twenty-item ID keyset pagination

The newest inbox page contains at most 20 summaries. PostgreSQL is asked for 21
rows; the extra row is only evidence that an older page exists and is removed
before the page reaches the template. This avoids a separate `COUNT(*)` query
and prevents a request from selecting an unbounded amount of personal data.

The first page uses this order:

```sql
ORDER BY id DESC
LIMIT 21
```

An older page uses the final visible ID from the current page as an exclusive
upper bound:

```sql
WHERE id < $1
ORDER BY id DESC
LIMIT 21
```

The browser-facing spelling is therefore:

```text
/admin/inquiries?before=<positive-inquiry-id>
```

This is keyset pagination. It is preferable here to `OFFSET` for two reasons:

1. A newly inserted inquiry that receives a higher ID appears above the current
   position without moving the older keyset boundary. Offset pages can duplicate
   or skip visible rows when records are inserted before the offset between
   requests.
2. PostgreSQL can seek below the supplied primary-key value instead of walking
   and discarding every earlier offset row on a deep page.

The existing `inquiries` primary-key B-tree supports the lookup and can be
scanned backward for descending ID order. Stage 16 therefore adds **no migration
4 and no new index**. PostgreSQL guarantees a chosen result order only when an
explicit `ORDER BY` is present; its documentation explains both [row
sorting](https://www.postgresql.org/docs/current/queries-order.html) and how
[B-tree indexes satisfy ordered
queries](https://www.postgresql.org/docs/current/indexes-ordering.html).

### Receipt-order timestamp caveat

The queue order is the descending database identity, not a claim of perfect
wall-clock chronology. An identity value is allocated when an insert asks for
one. Failed or rolled-back inserts can leave harmless gaps, and concurrent
transactions can allocate IDs, start at different times, and commit in a
different order. PostgreSQL's `CURRENT_TIMESTAMP` also describes the current
transaction's start time.

Consequently, two concurrently submitted inquiries can display timestamps that
look slightly out of sequence while their ID order remains deterministic. The
timestamp is useful interface information, but the `before=id` boundary defines
pagination. If a later business requirement demands strict event-time
pagination, that should be a separately designed `(created_at, id)` keyset and
index migration rather than an undocumented change to this contract.

## URL validation and safe failures

The list accepts either no query string or exactly one `before` value. The
detail route accepts no query string. A cursor or path ID must be the canonical
base-10 spelling of one positive signed 64-bit integer. Signs, whitespace,
zero, leading zeroes, percent-encoded alternate spellings, duplicates, unknown
query fields, non-decimal text, and overflow are rejected.

Malformed list navigation returns a generic `400 Bad Request`. A malformed
detail ID and a well-formed ID with no row both return the same ordinary `404
Not Found`. Database and stored-data failures return a generic `503 Service
Unavailable`. None of those responses reflects the supplied value or reveals
database diagnostics.

Numeric inquiry IDs and cursors are not authentication secrets; authorization
still protects every record. Strict parsing nevertheless keeps routing
canonical, prevents ambiguous inputs, and makes it clear which values can
reach a parameterized query.

## Database permission change without a schema change

Stage 16 does not alter a table, constraint, or index, so `go run . migrate
status` should still report only versions 1, 2, and 3 as applied. Do not create
an empty migration merely to mark an application-code stage.

The least-privilege runtime PostgreSQL role does need one deliberate grant
change: it must be able to `SELECT` the columns required from
`public.inquiries`. Stage 14 needed an insert and a narrow replay read; Stage 16
adds list and detail reads. The runtime role still must not own the schema,
alter migrations, create administrator users, or grant privileges.

Define and test grants in deployment automation or provider configuration, not
in committed application source. The Go process continues to use one shared
pool, while separate repository interfaces restrict application-level access.

## Manual verification with local test data

Use a local or disposable development database and an intentionally fictional
Contact submission. Never copy a real visitor's name, email, message, token, or
database row into a terminal transcript, screenshot, chat, issue, or test.

1. Confirm migrations 1 through 3 are applied with `go run . migrate status`.
2. Start the application with the local runtime `DATABASE_URL` and sign in as a
   local test owner or editor.
3. In another private browser context, submit a Contact form using unmistakably
   fictional data such as `Stage 16 Test` and `stage16@example.invalid`, plus a
   short message containing no real project information.
4. Open `http://localhost:8080/admin/inquiries`. The newest row should show the
   fictional name, discipline, `New` status, and UTC receipt time. The list must
   not show the email address or full message.
5. Open that row's detail link. It should use a canonical numeric URL and show
   the fictional email and escaped message. It must not show a submission key.
6. Confirm both a local owner account and a local editor account can read the
   pages. Sign out and confirm an unauthenticated visit returns to the login
   flow.
7. Inspect response **header names and values only**. Confirm `Cache-Control:
   no-store` and the established admin security policy; do not copy cookie
   values or page content.
8. Try `?before=0`, `?before=01`, a duplicate `before`, and an unknown query
   field. Each list request should fail generically. Try detail IDs `0`, `01`,
   and a positive missing ID; each should produce an ordinary 404.
9. Check the inbox and one fictional detail at 375, 430, 768, 960, 1024, and
   1280 CSS pixels. The header may wrap, but links, identity, logout, cards,
   facts, and the message must never overlap or create horizontal scrolling.
10. Repeat a keyboard-only pass at 200% browser zoom. Focus must stay visible,
    the skip link must reach the main content, and every navigation, card,
    pagination, back, and logout control must remain reachable in source order.

The browser check does not need 21 handcrafted records. Page boundaries and
insertion-during-pagination behavior belong to deterministic automated tests.
Because deletion is intentionally absent, use disposable data or retain only
the clearly fictional local row until a later reviewed cleanup workflow exists.

## Automated verification

Run the ordinary database-independent checks from the repository root:

```powershell
go fmt ./...
go test -count=1 ./...
go vet ./...
```

The tests cover owner/editor authorization, missing-context denial, route and
method behavior, strict `before` and detail-ID parsing, 20-item page boundaries,
stable descending keyset traversal, empty and not-found responses, data-free
error handling, trusted label mapping, template escaping, no-store headers,
and the rule that list reads omit email, message, and submission key.

The opt-in PostgreSQL suite described in [database.md](database.md) additionally
checks the real list and detail statements, descending page boundaries, field
mapping, missing records, and reader construction. It continues to test
the existing migration 1-to-3 cycle because Stage 16 has no schema migration.

## Explicitly deferred work

Stage 16 does not implement:

- status mutation or automatic marking as reviewed;
- search or filtering;
- export or bulk access;
- deletion or retention enforcement;
- email sending or mail-client integration;
- assignment, notes, or replies;
- a durable access-audit event store; or
- a finalized organizational retention and privacy policy.

Those capabilities change authorization, personal-data, logging, or destructive
operation requirements. Each needs its own focused stage rather than being
hidden inside this read-only foundation.
