# One Time Note

One Time Note is a small public service for sharing a note that can be opened once.
The browser encrypts the note before upload, and the shared link carries the browser-only secret
in the URL fragment.

The service is intentionally anonymous.
There are no accounts, sessions, cookies, admin UI, or app-level authentication.

## How It Works

1. The browser asks the server for a one-use creation ticket and burn token.
2. The browser encrypts the plaintext with AES-GCM and uploads only the encrypted payload.
3. The shared link contains the note path plus a secret fragment.
   The AES key stays in the fragment and is never sent to the server.
4. When the note is opened, the browser clears the fragment from the address bar.
5. The server verifies the burn token, burns the note, and returns the encrypted payload for browser-side decryption.

The path note ID is only a locator.
Knowing `/note/{id}` alone cannot open or destroy a note.

API clients can read current server-enforced limits from `GET /api/config`.
See [SECURITY.md](SECURITY.md) for the security model and operational boundaries.

## Local Development

```sh
cp .env.example .env
go run . --dev
```

Open `http://127.0.0.1:8080`.

Development mode is explicit.
Plain `go run .` starts in production mode and will fail unless production-required settings are present.

Useful commands:

```sh
go test ./...
npm install
npm test
npm run test:browser
```

The Playwright browser tests start the app on `127.0.0.1:18080`
and use `/tmp/one-time-note-playwright.db`.

## Production Deployment

Production must run behind a trusted TLS-terminating reverse proxy.
The app serves HTTP, validates trusted forwarded HTTPS metadata,
and uses the forwarded public host to generate browser links.

The checked-in Docker Compose setup runs the app behind Caddy:

```sh
docker compose -f deploy/docker-compose.yml pull
docker compose -f deploy/docker-compose.yml up -d
```

Configure the public hostname in `deploy/caddy/Caddyfile`.
For production, pin the image to a full release tag such as `ghcr.io/stkom/one-time-note:1.0.0`
instead of relying on a mutable tag.

The reverse proxy should terminate TLS, strip or overwrite client-supplied forwarding headers,
pass the public host and client IP chain, avoid request-body logging,
and enforce request or connection limits appropriate for the deployment.

Use `NOTE_PUBLIC_ORIGIN` when the deployment has one canonical public origin.
Use `NOTE_TRUSTED_PROXIES` only when the proxy connects from outside the default private, loopback,
link-local, and unique-local ranges.

## Configuration

Production defaults are strict.
`.env` is loaded only by `go run . --dev`; production reads the process environment.

Common settings:

- `NOTE_ENVIRONMENT` defaults to `production`.
  Use `development` only with `go run . --dev`.
- `NOTE_HOST` and `NOTE_PORT` choose the HTTP listen address.
  Containers normally use `NOTE_HOST=0.0.0.0`.
- `NOTE_DB_PATH` chooses the bbolt database path.
  The container writes to `/data/data.db`.
- `NOTE_DISPLAY_NAME` and `NOTE_FOOTER_TEXT` customize plain-text UI labels.
- `NOTE_PRIVACY_URL`, `NOTE_TERMS_URL`, and `NOTE_LEGAL_NOTICE_URL` add legal links below the note dialog.
- `NOTE_HIDE_GITHUB_LINK=true` hides the small source link in the UI.
- `NOTE_PUBLIC_ORIGIN` pins a canonical `https://` public origin.
- `NOTE_TRUSTED_PROXIES` sets the trusted proxy source IPs or CIDRs.
- `NOTE_HEALTH_CHECK_SOURCES` allows additional sources to call `/healthz`.
- `NOTE_MAX_NOTE_SIZE`, `NOTE_MAX_DB_SIZE`, and `NOTE_RATE_LIMIT` tune local abuse controls.
- `NOTE_GRACE_PERIOD` and `NOTE_CLEANUP_INTERVAL` tune shutdown and cleanup behavior.

Byte-size values accept suffixes such as `MiB` and `GiB`.
Rate-limit windows accept Go-style durations such as `1m` and `1h`.

## State And Recovery

All application state lives in `data.db`:
encrypted payloads, note metadata, used-ticket markers, schema version, and the root key.

Treat the database as secret-bearing disposable runtime state, not durable user data.
Do not routinely back it up.
If `data.db` leaks, stop the service and wipe or replace the database.

The app supports one process per database.
Horizontal scaling would require different storage and shared abuse-control design.
