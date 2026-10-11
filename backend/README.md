# backend (fazond)

Go API for fazon.nexus at `https://api.fazon.nexus`: magic-link accounts (no signup), threaded
comments with likes on every article, in-site notifications, and Kyōtsu trainer session history.
One binary, one SQLite file (`modernc.org/sqlite`, pure Go, so it cross-compiles with
`CGO_ENABLED=0`). The site itself stays static on GitHub Pages; `fazon.nexus` and
`api.fazon.nexus` are same-site, so the API's host-only `SameSite=Lax` cookie rides along on
`fetch(..., {credentials: "include"})`.

```
cmd/fazond         main: config (flags / FAZOND_* env), serve, CLI, hourly maintenance (comment-pages reload, purge, backup)
internal/api       handlers, session auth, CORS/CSRF, rate limits; api_test.go covers the spec
internal/db        open + pragmas, embedded migrations (PRAGMA user_version; 0001 init, 0002 avatars,
                   0003 track + bio, 0004 track 'anki' category), users/tokens, backup, purge
internal/week      Track week calendar (Sun 03:00 UTC weeks, Saturday keys)
internal/mail      resend (HTTP API), smtp (STARTTLS) and log mailers; RFC 5322 message builder
deploy/            systemd unit, env example, Caddy snippet
```

## API

All JSON; errors are `{"error":"msg"}` with 400/401/403/404/409/413/415/429/500. Times are RFC 3339 UTC.

| | |
|---|---|
| `POST /v1/auth/request` `{email,next}` | always `202 {"ok":true}`; emails `$FAZOND_SITE/account/verify/#t=TOKEN&next=PATH` if the account exists. Limits: 3/email/15 min, 10/IP/hour (silently dropped, logged) |
| `POST /v1/auth/verify` `{token}` | consumes the token (single use, 15 min) → `{"user":{…},"next":"/…"}` + `fz_session` cookie (90 days, sliding) |
| `POST /v1/auth/logout` | deletes the session, clears the cookie |
| `GET /v1/me` | `{"user":null}` or `{"user":{id,name,email,admin},"unread":N}` |
| `GET /v1/comments?page=/articles/x/` | `{"comments":[…]}` oldest first; deleted comments with live replies come back as tombstones |
| `POST /v1/comments` `{page,parent,body}` | 201 comment (login); 404 if `page` isn't in the site's comment-pages index, 503 if no index has loaded yet. A `title` field (older frontends) is ignored |
| `DELETE /v1/comments/{id}` | author or admin; soft delete (also drops its likes and notifications) |
| `POST`/`DELETE /v1/comments/{id}/like` | `{"likes":N,"liked":bool}`, idempotent |
| `GET /v1/notifications?limit=50&before=ID` | `{"notifications":[…],"unread":N}` newest first |
| `POST /v1/notifications/read` `{ids:[…]}` or `{all:true}` | `{"unread":N}` |
| `POST /v1/kyotsu/sessions` `{started,layout,advanced,answers}` | 201 `{"id":N}` |
| `GET /v1/kyotsu/sessions`, `GET /v1/kyotsu/sessions/{id}` | own sessions only (others' are 404) |
| `GET /v1/levels` | `{"levels":[0,5,20,…,49005]}`: XP needed for levels 1–100, `5·(L−1)²` |
| `GET /v1/users/{id}` | public profile: `{user,joined,comments,likes,recent:[…20]}` (no email) |
| `POST /v1/me/profile` `{name}` | change display name (1–40 characters) → `{"user":…}` |
| `POST /v1/me/avatar` | raw `image/jpeg` or `image/png` body ≤ 2 MB, ≤ 4096×4096 → centre-cropped 256×256 JPEG, metadata stripped |
| `DELETE /v1/me/avatar` | remove the picture → `{"user":…}` |
| `GET /v1/users/{id}/avatar?v=T` | the JPEG; immutable caching when `v` is current, else `no-cache` + ETag |
| `GET /v1/users/{id}/card` | hover card: `{user,bio,joined,comments,likes}`, `Cache-Control: private, max-age=60` |
| `GET /v1/track/config` | categories (in display order: `active`, `passive`, `anki`, `phonetic`, `output`, `soroban`), your sub-categories, `public`, `current_week` |
| `POST /v1/track/subcats` `{category,name}`; `POST`/`DELETE /v1/track/subcats/{id}` | sub-categories: rename/archive/reorder; delete only if unused (else 409) |
| `POST /v1/track/settings` `{public}` | show/hide yourself on the leaderboard and others' view of your track |
| `GET`/`POST /v1/track/weeks/{key}` | your week (hours, Kotu/Kyōtsu daily %, auto Kyōtsu from trainer sessions, summary); POST replaces it |
| `GET /v1/track/weeks?from=&to=` | your logged weeks (default last 52), totals/subtotals/scores |
| `GET /v1/users/{id}/track` | someone's track history (404 if they hid it); non-private summaries |
| `GET /v1/track/leaderboard?period=week\|4w\|12w\|52w\|all&week=` | ranked by immersion (active+passive), with per-week series |
| `GET /v1/track/summaries?limit=&before=&user=` | public feed of weekly summaries, newest week first |
| `GET /healthz` | `ok` |

Every user object carries `avatar` (path relative to the API base, or null), `level`, `xp`,
`level_xp` and `next_xp` (null at level 100). XP is the number of likes other people gave the
user's comments, counted live, so unlikes and deleted comments take it back.

Track weeks run Sunday 03:00 UTC → Sunday 03:00 UTC and are keyed by the Saturday before
(`internal/week`). Hours are stored in hundredths; a day's Kyōtsu score is the manual value, else
the trainer sessions that ended that day. Summaries are public unless marked private; users with
`track_public` off expose nothing.

Notifications: `reply` to the parent's author, `like` to the liked comment's author, and `comment`
to admins for every new comment by someone else — never two for one comment, never to yourself.
Unliking or deleting withdraws them.

Comment pages: the site publishes `/comment-pages.json`, `{"pages":{"/articles/x/":"Title",…}}`,
listing every page that renders a comment thread (`layouts/home.commentpages.json`, same condition
as `layouts/page.html`). fazond fetches `$FAZOND_SITE/comment-pages.json` at startup and hourly
(10 s timeout, 1 MB cap, a `?t=` query so GitHub Pages' CDN can't serve a stale copy) and keeps it
in memory. Only listed pages take comments, and page titles in notifications and profiles are the
listed ones — never anything a client sends. A comment on an unlisted page (say, a just-published
article) refetches the index first, at most once a minute across all requests. A fetch that fails,
isn't valid JSON or lists no valid pages keeps the previous index. Each successful load rewrites
the stored title of every page that has comments: to the listed title, or to `""` for a page no
longer listed (comments stay; the UI shows its path). If no index has loaded since startup (site
unreachable), posting answers 503 — fail closed — while reading comments, notifications and
profiles carries on as normal.

CSRF: every non-GET request needs an `Origin` in `FAZOND_ORIGINS` (403 otherwise) and, with a
body, `Content-Type: application/json` (415); the avatar upload alone takes `image/jpeg`/`image/png`
instead (still not a CORS-safelisted type, so it still needs our preflight). Bodies are capped at 64 KB (Kyōtsu sessions 1 MB).
Comments are limited to 30 per user per 10 minutes; Kyōtsu sessions, avatar uploads, profile
edits, week saves and other Track changes to 60, 20, 20, 120 and 120 per hour (429).

## Build

```sh
go vet ./... && go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o bin/fazond ./cmd/fazond
```

Local dev (magic links are printed to the log instead of emailed):

```sh
FAZOND_DATA=/tmp/fazond-dev FAZOND_SITE=http://localhost:1313 FAZOND_COOKIE_SECURE=false \
  FAZOND_ORIGINS=http://localhost:1313,http://127.0.0.1:* FAZOND_MAIL=log go run ./cmd/fazond
go run ./cmd/fazond login-link -data /tmp/fazond-dev -site http://localhost:1313 -email m.faizan.rasul@gmail.com
```

## Deploy (Hetzner, next to cinemad)

```sh
# DNS: A record api.fazon.nexus -> the server's IP
scp bin/fazond deploy/fazond.service deploy/env.example root@SERVER:/tmp/
ssh root@SERVER '
  useradd --system --no-create-home --shell /usr/sbin/nologin fazond
  install -m 0755 /tmp/fazond /usr/local/bin/fazond
  install -d -m 0750 -o root -g fazond /etc/fazond
  install -m 0640 -o root -g fazond /tmp/env.example /etc/fazond/env
  install -m 0644 /tmp/fazond.service /etc/systemd/system/fazond.service'
# put the Resend key in /etc/fazond/resend_key (see Mail), then:
ssh root@SERVER 'systemctl daemon-reload && systemctl enable --now fazond &&
  journalctl -u fazond -n 50 --no-pager && curl -s localhost:8091/healthz'
```

Add `deploy/Caddyfile.snippet` to `/etc/caddy/Caddyfile`, then `systemctl reload caddy`.
Upgrade: copy the new binary to `/usr/local/bin/fazond`, `systemctl restart fazond`
(migrations run on start). Deploy the site before a fazond that needs something new from it:
fazond only takes comments on pages listed in `$FAZOND_SITE/comment-pages.json`, so if the site
doesn't publish that file yet, posting comments answers 503 until it does (picked up within a
minute of the next attempt).

Data lives in `/var/lib/fazond/fazon.db`. Every hour fazond purges expired login tokens and
sessions and, if today's (UTC) is missing, writes `backups/fazon-YYYY-MM-DD.db.zst`
(`VACUUM INTO` a temp file, then zstd), keeping the newest 14. Leftover temp files and old
uncompressed `fazon-*.db` backups from earlier versions are cleaned up. To restore:

```sh
systemctl stop fazond
cd /var/lib/fazond
rm -f fazon.db-wal fazon.db-shm
zstd -d -f backups/fazon-YYYY-MM-DD.db.zst -o fazon.db   # apt install zstd
chown fazond:fazond fazon.db && chmod 0640 fazon.db
systemctl start fazond
```

## Users

There is no signup. The admin (`FAZOND_ADMIN_EMAIL`/`FAZOND_ADMIN_NAME`) is created at startup if
missing. Run the CLI **as the fazond user** with the service's env, so files it creates stay
owned by fazond:

```sh
fz() { sudo -u fazond sh -c 'set -a; . /etc/fazond/env; exec /usr/local/bin/fazond "$@"' fazond "$@"; }
fz user add -email friend@example.com -name "Friend"     # -admin to make an admin
fz user list
fz login-link -email friend@example.com -next /articles/some-post/   # prints a magic link, sends nothing
```

## Mail

`FAZOND_MAIL` picks the sender; sending happens in the background (the request returns at once)
and failures are logged (`journalctl -u fazond`).

- **`resend`** (production): Resend's HTTP API over HTTPS, so Hetzner's SMTP port blocks don't
  matter. Verify `fazon.nexus` as a domain in Resend (it gives you SPF/DKIM DNS records), create an
  API key with sending access only, and store it:
  ```sh
  install -m 0640 -o root -g fazond /dev/stdin /etc/fazond/resend_key <<< 're_...'
  ```
  with `FAZOND_RESEND_KEY_FILE=/etc/fazond/resend_key` (or `FAZOND_RESEND_KEY`). From defaults to
  `Fazon Nexus <login@fazon.nexus>`; override with `FAZOND_MAIL_FROM`. Each accepted email logs
  Resend's id; rejections log the status and Resend's message.
- **`smtp`**: STARTTLS submission on 587 (Hetzner blocks outbound 25 and 465). For Gmail, turn on
  2-step verification, create an app password, and set `FAZOND_SMTP_HOST=smtp.gmail.com`,
  `FAZOND_SMTP_USER=you@gmail.com`, `FAZOND_SMTP_PASS_FILE=/etc/fazond/smtp_pass` and
  `FAZOND_MAIL_FROM=Fazon Nexus <you@gmail.com>` (Gmail rewrites other From addresses).
- **`log`** (default): writes the email, including the link, to the log. For development.
