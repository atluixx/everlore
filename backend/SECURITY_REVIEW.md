# Security review — November 3, 2026

This review covered Everlore's Go handlers, authentication and recovery flows,
authorization boundaries, database calls, server-rendered templates, request
limits, cookies, and production response headers. It also included the Go
vulnerability scanner and local regression tests. It was a focused application
review, not an infrastructure penetration test.

## Fixed findings

- **Cross-site request hardening:** state-changing requests now verify `Origin`
  when browsers provide it, in addition to the existing CSRF cookie and form
  token. This closes the sibling-subdomain cookie injection case.
- **Sensitive response caching:** every rendered HTML response now uses
  `Cache-Control: no-store`, because rendered pages contain a browser-specific
  CSRF token. The deployed version previously returned a public cache policy.
- **Password-reset resource exhaustion:** invalid reset tokens are rejected
  before bcrypt runs, preventing cheap unauthenticated requests from forcing
  expensive password hashes.
- **Browser exploit containment:** responses now set a restrictive Content
  Security Policy and Permissions Policy. Inline event handlers were removed so
  the policy does not need `unsafe-inline` script execution.
- **Session handling:** session deletion now preserves the session cookie's
  `HttpOnly`, `Secure`, and `SameSite` protections, and login handles random
  number generator failures instead of creating a predictable token.

## Checks with no confirmed vulnerability

- SQL values are parameterized; the remaining dynamic search fragments come
  from fixed server-side choices.
- Story, chapter, draft, comment, account, and moderation mutations enforce
  ownership or administrator authorization.
- User-authored story and profile content is rendered through `html/template`;
  no stored-XSS path was found.
- Passwords use bcrypt, session and email tokens use cryptographic randomness,
  and only token hashes are stored.
- The Go vulnerability scan found no known reachable dependency vulnerability.
- No credential was found in tracked files or Git history by the pattern scan.

## Remaining launch risks

- Rate limits focus on authentication and guest publishing. Per-account limits
  or quotas are still needed for comments, reports, stories, and storage abuse.
- Administrator accounts have no second authentication factor or step-up check.
- Security-event monitoring and alerting are not implemented.
- Backup restoration and incident-response procedures still need to be tested.
- The fixes in this review do not protect production until they are deployed.

Run `go test ./...`, `go vet ./...`, and `govulncheck ./...` before releases.
