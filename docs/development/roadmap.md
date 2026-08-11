# Zafarmand working roadmap

The current working plan contains 26 stages. This is a deliberate project plan,
not a database-version count: several application stages reuse an existing
schema, and homepage Stage 4 was delivered through smaller 4A-4C checkpoints.

The plan can still change when a real interface or data requirement shows that
a safer split is needed. Any change should be recorded here before implementation
so a stage number never silently changes meaning.

## Completed foundation and public interface

| Stage | Outcome | Status |
| --- | --- | --- |
| 1 | Go server, routes, templates, and static-asset structure | Complete |
| 2 | Responsive design tokens and reusable CSS foundation | Complete |
| 3 | Responsive header, navigation, drawer, and keyboard behavior | Complete |
| 4 | Homepage foundation, discipline entrances, and supporting composition | Complete |
| 5 | Shared discipline landing-page system | Complete |
| 6 | Data-driven Product catalogue foundation | Complete |
| 7 | Server-rendered Product detail routes | Complete |
| 8 | Data-driven Interior Design portfolio | Complete |
| 9 | Server-rendered Interior Design project details | Complete |
| 10 | Data-driven Architecture portfolio | Complete |
| 11 | Server-rendered Architecture project details | Complete |
| 12 | Secure Contact inquiry preview and submission flow | Complete |

## Completed persistence and administration foundation

| Stage | Outcome | Status |
| --- | --- | --- |
| 13 | PostgreSQL migration runner and guarded database workflow | Complete |
| 14 | Idempotent Contact inquiry persistence | Complete |
| 15 | Administrator authentication, sessions, dashboard, and logout | Complete |
| 16 | Protected read-only inquiry inbox and detail | Complete |
| 17 | Explicit inquiry status workflow | Complete |
| 18 | PostgreSQL-backed published Product catalogue and public detail | Complete |
| 19 | Protected read-only all-state Product catalogue and detail | Complete |
| 20 | Protected Product create/edit/publication workflow with validation and stale-edit protection | Complete |
| 21 | Rich Product content and reviewed single-cover-image management | Complete |

## Remaining focused stages

| Stage | Planned outcome |
| --- | --- |
| 22 | PostgreSQL-backed Interior projects and protected management workflow |
| 23 | PostgreSQL-backed Architecture projects and protected management workflow |
| 24 | Homepage, Contact information, featured-content, and SEO management |
| 25 | Security, privacy, retention, observability, backup, and operational hardening |
| 26 | End-to-end acceptance, accessibility/performance review, and deployment |

Later stages must remain vertical. Stage 21 deliberately stops at one reviewed
Product cover instead of pretending to solve galleries, cropping, and storage
operations in the same change. Stages 22-23 should reuse the completed Product
read/write, accessibility, media-validation, and concurrency boundaries rather
than generating two large administration systems at once.
