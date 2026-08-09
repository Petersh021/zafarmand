# Stage 15 administrator access

Stage 15 adds the smallest secure access boundary needed before Zafarmand can
grow a private content-management area. It creates administrator and session
storage, provides an operator-only first-user command, and adds login,
dashboard, and logout routes.

Stage 16 now builds one bounded feature on that foundation: both current roles
can read saved Contact inquiries through a protected list and detail view. See
the [Stage 16 administrator inquiry guide](admin-inquiries.md) for its exact
authorization, pagination, privacy, and verification contract.

Stage 15 itself deliberately added no management feature; its first dashboard
was only an authenticated shell. The current project still has no inquiry
mutation, product, project, media, publishing, account-management, password
change or recovery, multi-factor authentication, or audit-reporting workflow.

Expired rows are rejected during authentication but Stage 15 does not yet run
a scheduled session-pruning job. The expiry index prepares for that later
maintenance without making it part of an HTTP request.

The two current roles are `owner` and `editor`. Stage 15 validates and displays
those labels. Stage 16 explicitly authorizes both roles to read inquiries, but
does not give either role a mutation or broader management permission. Do not
describe an owner as having working user-management powers or an editor as
having general content permissions until later handlers enforce those rules.

## What migration 000003 adds

The paired migration
`000003_create_admin_access.up.sql`/`.down.sql` adds two tables:

- `public.admin_users` stores a normalized unique email address, a one-way
  password verifier, an `owner` or `editor` role, an active flag, and
  timestamps. It never stores a plaintext password.
- `public.admin_sessions` stores hashes of the browser's session and CSRF
  tokens, an absolute expiry, and an optional revocation time. A foreign key
  removes a user's sessions if that user is deliberately deleted later.

The migration also indexes session user IDs and expiry times. The token hashes
are fixed at 32 bytes, session expiry must follow creation, and revocation
cannot predate creation. These database constraints defend the same invariants
as Go if a future maintenance tool writes directly to PostgreSQL.

As in earlier stages, the server never migrates itself. Apply migrations with
an appropriately privileged migration role before starting the updated
application:

```powershell
go run . migrate status
go run . migrate up
go run . migrate status
```

The final status should report versions 1, 2, and 3 as applied. Do not edit an
applied migration. Add a new version for any later schema correction.

`go run . migrate down --confirm` reverses only the newest applied version and
drops the Stage 15 session and user tables. That destroys administrator access
records. Use it only in a verified disposable database, never as a production
cleanup technique.

## Creating the first administrator safely

There is no public registration route. An operator creates an initial user
from the repository root with this closed command grammar:

```text
go run . admin create-user --email <email> --role <owner|editor>
```

The email is trimmed and lowercased, then validated as exactly one plain
mailbox. Display-name syntax such as `Person <address>` is rejected. The role
is case-sensitive and must be exactly `owner` or `editor`.

The command reads the database connection from `DATABASE_URL` and the password
from `ZAFARMAND_ADMIN_PASSWORD`. The password is intentionally absent from the
command arguments, process list, and command's success output.

Do not type either secret as a literal PowerShell assignment: shell history can
retain the complete line. The following PowerShell 5.1-compatible helper asks
for a hidden value and installs it only in the current process environment.
The native environment ultimately needs a plaintext string, so this reduces
accidental display and history exposure; it is not a replacement for an
operating-system or deployment secret manager.

```powershell
function Set-SecretProcessVariable {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,

        [Parameter(Mandatory = $true)]
        [string]$Prompt
    )

    # Read-Host receives the secret interactively, so its value is not part of
    # the recorded command. The unmanaged copy is zeroed in every outcome.
    $secureValue = Read-Host $Prompt -AsSecureString
    $valuePointer = [IntPtr]::Zero
    try {
        $valuePointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR(
            $secureValue
        )
        [Environment]::SetEnvironmentVariable(
            $Name,
            [Runtime.InteropServices.Marshal]::PtrToStringBSTR($valuePointer),
            [EnvironmentVariableTarget]::Process
        )
    }
    finally {
        if ($valuePointer -ne [IntPtr]::Zero) {
            [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($valuePointer)
        }
    }
}

Set-SecretProcessVariable `
    -Name 'DATABASE_URL' `
    -Prompt 'Enter the PostgreSQL URL for the bootstrap role'
Set-SecretProcessVariable `
    -Name 'ZAFARMAND_ADMIN_PASSWORD' `
    -Prompt 'Enter the first administrator password'

# Reading the email interactively also keeps the real address out of the
# recorded command line. Choose editor instead only when that is intentional.
$adminEmail = Read-Host 'Enter the first administrator email'
go run . admin create-user --email $adminEmail --role owner

# Remove all sensitive process values immediately, including on a failed run.
Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:ZAFARMAND_ADMIN_PASSWORD -ErrorAction SilentlyContinue
$adminEmail = $null
```

For stronger cleanup guarantees, run the setup and command inside a `try` block
and put the three cleanup lines in `finally`. Close the PowerShell window after
the operation. Never use `setx`, a PowerShell profile, a committed `.env` file,
a command-line flag, a screenshot, or a pasted chat/issue to carry either
secret. Production should inject secrets through the deployment platform's
approved secret facility.

The password must contain between 15 and 128 Unicode code points. Go rejects
invalid UTF-8 and the NUL character and does not trim or normalize a password:
deliberate spaces are part of it. Use a unique long passphrase generated or
stored by a password manager; do not reuse a personal or database password.

The command prints only `Administrator account created.` on success. It does
not echo the email, role, plaintext password, hash, or database URL. Running it
again for the same normalized address fails without creating a duplicate.

## Routes and expected behavior

Stage 15 adds these real server-rendered routes:

```text
GET  /admin/login   render the anonymous login form
POST /admin/login   validate credentials and create a session
GET  /admin         render the protected dashboard shell
POST /admin/logout  revoke the session and clear its cookies
```

Stage 16 separately adds these authenticated and explicitly authorized routes:

```text
GET /admin/inquiries       list up to 20 inquiry summaries
GET /admin/inquiries/{id}  show one inquiry detail
```

Both `owner` and `editor` are on the inquiry-reader allowlist. A future role is
denied unless route composition deliberately adds it. The inquiry routes are
read-only, so viewing a detail does not change its status.

There is intentionally no `GET /admin/logout`: logout changes server state and
therefore requires a protected POST. An unauthenticated visit to `/admin`
redirects to `/admin/login`. A successful login redirects with HTTP 303 to
`/admin`; a successful logout redirects with HTTP 303 back to `/admin/login`.

Login failures do not reveal whether an address is missing, inactive, or has a
wrong password. Unknown valid addresses still perform an intentionally
expensive dummy password verification, reducing account-enumeration timing
differences. Database or entropy failures return a generic unavailable response
and do not include credentials or driver detail.

The dashboard displays the authenticated email and trusted role label and now
links to the Stage 16 read-only inquiry inbox. It provides a logout form but no
product, project, media, inquiry mutation, user, or publishing controls.

## How password storage works

Creating a user turns the plaintext password into a versioned
PBKDF2-HMAC-SHA256 verifier:

```text
pbkdf2-sha256$v=1$i=600000$<random-salt>$<derived-key>
```

The production work factor is 600,000 iterations. Every hash receives a fresh
random 16-byte salt and produces a 32-byte derived key. A salt is not a secret;
its purpose is to ensure that equal passwords do not normally have equal stored
values and that precomputed password tables cannot be reused across accounts.

Verification accepts only the exact supported version, work factor, component
sizes, and canonical unpadded URL-safe Base64 spelling. Go derives the candidate
key and compares equal-length key bytes in constant time. PostgreSQL receives
only the encoded verifier, never the plaintext password.

PBKDF2 makes each guess costly but cannot rescue a weak or reused password. The
password policy, password manager, database access controls, TLS, and login
throttling are complementary protections rather than substitutes for one
another.

The policy follows the current [NIST SP 800-63B password guidance](https://pages.nist.gov/800-63-4/sp800-63b.html),
the work-factor choice follows the [OWASP password-storage guidance](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html),
and the implementation uses Go's standard [`crypto/pbkdf2` package](https://pkg.go.dev/crypto/pbkdf2).

## Sessions, CSRF tokens, and cookies

On successful login, the application generates two independent 32-byte random
values:

1. The session bearer token proves possession of the browser session.
2. The CSRF token proves that an authenticated state-changing form came from
   the same browser context.

The browser receives the raw values in separate cookies. PostgreSQL stores only
their SHA-256 hashes. A database disclosure therefore does not directly expose
a usable bearer or CSRF token. Authentication hashes the presented cookie and
loads only a session that is unrevoked, unexpired, and attached to an active
user. The returned hashes are compared in constant time before the request is
accepted.

Sessions have an absolute eight-hour lifetime. Activity does not slide or
extend that deadline. Logout sets `revoked_at` instead of erasing the row, then
expires both cookies. Disabling a user also makes every associated session
unusable on its next lookup.

The anonymous login form has its own short-lived, ten-minute CSRF cookie and
hidden form value. Authenticated logout requires the independent session-bound
CSRF value. Forms are size-bounded, accept only their exact expected fields,
and reject unsupported content types or duplicated values.

Administrator cookies are host-only because no `Domain` is set. They use
`HttpOnly`, `SameSite=Strict`, bounded `Expires`/`Max-Age`, and narrow paths:
the login CSRF cookie is scoped to `/admin/login`, while session cookies are
scoped to `/admin`. JavaScript is not required to read them.

The session and request-forgery boundaries follow the concepts in OWASP's
[Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
and [Cross-Site Request Forgery Prevention](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)
guides. Those references explain the broader threat model; this document
describes the exact choices implemented by Zafarmand.

All `/admin` responses, including router-generated errors, receive a private
security-header policy:

- `Cache-Control: no-store` keeps authenticated pages out of browser caches.
- Content Security Policy allows same-origin styles and forms while denying
  all other default resources, framing, and base-URL changes.
- `X-Frame-Options: DENY` and CSP `frame-ancestors 'none'` resist clickjacking.
- `Referrer-Policy: no-referrer` avoids sending admin paths as referrers.
- `X-Content-Type-Options: nosniff`, same-origin opener/resource policies, and
  a restrictive permissions policy reduce unrelated browser capabilities.
- `X-Robots-Tag` complements the template's robots metadata for private routes.

## Local HTTP and production HTTPS

Local development uses `http://localhost:8080`. The current cookie code sets
the `Secure` flag when Go sees a TLS request (`r.TLS != nil`), so cookies are
not marked Secure during this deliberate local HTTP workflow.

Public or shared deployment must use HTTPS. If Go terminates TLS itself, it can
observe TLS directly and mark the cookies Secure. A TLS-terminating reverse
proxy is more subtle: the backend may receive plain HTTP even though the browser
used HTTPS. The application does **not** currently trust or interpret
`X-Forwarded-Proto`, because accepting that header from arbitrary clients would
let them spoof connection security.

Before deploying behind a proxy, establish and review an explicit trusted-proxy
boundary. The proxy must replace forwarding headers, accept traffic only from
the intended network path, and preserve the Secure-cookie guarantee through a
reviewed application or edge configuration. Do not expose the admin routes
publicly while the backend incorrectly believes production requests are HTTP.
End-to-end TLS between browser, proxy, and Go is the simplest interpretation.
The Go server also does not emit HTTP Strict Transport Security during this
local-HTTP stage. A production HTTPS edge should add HSTS only after the domain,
subdomains, certificate renewal, and rollback plan have been verified.

Stage 15 also has no distributed login rate limiter. Before any public
exposure, configure throttling at a trusted deployment edge such as the reverse
proxy, load balancer, or managed web-application firewall. It must apply
consistently across every application instance and must derive client identity
only from forwarding data supplied by that trusted edge. PBKDF2 is intentionally
expensive and is not a rate limiter; without edge throttling, repeated login
attempts can consume CPU as well as guess passwords.

## Database roles and least privilege

Do not run the web server with the schema owner used for migrations. Even
though every query is parameterized, a compromised runtime process should not
be able to alter tables, edit migration history, or grant itself privileges.

Use separate credentials for these responsibilities:

- The migration role owns or can alter the schema, migration ledger, tables,
  constraints, indexes, and sequences. Use it only for explicit migration
  commands.
- A short-lived bootstrap role can insert `admin_users` rows and use the
  generated-ID sequence when an operator runs `admin create-user`. It does not
  need schema-alteration rights. Remove or disable this access when bootstrap
  is finished.
- The server runtime role needs `INSERT` and `SELECT` on inquiries: Stage 14
  inserts and checks an idempotent replay, while Stage 16 reads protected list
  and detail fields. It also needs read access to active admin users (including
  the verifier needed for login) and narrow insert/select/update access for
  admin sessions. Logout updates `revoked_at`; it does not require table
  deletion. The server does not need permission to create admin users or modify
  their roles.

The CLI and server both read a variable named `DATABASE_URL`, but separate
processes can and should supply different role-specific URLs. Define grants in
deployment automation or provider configuration, not in application source or
this repository. Test the exact grants in a non-production environment before
deployment.

## Manual verification without exposing secrets

After migration 3 is applied and a placeholder test administrator has been
created locally, start the server in the same process environment that contains
the runtime `DATABASE_URL`:

```powershell
go run .
```

Then verify in a private browser window:

1. Open `http://localhost:8080/admin`. It should redirect to `/admin/login`.
2. Submit a deliberately wrong password. The page should show one generic
   authentication failure, without confirming whether the address exists.
3. Sign in with the local test account. The browser should reach `/admin`,
   display the expected role label, and offer the read-only Inquiries link.
4. In browser developer tools, inspect cookie **names and attributes only**.
   Confirm `HttpOnly`, `SameSite=Strict`, expiry, and paths; do not copy, log,
   screenshot, or paste cookie values. `Secure` is expected to be absent only
   for the local HTTP case described above.
5. Inspect the `/admin` response headers and confirm the no-store, CSP,
   anti-framing, referrer, MIME, opener/resource, and permissions policies.
6. Use the page's logout button. It should POST, redirect to `/admin/login`, and
   a later `/admin` request should remain unauthenticated. Browser Back must not
   restore a usable cached dashboard.

If manual database inspection is useful, keep the connection string out of the
`psql` argument list by copying the already-present process variable:

```powershell
$env:PGDATABASE = $env:DATABASE_URL
psql -X --set ON_ERROR_STOP=on --command 'SELECT role, active, COUNT(*) FROM admin_users GROUP BY role, active ORDER BY role, active;'
psql -X --set ON_ERROR_STOP=on --command 'SELECT revoked_at IS NOT NULL AS revoked, expires_at > CURRENT_TIMESTAMP AS unexpired, COUNT(*) FROM admin_sessions GROUP BY revoked, unexpired ORDER BY revoked, unexpired;'
Remove-Item Env:PGDATABASE -ErrorAction SilentlyContinue
```

These aggregate checks do not print email addresses, password verifiers, or
token hashes. Never select those columns into terminal logs merely to prove
that rows exist. Remove `DATABASE_URL` when the server stops if the shell is no
longer being used:

```powershell
Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:ZAFARMAND_ADMIN_PASSWORD -ErrorAction SilentlyContinue
```

The eight-hour expiry path is covered deterministically by automated tests. Do
not weaken the configured lifetime or edit valuable database rows simply to
make a manual check faster.

Use only a fictional Contact row when verifying the inquiry link, and follow
the data-minimizing steps in [admin-inquiries.md](admin-inquiries.md). Do not
copy a real visitor's personal information merely to prove that access works.

## Automated verification

The normal suite is database-independent and uses injected low-cost password
work factors and in-memory repository doubles:

```powershell
go fmt ./...
go test -count=1 ./...
go vet ./...
```

It covers password policy and canonical encoding, exact email normalization,
SQL and parameter ordering, safe error mapping, login neutrality, session and
CSRF handling, cookie attributes, security headers, route methods, dashboard
protection, explicit owner/editor inquiry authorization, read-only inquiry
routes, logout, and expiry. Production continues to use the fixed 600,000
iteration manager; only tests can inject the inexpensive manager.

The PostgreSQL tests are destructive and opt-in. Supply only a dedicated empty
database whose name ends in `_test`; never use a development, shared, or
production database. The test checks its separate URL, literal confirmation,
server-reported database name, and reserved schema relations before changing
anything:

```powershell
Set-SecretProcessVariable `
    -Name 'ZAFARMAND_TEST_DATABASE_URL' `
    -Prompt 'Enter the disposable PostgreSQL test URL'
$env:ZAFARMAND_TEST_DATABASE_CONFIRM = 'stage13-disposable-database'

go test -count=1 -run 'Postgres' ./...

Remove-Item Env:ZAFARMAND_TEST_DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:ZAFARMAND_TEST_DATABASE_CONFIRM -ErrorAction SilentlyContinue
```

`Set-SecretProcessVariable` is the history-safe helper defined earlier in this
guide. The live suite covers the complete v1-to-v3 migration cycle, rollback
and reapplication, real PostgreSQL constraints, duplicate normalized email,
session byte mapping, active-user filtering, expiry, revocation, and the Stage
16 inquiry list/detail reader. Stage 16 adds no fourth migration. The suite
never falls back to `DATABASE_URL` and skips only when its explicit opt-in
variables are absent. Ensure cleanup succeeds before reusing or removing the
disposable database.
