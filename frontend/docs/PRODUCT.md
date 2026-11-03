# Everlore

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Everlore primarily serves readers looking for fiction through deliberate search,
metadata, and filtering. Readers may browse and read public stories without an
account. Accounts add bookmarks, comments, likes, subscriptions, and
notifications.

Creators are the second core audience. They publish fiction under an account,
multiple pseudonyms, or a confirmed guest identity, and retain control over
their stories, chapters, metadata, drafts, and publishing identity.

Moderators maintain the archive through reports, consistent enforcement, tag
management as that system matures, and auditable administrative actions.

## Product Purpose

Everlore is a community fiction archive for fanfiction, original fiction, and
transformative works. Its primary job is to help readers find stories they
actively want and read them without distraction. It also gives creators a
durable place to publish and manage work without surrendering control to a feed
or popularity-ranking system.

Success means that a reader can move from an interest or exclusion preference
to a relevant story through understandable metadata, while a creator can
publish unconventional or niche work with clear classification and ownership.

## Positioning

Everlore is archive-first. Discovery comes from reader-selected searches,
filters, tags, fandoms, relationships, characters, ratings, and mandatory
notices rather than personalized recommendations. It is not a social feed and
does not optimize visibility around opaque engagement ranking.

The archive combines public access, extensive creator control, custom tagging,
multiple publishing identities, and transparent content classification. Its
community features support the archive instead of replacing it.

## Operating Context

- Public visitors browse, search, filter, open metadata, and read stories
  without registering.
- Readers use shareable search URLs and visible ratings and notices to decide
  what to open or exclude.
- Signed-in readers save stories, respond to creators, and subscribe to future
  updates.
- Creators write or paste story text, assign metadata and mandatory notices,
  save private drafts, publish chapters, and maintain existing stories.
- A guest may submit one chapter after email confirmation, then claim that
  story through a verified account using the same email.
- Moderators review private reports and record administrative actions in an
  audit trail.
- The production service runs at `everlore.online`; the application is a
  server-rendered Go service backed by Turso/libSQL and deployed on Vercel.

## Capabilities and Constraints

Current capabilities include public reading and browsing, shareable search
filters, verified accounts, password recovery, profiles and pseudonyms, story
and chapter publishing, manual drafts, guest submissions, bookmarks, likes,
comments, subscriptions, in-app notifications, reports, moderation, and basic
reading controls.

The product uses **story** in reader-facing language. Tags and fandom names are
author-supplied rather than restricted to a closed vocabulary. Mandatory notices
are Sexual content, Non-con, Dub-con, Incest, Underage sex, Graphic violence,
and None apply.

The archive must remain usable without advertising, algorithmic personalization,
or an account for public reading. Ratings and mandatory notices must be visible
before a reader opens a story. User actions and published content must persist;
controls must not imply functionality that does not exist.

Canonical tags and aliases, richer advanced search, collections, series,
reading history, export, coauthors, visibility controls, autosave, email
notifications, and stronger operational tooling remain planned rather than
current product claims.

## Brand Commitments

The product name is **Everlore**. Its principle is **“Every story deserves a
place in the archive.”** Everlore may take product inspiration from Archive of
Our Own, but must retain its own name, logo, terminology, and interface identity.

The established voice is direct, literary, welcoming, and plain about archive
rules. It should describe real archive behavior without promotional hype or
invented social proof. The supplied Everlore mark is the canonical logo asset at
`frontend/static/everlore.svg`.

## Evidence on Hand

- A deployed, functional archive at `https://everlore.online`.
- The canonical logo at `frontend/static/everlore.svg`.
- Working server-rendered templates and realistic development seed stories.
- Product, API, security, and interface documentation in the repository.
- No verified testimonials, readership metrics, partner claims, press coverage,
  or usage benchmarks are available; future surfaces must not fabricate them.

## Product Principles

1. Help readers discover stories through explicit choices they can understand
   and share.
2. Preserve creator ownership, publishing identity, metadata control, and the
   ability to revise or remove their stories.
3. Keep public reading open and comfortable, with classification visible before
   entry.
4. Treat niche and unconventional fiction as first-class archive material,
   subject to law and published safety policies.
5. Build community features around the archive without turning discovery into
   a popularity contest.

## Accessibility & Inclusion

Public stories must remain available without registration. Core navigation,
forms, filters, moderation controls, and reading tools must work with a keyboard,
retain visible focus, use understandable labels and errors, and adapt to narrow
screens. Reading controls must support comfortable long-form text and a dark
theme. Ratings and mandatory notices must be presented as useful reader choices,
not hidden behind engagement mechanics.
