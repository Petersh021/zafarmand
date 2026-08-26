# Stage 26 acceptance and deployment record

Stage 26 consolidates end-to-end acceptance, accessibility and performance
review, and production deployment. This document separates repository evidence
from decisions that only an authorized operator or business owner can make.
Passing the repository gates makes a commit **release-ready**; it does not mean
that an unspecified public environment has been deployed.

## Completion rule

Stage 26 may be marked complete only when all of the following are true:

1. the exact release commit passes the required GitHub CI workflow;
2. CI builds the production Linux binary and non-root container, applies all
   migrations to disposable PostgreSQL 18, starts the packaged artifact, and
   completes the full-stack smoke flow;
3. automated structural accessibility and performance budgets pass;
4. the manual browser matrix below is completed on the selected release target;
5. the target inventory and policy decisions below have named, non-secret
   values or authoritative external references; and
6. the deployed commit passes readiness and post-deployment smoke checks, with
   rollback and restore procedures available to the operator.

If the target or owner decisions are absent, record the candidate as
release-ready and leave Stage 26 in progress.

## Automated release gates

The tracked workflow is the authoritative automated gate. It must pass:

- module download and checksum verification;
- `gofmt` and `go vet`;
- race-enabled unit and PostgreSQL integration tests against a disposable
  PostgreSQL 18 service;
- the complete embedded migration lifecycle and the current Contact,
  readiness, and retention least-privilege smoke checks;
- known-vulnerability scanning;
- a trimmed, statically linked Linux/amd64 build;
- construction of the scratch-based, numeric-non-root runtime image containing
  its CA bundle, `templates/`, and `static/`; and
- container smoke checks for liveness, readiness, public pages, static assets,
  Contact persistence, administrator login/dashboard/logout, and graceful
  cleanup.

The targetless repository workflow deliberately does not publish to a registry.
It proves a release commit and an ephemeral Linux/amd64 image, not an artifact
promotion chain. Once a target is approved, its release workflow must publish
the exact smoke-tested image (or repeat the full smoke against the published
image), record the immutable digest and CA-bundle provenance, and deploy that
digest without rebuilding it.

Checked-in source budgets cap each public page's shared CSS/JavaScript plus its
page stylesheet at 80 KiB, the admin stylesheet at 48 KiB, the fallback hero at
256 KiB, and each representative server-rendered document at 96 KiB. These are
uncompressed upper bounds. The HTTPS edge must still enable Brotli or gzip and
must preserve the application's security and cache headers.

These source budgets are regression tripwires, not production transfer budgets.
They do not bound the number of published catalogue records or managed-image
bytes in a content-populated page. Target Lighthouse measurements, approved
catalogue size, and optimized production media therefore remain mandatory
release evidence rather than being inferred from a green unit test.

Unversioned checked-in assets use explicit `Last-Modified` revalidation rather
than an unsafe immutable lifetime. Managed database media retains revisioned
ETags and publication checks. Content editors should upload appropriately sized,
web-optimized images; the application does not currently generate responsive
variants automatically.

## Manual browser acceptance matrix

Run this matrix on the exact deployed commit with synthetic records. Do not use
real inquiry or administrator data in screenshots, recordings, or reports.

| Area | Required checks | Result/evidence |
| --- | --- | --- |
| Responsive layout | 320, 375, 430, 768, 1024, 1440, and 1920 CSS-pixel widths; portrait and short landscape; no horizontal document overflow | Pending target |
| Text reflow | Browser text-only zoom and full-page zoom at 200%; reflow at 400%/320 CSS pixels without lost content or two-dimensional reading | Pending target |
| Keyboard | Skip links, visible focus, logical order, drawer open/trap/Escape/restore, every public form, admin navigation, login, edit, and logout | Pending target |
| JavaScript unavailable | With scripting disabled and with `main.js` blocked, native fallback navigation still reaches every discipline and Contact; server-rendered forms and links remain usable | Pending target |
| User preferences | Reduced motion, increased/forced contrast, and platform high-contrast mode preserve content and focus indicators | Pending target |
| Semantics | One useful page title and `h1`, landmark names, labels/instructions/errors, meaningful image alternatives, valid referenced IDs, and no empty actionable controls | Pending target |
| Public workflow | Home, all catalogues/details, Contact validation/success/replay, unknown routes, and method boundaries | Pending target |
| Protected workflow | With the exact target runtime role: login failure/success, dashboard, inquiry status, Product/Interior/Architecture create-edit-cover-publish/archive, Site-content changes, stale edits, and logout | Pending target |
| Browser audit | Current axe or equivalent WCAG 2.2 AA rules: zero serious or critical violations; reviewed exceptions recorded with owner and deadline | Pending target |
| Performance audit | Mobile and desktop Lighthouse (or equivalent) on Home, one catalogue/detail, Contact, and login; record LCP, CLS, INP/TBT, transferred bytes, and test conditions | Pending target |

This matrix is an acceptance review, not a blanket claim of formal WCAG
conformance. Any exception needs a concrete issue, owner, risk decision, and
deadline before launch approval.

## Deployment target inventory

Record identifiers and policy references, never credentials:

| Decision | Required value | Current status |
| --- | --- | --- |
| Hosting platform and runtime | Provider/service, OS or container runtime, instance count | Not supplied |
| Public origin | Canonical HTTPS hostname and DNS owner | Not supplied |
| Region and data residency | Application, database, backup, and log regions | Not supplied |
| HTTPS edge | Certificate owner, HTTP redirect, private upstream, forwarding-header replacement, compression and rate limits | Not supplied |
| PostgreSQL | Version, direct migration endpoint, least-privilege runtime role, maintenance role, backup role, connection limits | Not supplied |
| Secret management | Approved store, rotation owner, and emergency-revocation procedure | Not supplied |
| Monitoring | Readiness/liveness probes, alert routes, on-call owner, and log retention | Not supplied |
| Privacy retention | Approved inquiry/session periods and policy owner/reference | Not supplied |
| Recovery | RPO, RTO, encrypted backup retention, restore owner, and last successful restore rehearsal | Not supplied |
| Production content | Approved Homepage/Contact copy, catalogue records, optimized media, administrator owner, and any external profile URLs | Not supplied |

Use one application instance initially unless the Contact success-receipt signing
design is made multi-instance-safe. The current process-local signing key is
deliberately not persisted; a redirect routed to another instance can therefore
lose only its success banner, not the already committed inquiry.

## Release sequence

1. Identify the exact signed/reviewed commit and preserve its successful CI run.
2. Complete the target and policy inventory above.
3. Verify a current encrypted backup and its checksum; confirm the last restore
   rehearsal satisfies the approved RPO/RTO.
4. Build the release artifact from the exact commit. Record the immutable image
   digest produced by the chosen registry.
5. Run `migrate status`, `migrate up`, and status again through the exact
   release image with the schema-owner URL over the direct/session-pooled
   endpoint. `go run . migrate ...` is the equivalent developer workflow, not
   the immutable production release path.
6. Replace the migration credential with the least-privilege runtime URL. Set
   exact TLS, listener, and reviewed-edge values from the operations runbook.
7. Start the candidate without public traffic. Wait for `/health/ready`.
8. Run the browser matrix and representative public/protected smoke flow with
   synthetic data. Verify privacy-safe structured logs and alerts.
9. Admit traffic gradually at the HTTPS edge and repeat readiness and public
   smoke checks from outside the private network.
10. Record deployment time, commit, image digest, migration version, evidence
    links, approvers, and any accepted exceptions in the authorized operations
    system—not in this repository if that record contains sensitive data.

## Rollback and recovery

Prefer routing traffic back to the last compatible immutable application image.
Do not automatically run `migrate down`: migration 10 rollback removes replay
tombstones, and earlier reversals delete managed data. If a schema rollback is
unavoidable, stop traffic, take and verify a fresh backup, obtain explicit
approval, and follow the migration and retention runbooks.

Keep the candidate out of service whenever readiness fails. A data-loss or
credential event uses the recovery and secret-revocation procedures rather than
an application-only rollback.
