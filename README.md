# Everlore

**Every story deserves a place in the archive.**

Everlore is an independent, community-oriented fiction archive for fanfiction, original fiction, and transformative works. It favors public reading, creator control, and reader-directed discovery over algorithmic feeds.

> **Project status:** early development. The application is not yet ready for a public production launch. See [Current limitations](#current-limitations) before inviting users.

## What works today

- Public work browsing and keyword, fandom, tag, rating, and completion filters, with shareable search URLs.
- Single-chapter work publishing, reading, and owner-only editing.
- Accounts, public profiles, multiple publishing pseudonyms, email verification, and password recovery.
- Personal bookmarks, likes, and comments.
- Reading theme and text-size controls.
- Report submission and administrator review, moderation actions, and audit log through the API.

See [API.md](API.md) for account and moderation endpoint details. The interface is server-rendered; Go, SQLite, and a small amount of JavaScript power the application.

## Run locally

Requirements: Go 1.27 or newer.

```sh
go run .
```

Open [http://localhost:8080](http://localhost:8080). The server creates `everlore.db` in its working directory. Set `PORT` to use another port. To populate a development database with illustrative works, start the server with `EVERLORE_SEED=1`; existing seed data is not removed automatically.

```sh
go test ./...
go vet ./...
```

## Email configuration

Registration and password recovery require an SMTP service that supports STARTTLS. Configure these environment variables in the server environment; never commit credentials:

| Variable | Purpose |
| --- | --- |
| `EVERLORE_BASE_URL` | Public HTTPS origin used in verification and reset links. |
| `EVERLORE_SMTP_ADDR` | SMTP host and port, for example `smtp.example.com:587`. |
| `EVERLORE_SMTP_FROM` | Verified sender address. |
| `EVERLORE_SMTP_USER` | SMTP username, if required. |
| `EVERLORE_SMTP_PASSWORD` | SMTP password, if required. |

If delivery fails, the account remains unverified and can request another verification email through the API. Accounts created before email support do not have recovery addresses.

## Administration and deployment

To create the first administrator, verify an account, then start the server once with `EVERLORE_BOOTSTRAP_ADMIN` set to its username. Remove the variable afterward. Reports, account and work deletion, and moderation actions are currently API-only.

Set `EVERLORE_SECURE_COOKIES=1` behind an HTTPS reverse proxy and block direct HTTP access to the application. Rate limits use the connecting IP and SQLite; forwarded IP headers are not trusted, so visitors behind one proxy share its limit. SQLite is intended for a single host. Use SQLite's online backup facilities, include WAL state, and test restoration.

Configure HTTPS, persistent storage, mail delivery, monitoring, backups, and moderation access before accepting users. A security review and deployment validation remain necessary.

## Current limitations

- Multi-chapter publishing, drafts, subscriptions, collections, series, and notifications are not implemented.
- Search and tags are basic; canonical tags, aliases, full-text indexing, and several advanced filters are still needed.
- Reports, deletion, and moderation need user-facing screens; email address changes and migration of legacy accounts are unfinished.
- Production deployment, operational recovery, and security hardening are incomplete. Do not treat this implementation as a security audit.

## Project notes

[API reference](API.md) · [Product direction](PRODUCT.md) · [Interface direction](DESIGN.md)
