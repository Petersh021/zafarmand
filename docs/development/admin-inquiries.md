# Stages 16–17 administrator inquiries

Stage 16 turns the authenticated administrator shell into one small, useful
vertical feature: an owner or editor can review inquiries already saved by the
public Contact form. That first feature is deliberately read-only. It proves the
authorization, privacy, repository, pagination, and server-rendered interface
boundaries before any workflow changes stored data.

Stage 17 adds the first narrow workflow mutation: an owner or editor can
explicitly set one inquiry to `new`, `reviewed`, or `archived`. Opening a detail
page still changes nothing. The update requires a native POST form, the existing
session-bound CSRF secret, an explicit target status, and a successful database
write before the browser returns to the canonical detail page.

This guide supplements the [administrator access guide](admin-access.md) and
the [database development guide](database.md). Stage 15 authentication must be
working, and the current migrations 1 through 7 must already be applied before
starting the Stage 22 server. Migrations 4–6 belong to Products and migration 7
belongs to Interior projects; none changes inquiry behavior. Their separate
boundaries are documented in [products.md](products.md) and
[interior-projects.md](interior-projects.md).

## Exact route scope

Stage 16 adds two protected, server-rendered routes:

```text
GET /admin/inquiries       show one page of inquiry summaries
GET /admin/inquiries/{id}  show one complete inquiry
```

Stage 17 adds one protected state-changing route:

```text
POST /admin/inquiries/{id}/status  set one explicit workflow status
```

A GET route also answers HEAD according to Go's `http.ServeMux` method rules.
There is no status-changing GET, automatic mark-as-reviewed behavior, PUT,
PATCH, or DELETE route. Opening or refreshing an inquiry therefore does not
silently change its status.

Both current roles may read the pages and submit the status form:

- `owner`
- `editor`

The routes first use the existing session middleware and then separate,
explicit read and mutation role allowlists. This separation matters:
authentication establishes who made the request, while authorization decides
whether that identity may perform this particular operation. A future role
does not inherit inquiry read or mutation access merely because it can
authenticate. Missing or invalid sessions follow the established login
redirect; an authenticated but unlisted role fails closed with `403 Forbidden`.

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

The Stage 17 form sends only `csrf_token` and `status`. It never repeats the
visitor name, email address, message, submission key, administrator identity,
or a caller-provided return URL in hidden controls. Hidden form values are
attacker-editable input, not authorization evidence; the handler independently
validates the session, CSRF secret, canonical inquiry ID, role, and closed
status vocabulary.

Every `/admin` response continues to receive `Cache-Control: no-store` plus the
existing CSP, no-referrer, anti-framing, no-sniff, no-index, opener, resource,
and permissions headers. Do not add an ETag, browser storage, client-side cache,
or public cache around these pages.

Repository and handler failures cross the boundary as data-free categories.
Application log messages identify only the failed operation. They must not
include a name, email address, message, `submission_key`, URL cursor, target
status, session value, form body, SQL statement, or lower-level database
diagnostic. This is consistent with the data-exclusion guidance in the OWASP [Logging Cheat
Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html).

## Separate read and status repositories

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

Stage 17 adds a separate narrow administrator status writer rather than adding
read or update methods to the public Contact repository:

```text
UpdateStatus(ctx, id, status)  set one supported status for one inquiry
```

The HTTP handler and repository both reject unsupported statuses. The writer
updates only `status` and `updated_at`; it never receives or rewrites the
visitor's name, email, discipline, message, or submission key. Keeping this
dependency separate prevents the unauthenticated Contact handler from gaining
administrator update authority.

## Explicit, no-JavaScript status workflow

The detail page displays `Current status: New`, `Current status: Reviewed`, or
`Current status: Archived` in text. Color reinforces that label but never
carries the meaning by itself. After the project message, one native HTML form
groups exactly two server-provided alternatives under a `Change status`
legend. The current status is omitted as an action, and every remaining button
names its complete result:

- `Mark as new`;
- `Mark as reviewed`; or
- `Mark as archived`.

Only the deliberately activated submit button contributes the `status` field.
The form has no automatic submission and requires no JavaScript. Its hidden
`csrf_token` is the same random, session-bound value already used by the
authenticated logout form. The handler requires exactly one canonical token
and one canonical supported status, compares the token in constant time, and
performs no repository operation when validation fails. CSRF values never
belong in a URL, log, browser storage, screenshot, or documentation transcript.

On success, the handler returns `303 See Other` to the canonical detail GET.
This Post/Redirect/Get boundary means refreshing the resulting page performs a
read instead of repeating the mutation. The re-rendered textual badge and two
new alternatives provide the persistent result; no script-driven live region
is necessary for the full-page navigation.

The native fieldset, legend, and buttons preserve programmatic relationships,
visible labels, keyboard behavior, and browser high-contrast adaptation. This
follows the W3C guidance for [grouping form
controls](https://www.w3.org/WAI/tutorials/forms/grouping/) and [native HTML
controls](https://www.w3.org/WAI/WCAG22/Techniques/html/H91.html). Requiring the
existing session token on this state-changing POST follows the OWASP [CSRF
Prevention Cheat
Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html).

### Idempotency and concurrent administrators

Submitting a valid status that is already stored is an idempotent success. It
preserves the stored `updated_at` value, which keeps that timestamp meaningful
as the time of the latest real transition. The normal interface omits this
no-op button, but the repository still enforces the invariant for repeated or
stale requests.

Stage 17 deliberately uses last-successful-write-wins behavior. If two
administrators open the same inquiry and submit different supported statuses,
the database status from the update that completes last becomes current. The
stage does not claim edit-conflict detection, actor attribution, or durable
status history. Those require a separately designed version or audit model.

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
scanned backward for descending ID order. Stage 16 therefore required no new
inquiry index or migration. The later migration 4 creates only Product storage.
PostgreSQL guarantees a chosen result order only when an explicit `ORDER BY` is
present; its documentation explains both [row
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
detail and status routes accept no query string. A cursor or path ID must be the
canonical base-10 spelling of one positive signed 64-bit integer. Signs,
whitespace, zero, leading zeroes, percent-encoded alternate spellings,
duplicates, unknown query fields, non-decimal text, and overflow are rejected.

Malformed list navigation returns a generic `400 Bad Request`. A malformed
detail ID and a well-formed ID with no row both return the same ordinary `404
Not Found`. Database and stored-data failures return a generic `503 Service
Unavailable`. None of those responses reflects the supplied value or reveals
database diagnostics.

The mutation additionally accepts only
`application/x-www-form-urlencoded` within the existing bounded admin form
size, with exactly one `csrf_token` and one `status` field. A missing, duplicate,
unknown, non-canonical, or unsupported field fails generically with `400 Bad
Request`. A wrong media type or nonempty content encoding returns `415
Unsupported Media Type`. A structurally present but empty, malformed,
non-canonical, or mismatched CSRF value returns `403 Forbidden`. A valid inquiry
ID with no row returns the same ordinary 404 used by detail reads. Repository
failure remains a data-free 503. Only a successful update redirects with 303.

Numeric inquiry IDs and cursors are not authentication secrets; authorization
still protects every record. Strict parsing nevertheless keeps routing
canonical, prevents ambiguous inputs, and makes it clear which values can
reach a parameterized query.

## Database permission change without a schema change

Stages 16 and 17 did not alter an inquiry table, constraint, or index. Stage 17
reuses the existing `status`, `updated_at`, and
`inquiries_status_supported` constraint; no empty inquiry migration should be
created merely to mark an application-code stage. In the current Stage 22
project, `go run . migrate status` should report versions 1 through 7 because
migrations 4–6 independently add Product storage, revision, and content/cover
storage, while migration 7 adds separate Interior-project storage. None changes
the inquiry schema.

The least-privilege runtime PostgreSQL role does need one deliberate grant
change: it must be able to `SELECT` the columns required from
`public.inquiries`. Stage 14 needed an insert and a narrow replay read; Stage 16
adds list and detail reads; and Stage 17 needs column-level `UPDATE` permission
for only `status` and `updated_at`. The runtime role still must not own the
schema, alter migrations, update visitor content, create administrator users,
or grant privileges.

Define and test grants in deployment automation or provider configuration, not
in committed application source. The Go process continues to use one shared
pool, while separate repository interfaces restrict application-level access.

## Manual verification with local test data

Use a local or disposable development database and an intentionally fictional
Contact submission. Never copy a real visitor's name, email, message, token, or
database row into a terminal transcript, screenshot, chat, issue, or test.

1. Confirm migrations 1 through 7 are applied with `go run . migrate status`.
2. Start the application with the local runtime `DATABASE_URL` and sign in as a
   local test owner or editor.
3. In another private browser context, submit a Contact form using unmistakably
   fictional data such as `Stage 17 Test` and `stage17@example.invalid`, plus a
   short message containing no real project information.
4. Open `http://localhost:8080/admin/inquiries`. The newest row should show the
   fictional name, discipline, `New` status, and UTC receipt time. The list must
   not show the email address or full message.
5. Open that row's detail link. It should use a canonical numeric URL, show the
   fictional email and escaped message, and visibly say `Current status: New`.
   It must not show a submission key. Refreshing this GET must not change the
   status.
6. Disable JavaScript or use a browser profile where it is unavailable. With
   the keyboard, activate `Mark as reviewed`. The POST should finish at the
   canonical detail URL without a query string and display `Current status:
   Reviewed`. Refreshing that result must not resubmit a form.
7. Return to the inbox and confirm the same fictional row says `Reviewed`.
   From the detail page, mark it archived and confirm it remains stored,
   visible, and reversible. Return it to `New` when the exercise is finished.
8. Confirm both a local owner account and a local editor account can read and
   change the fictional row. Sign out and confirm an unauthenticated visit
   returns to the login flow.
9. In two tabs, open the same fictional inquiry, choose different statuses,
   and submit them in sequence. The last successfully completed update should
   be current. This stage deliberately does not show an edit-conflict warning.
10. Inspect response **header names and values only**. Confirm `Cache-Control:
   no-store` and the established admin security policy; do not copy cookie
   values or page content.
11. Try `?before=0`, `?before=01`, a duplicate `before`, and an unknown query
   field. Each list request should fail generically. Try detail IDs `0`, `01`,
   and a positive missing ID; each should produce an ordinary 404.
12. Check the inbox and one fictional detail at 375, 430, 768, 960, 1024, and
   1280 CSS pixels. The header may wrap, but links, identity, logout, cards,
   facts, message, workflow legend, and status buttons must never overlap or
   create horizontal scrolling.
13. Repeat a keyboard-only pass at 200% browser zoom and with Windows forced
   colors enabled. Focus and control borders must stay visible, the skip link
   must reach the main content, and every navigation, card, pagination, back,
   status, and logout control must remain reachable in source order.

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

The tests cover owner/editor read and mutation authorization, missing-context
denial, route and method behavior, strict `before`, detail-ID, form, status, and
CSRF parsing, 20-item page boundaries, stable descending keyset traversal,
empty and not-found responses, data-free error handling, trusted label and
action mapping, template escaping, no-store headers, Post/Redirect/Get,
same-status timestamp preservation, successive explicit transitions, and
the rule that list reads and status writes omit email, message, and submission
key.

The opt-in PostgreSQL suite described in [database.md](database.md) additionally
checks the real list, detail, and conditional status statements, descending
page boundaries, field mapping, missing records, timestamp behavior, and
repository construction. It now tests the complete migration 1-to-7 cycle;
Stage 17 still has no schema migration, migrations 4–6 belong to Products, and
migration 7 belongs to Interior projects.

## Explicitly deferred work

Stage 17 does not implement:

- automatic marking as reviewed merely by opening a detail page;
- optimistic concurrency, edit-conflict warnings, or version tokens;
- search or filtering;
- export, bulk access, or bulk status changes;
- deletion or retention enforcement;
- email sending or mail-client integration;
- assignment, notes, or replies;
- a durable status/access audit history or actor attribution;
- status-change notifications; or
- a finalized organizational retention and privacy policy.

Those capabilities change authorization, personal-data, logging, or destructive
operation requirements. Each needs its own focused stage rather than being
hidden inside this manual status workflow.
