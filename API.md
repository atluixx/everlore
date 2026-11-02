# API example

Use HTTPS and a cookie jar. The API returns JSON. The following is illustrative; never put real passwords in shell history.

1. GET `/api/csrf` and retain both the cookie and returned `csrf` value.
2. POST `/api/account/register` with `csrf`, `username`, `email`, and `password` as form fields.
3. Open the emailed verification link and submit its confirmation form.
4. GET `/login`, then POST `username`, `password`, and `csrf` using the same cookie jar.
5. Authenticated state-changing requests need the session cookie and `csrf` value.

A token is single-use and expires after 24 hours for verification or one hour for recovery. Recovery responses do not reveal whether an email is registered. An unsuccessful email delivery after registration leaves an unverified account; use `/api/account/verification` to request a new link.
