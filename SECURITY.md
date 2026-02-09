# Security

One Time Note is a public, anonymous one-time note service.
It has no accounts, cookies, sessions, admin area, or app-level authentication.

The core model is that the browser owns plaintext.
The browser encrypts notes before upload, keeps the AES key in the link fragment,
and decrypts the payload after a successful open.
The server stores encrypted payloads, note metadata, burn-token verifiers, used-ticket markers,
schema version, and the root key in `data.db`.

The server must never store or log plaintext, AES keys, URL fragments, or raw burn tokens.
The path note ID is only a locator; knowing `/note/{id}` must never be enough to open or destroy a note.

Because the server delivers the JavaScript that handles encryption,
a malicious server operator is outside the confidentiality model.
So are compromised browsers, devices, and browser extensions.
A stolen database should not reveal note plaintext,
but it is still sensitive because it contains metadata and the root key.

## Notes And Links

Generated note links put secret material in the URL fragment, which browsers do not send in HTTP requests.
The server receives the note ID in the path and receives the burn token only when the browser opens the note.

On the open page, the browser reads the fragment into memory and clears it from the address bar.
The server then verifies the burn token and burns the note before returning the encrypted payload.
Expired, missing, already-opened, and invalid-token opens should all fail without revealing which case occurred.

One-time semantics favor destruction over availability.
If the browser crashes, the network fails, or decryption fails after a valid open, the note is still gone.
Do not add auto-copying, persistent browser storage, plaintext server handling,
or alternate open flows without revisiting this model.

## Production Deployment

Production runs behind a trusted TLS-terminating reverse proxy.
The app itself serves HTTP and validates forwarded HTTPS metadata before normal request handling.

The proxy must terminate TLS, strip or overwrite client-supplied `Forwarded` and `X-Forwarded-*` headers,
pass the public host and original client IP chain, and send `https` as the forwarded scheme.
It should keep request-body logging disabled and enforce body-size, connection,
or concurrency limits appropriate for the deployment.

Set `NOTE_PUBLIC_ORIGIN` when the deployment has one canonical public origin.
Set `NOTE_TRUSTED_PROXIES` when the proxy connects from outside the default private, loopback, link-local,
and unique-local ranges.
Pin deployed images to full release tags instead of mutable tags.

`go run . --dev` is for local development only.
Development mode loads `.env` and allows plain HTTP; do not use it in containers or production.

## State And Recovery

`data.db` is secret-bearing disposable runtime state, not durable user data.
Protect the file and its parent directory, run only one app process per database,
and do not routinely back it up.

If `data.db` leaks, stop the service and wipe or replace the database.
Continuing with a leaked root key is out of scope.
If an operator manually backs up the database anyway, treat the backup as secret-bearing;
restoring it can resurrect stale note state.

## Logging

Logs should be useful without carrying secrets.
Use route patterns, status codes, coarse reason categories, normalized client rate-limit keys,
and coarse size buckets for explicit abuse or storage events.

Do not log raw request paths, queries, note IDs, tickets, burn tokens, AES keys, URL fragments,
request bodies, plaintext, full forwarded chains, or exact normal payload sizes.

## Abuse And Health

The app is anonymous and public, so abuse controls must stay deterministic and local:
shared app rate limiting through `NOTE_RATE_LIMIT`, encrypted payload limits through `NOTE_MAX_NOTE_SIZE`,
database file size limits through `NOTE_MAX_DB_SIZE`,
and proxy-side request or connection limits when needed.

`GET /healthz` is process liveness only.
It must not expose database state, dependency details, or other operational internals.

## Browser And HTTP Controls

Browser execution should stay same-origin and deny-by-default.
Do not add third-party scripts, analytics, fonts, CDNs, telemetry, inline scripts,
or HTML injection sinks without deliberately revisiting CSP and Trusted Types.
Dynamic HTML and note API responses should keep `Cache-Control: no-store`,
and browser code should write untrusted text through `textContent`, textarea values,
or equivalent non-HTML sinks.

Production responses should keep the existing security headers,
including HSTS after trusted HTTPS validation, a same-origin CSP, `X-Content-Type-Options: nosniff`,
`Referrer-Policy: no-referrer`, frame denial,
and deny-by-default permissions for unused browser features.

## Changing Security-Sensitive Code

Re-check this document before changing request handling, proxy validation, note lifecycle,
browser crypto or link handling, storage, logging, deployment defaults, dependencies, or verification gates.

Focused regression tests are expected for changes that affect storage, configuration, handlers,
proxy behavior, note opening, or frontend crypto helpers.
Before a pull request, run the Go, frontend unit, and browser tests listed in `README.md`.
