# backend (fazond)

Go API for fazon.nexus at `https://api.fazon.nexus`: magic-link accounts with open sign-up, roles
(user / moderator / admin) and moderation, threaded comments with likes and article likes on every article, in-site notifications, Kyōtsu trainer session history, video watch time from the site's tools, the
Track immersion log, and personal API keys for scripting your own Track data.
One binary, one SQLite file (`modernc.org/sqlite`, pure Go, so it cross-compiles with
`CGO_ENABLED=0`). The site itself stays static on GitHub Pages; `fazon.nexus` and
`api.fazon.nexus` are same-site, so the API's host-only `SameSite=Lax` cookie rides along on
`fetch(..., {credentials: "include"})`.

```
cmd/fazond         main: config (flags / FAZOND_* env), serve, CLI, hourly maintenance (comment-pages reload, purge, backup)
internal/api       handlers, session auth, CORS/CSRF, rate limits; api_test.go covers the spec
internal/db        open + pragmas, embedded migrations (PRAGMA user_version; 0001 init, 0002 avatars,
                   0003 track + bio, 0004 track 'anki' category, 0005 track ratings/goals + settings,
                   0006 api_keys, 0007 roles + mutes + signup_tokens, 0008 user ids never reused,
                   0009 Kyōtsu practice time on kyotsu_sessions, 0010 page_likes + notifications
                   rebuilt for 'page_like', 0011 Tokyo mode: users.hyojungo + kyotsu_sessions.mode,
                   0012 watch_sessions),
                   users/tokens, backup, purge
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
| `POST /v1/auth/signup` `{email,name,next,website}` | always `202 {"ok":true}` (see Sign-up): a new address gets `$FAZOND_SITE/account/verify/#s=TOKEN&next=PATH` (single use, 1 hour), an existing one a normal login link saying "You already have an account". `website` is a honeypot (filled → nothing sent). 400 only for a malformed email or name. Shares the auth/request limits |
| `POST /v1/auth/signup/confirm` `{token}` | consumes the sign-up token, creates the account (or uses the one the address got meanwhile) → `{"user":{…},"next":"/…","created":bool}` + `fz_session` cookie |
| `GET /v1/me` | `{"user":null}` or `{"user":{id,name,email,role,admin,muted,…},"unread":N}` |
| `DELETE /v1/me` | delete your account and all its data (see Users), clears the cookie; 409 for the only admin |
| `GET /v1/me/export` | everything stored about you as a JSON download (`Content-Disposition: attachment; filename="fazon-nexus-export-YYYY-MM-DD.json"`): profile, comments, likes (`likes` on comments, `article_likes`), notifications, Track (sub-categories, weeks with hours/scores/ratings/goals/summaries), Kyōtsu sessions, watch sessions (`watch_sessions`: `id, started, updated, video_id, active_seconds, track_added`), API key metadata (never keys or hashes). 10/hour (429 + `Retry-After`) |
| `GET /v1/admin/users?q=&limit=50&offset=0` | moderators + admins (403 otherwise): `{users:[{user,email (admins only),role,joined,last_active,comments,muted,muted_at,muted_by,track_public,can:{role,mute,delete}}],total,limit,offset,viewer,admins}`, newest first; `q` matches names (admins: and emails); `limit` ≤ 200 |
| `POST /v1/admin/users/{id}/role` `{role}` | admins: `user`/`moderator`/`admin` → `{"user":<row>}` |
| `POST /v1/admin/users/{id}/mute`, `…/unmute` | moderators (regular users only) and admins → `{"user":<row>}`; idempotent |
| `DELETE /v1/admin/users/{id}` | admins: delete that account and all its data, like `DELETE /v1/me` |
| `POST /v1/admin/users/{id}/hyojungo` `{on}` | admins: give / take Tokyo-mode access (not for admins, who always have it: 409) → `{"user":<row>}` |
| `GET /v1/comments?page=/articles/x/` | `{"comments":[…]}` oldest first; deleted comments with live replies come back as tombstones |
| `POST /v1/comments` `{page,parent,body}` | 201 comment (login; 403 `"You've been blocked from commenting."` if muted); 404 if `page` isn't in the site's comment-pages index, 503 if no index has loaded yet. A `title` field (older frontends) is ignored |
| `DELETE /v1/comments/{id}` | author, moderator or admin; soft delete (also drops its likes and notifications) |
| `POST`/`DELETE /v1/comments/{id}/like` | `{"likes":N,"liked":bool}`, idempotent |
| `GET /v1/pages/likes?page=/articles/x/` | article likes: `{"count":N,"liked":bool}` (`liked` false when logged out); 404 if `page` isn't in the comment-pages index, 503 if no index has loaded yet (see Article likes) |
| `POST`/`DELETE /v1/pages/likes?page=/articles/x/` | like / unlike an article (login; muted users too) → `{"count":N,"liked":bool}`, idempotent; same 404/503; 60 per user per 10 minutes |
| `GET /v1/notifications?limit=50&before=ID` | `{"notifications":[…],"unread":N}` newest first |
| `POST /v1/notifications/read` `{ids:[…]}` or `{all:true}` | `{"unread":N}` |
| `POST /v1/kyotsu/sessions` `{started,layout,mode,advanced,answers,active_seconds,client_id}` | `mode` `keihan` (default) or `tokyo` (needs Tokyo-mode access, else 403; see Tokyo mode); 201 `{"id":N,"active_seconds":S,"track_added":{…}\|null}` (200 for a retry of the same `client_id`); adds the practice time to your Track (see Kyōtsu practice time) |
| `GET /v1/kyotsu/sessions`, `GET /v1/kyotsu/sessions/{id}` | own sessions only (others' are 404); each has `mode`, `active_seconds` and `track_added` |
| `POST /v1/watch/sessions` `{client_id,started,active_seconds,video_id}` | a watch-session heartbeat (see Watch time): 201 `{"id":N,"active_seconds":S,"track_added":{…}\|null}` for a new `client_id`, 200 for later ones; adds the not-yet-added playing time to your Track. 300 per user per hour |
| `GET /v1/hyojungo/pairs`, `GET /v1/hyojungo/audio/{id}.aac` | Tokyo mode's pairs index and clips: login + Tokyo-mode access (401/403), 404 if not set up; `Cache-Control: private` |
| `GET /v1/levels` | `{"levels":[0,5,20,…,49005]}`: XP needed for levels 1–100, `5·(L−1)²` |
| `GET /v1/users/{id}` | public profile: `{user,joined,comments,likes,recent:[…20]}` (no email) |
| `POST /v1/me/profile` `{name}` | change display name (1–40 characters) → `{"user":…}` |
| `POST /v1/me/avatar` | raw `image/jpeg` or `image/png` body ≤ 2 MB, ≤ 4096×4096 → centre-cropped 256×256 JPEG, metadata stripped |
| `DELETE /v1/me/avatar` | remove the picture → `{"user":…}` |
| `GET /v1/users/{id}/avatar?v=T` | the JPEG; immutable caching when `v` is current, else `no-cache` + ETag |
| `GET /v1/users/{id}/card` | hover card: `{user,bio,joined,comments,likes}`, `Cache-Control: private, max-age=60` |
| `GET /v1/track/config` | categories (in display order: `active`, `passive`, `phonetic`, `output`, `soroban`, `anki`), your sub-categories, `public`, `current_week`, `current_week_end`, `published_through`, `ended_early`; admins also get `end_week` (see Track) |
| `POST /v1/track/subcats` `{category,name}`; `POST`/`DELETE /v1/track/subcats/{id}` | sub-categories: rename/archive/reorder; delete only if unused (else 409) |
| `POST /v1/track/settings` `{public}` | show/hide yourself on the leaderboard and others' view of your track |
| `GET`/`POST /v1/track/weeks/{key}` | your week (hours, Kotu/Kyōtsu daily %, auto Kyōtsu from trainer sessions, `ratings`, summary, plus read-only `goals`/`goals_open`, `updated`); POST replaces it (goals excepted); with `if_updated` (the `updated` it loaded, `""` for an unlogged week) it is a 409 if the week changed since |
| `POST /v1/track/weeks/{key}/goals` `{goals:{category:hours}}` | set goals for the current week; write-once (see Track) → the week |
| `GET /v1/track/weeks?from=&to=` | your logged weeks (default last 52), totals/subtotals/scores/ratings |
| `GET /v1/users/{id}/track` | someone's track history up to `published_through` (the owner: up to now; 404 if they hid it); non-private summaries, ratings, day-by-day scores |
| `GET /v1/track/leaderboard?period=last\|4w\|12w\|52w\|all&week=` | published weeks only; per-user totals, series, scores, latest public summary; ranked by active immersion |
| `GET /v1/track/leaderboard/goals?week=` | goal achievers of one published week |
| `GET /v1/track/summaries?limit=&before=&user=` | public feed of weekly summaries up to `published_through`, newest week first |
| `POST`/`DELETE /v1/track/admin/end-week` | admin: end the current week early (last 2 hours only) / undo |
| `GET /v1/me/api-keys` | your API keys `{keys:[{id,name,scope,prefix,created,last_used}],max:5}` (never the keys themselves) |
| `POST /v1/me/api-keys` `{name,scope}` | 201 `{"key":"fz_…","api_key":{…}}`; `scope` `read` or `write`; the key is shown only here; at most 5 (409) |
| `DELETE /v1/me/api-keys/{id}` | revoke (deletes it; works no more from the next request) |
| `GET /v1/api/track/weeks?from=&to=&days=1`, `GET /v1/api/track/weeks/{key}?days=1` | **API key only** (see API keys): your weeks with sub-category hours, totals, ratings, goals, summary, Kotu/Kyōtsu |
| `PATCH /v1/api/track/weeks/{key}/hours` `{entries:[…]}` | API key (write): set or add hours per category / sub-category, atomically |
| `PUT /v1/api/track/weeks/{key}/summary` `{summary,private}` | API key (write): the summary and its private flag only |
| `POST /v1/api/track/weeks/{key}/goals` `{goals:{…}}` | API key (write): the site's goals rules (current week, write-once) |
| `GET`/`POST /v1/api/track/subcats` | API key: list / create sub-categories (the site's rules) |
| `GET /healthz` | `ok` |

Every user object carries `role` (`user`, `moderator` or `admin`), `admin` (= role is admin; kept
for older frontends), `avatar` (path relative to the API base, or null), `level`, `xp`,
`level_xp` and `next_xp` (null at level 100). XP is the number of likes other people gave the
user's comments, plus, for the article author (the `FAZOND_ADMIN_EMAIL` account), the likes other
people gave the articles. It is counted live, so unlikes, deleted comments and deleted accounts
take it back. The profile's and hover card's `likes` is the same number.

**Article likes** (`page_likes`, `internal/api/pagelikes.go`): every page in the comment-pages
index can be liked, once per account; muted accounts can like (muting only blocks commenting).
Every article is written by the `FAZOND_ADMIN_EMAIL` account (`api.Config.AuthorEmail`, resolved
to its user id on each use, so nothing breaks if the account is recreated): each like from
someone else is one XP for it and notifies it (`page_like`). The author may like their own
article: the like counts on the article but gives no XP and no notification, as with comments.
On the site, the like button sits above the comments, with a compact ♥ in the article's date
line (`static/js/fazon-comments.js`).

Track weeks run Sunday 03:00 UTC → Sunday 03:00 UTC and are keyed by the Saturday before
(`internal/week`); their days are labelled Sunday … Saturday. Hours are stored in hundredths; a
day's Kyōtsu score is the manual value, else the trainer sessions that ended that day. Summaries are public unless marked private; users with
`track_public` off expose nothing. Track details below.

## Track

**Categories**, in display order: `active`, `passive`, `phonetic`, `output`, `soroban`, `anki`.
Hours in JSON are decimal hours built from exact hundredths (`1.17` = 1 h 10.2 min), so
`Math.round(h * 60)` minutes is exact.

**Publication.** Other people only ever see weeks up to `published_through`: normally the
previous week (`current_week` − 7 days); the current week too once the admin has ended it early.
This applies to the leaderboard (every period, totals and series), the goal achievers, the
summaries feed and `GET /v1/users/{id}/track` for anyone but its owner (hover cards and profiles
carry no Track data). The owner still sees their current week in their own week views and on
their own profile. Logging is unaffected: the calendar is time-based, so an ended week stays
editable until it naturally rolls over at `current_week_end`, and those edits show publicly at
once. The early end is stored in `settings` (`track_ended_week` = the week's key) and is simply
ignored once that week is over.

`POST /v1/track/admin/end-week` (admin only, 403 otherwise; usual Origin/CSRF rules; no body)
is allowed from `current_week_end` − 2 h until the natural end; earlier it answers 409 with the
time it opens. It is idempotent; `DELETE` undoes it (also idempotent). Both answer:

```json
{"published_through":"2026-10-10","current_week":"2026-10-10","current_week_end":"2026-10-18T03:00:00Z",
 "ended_early":true,"end_week":{"allowed":true,"allowed_from":"2026-10-18T01:00:00Z"}}
```

`GET /v1/track/config` carries the same fields (`end_week` for admins only).

**Tokyo mode (標準語).** The trainer's second mode uses Kotu's minimal pairs (kotu.io, via
Kuuuube/minimal-pairs): NHK accent-dictionary recordings, so they are never in the repo or on
the static site. `deploy/hyojungo_build.py` turns the pairs into `index.json` + `audio/*.aac`
under `$FAZOND_DATA/hyojungo/`, and fazond serves them only to logged-in accounts with access:
admins, and anyone given it on the admin page or with `fazond user hyojungo -email X [-off]`
(`/v1/me` has `hyojungo`). Tokyo sessions are stored with `mode: "tokyo"`, Kotu's own Pattern
Stats buckets as `cat` (`TH` Heiban / Odaka, `TA` Atamadaka, `TN` Nakadaka), their practice
time goes to a `Kotu` Phonetic sub-category (else `コツ`; same rules as below), and their
scores fill Track's Kotu % as `kotu_auto` the way Keihan sessions fill Kyōtsu's.

**Kyōtsu practice time.** The trainer measures the session's active time on the client (it
counts while the tab is visible and in use, stops when the tab is hidden and 5 minutes after the
last tap/click/key/scroll until the next one, and stops at End session) and sends it as
`active_seconds` with the session. In the transaction that stores the session the server adds
it to the user's Track: category `phonetic`, week = the week the session ended in (the server's
now, `week.Of`). Clamped to 0 … the session's wall-clock span (`started` → now), rounded to whole
minutes, then to hundredths of an hour like the stopwatch (12:34 → 13 min → 0.22 h); under half
a minute adds nothing. It goes into the user's active Phonetic sub-category named `Kyōtsu` or
`Kyotsu` (any case; the exact `Kyōtsu` first); if there is none, an archived one is restored,
else a `Kyōtsu` sub-category is created — by the site's rules (at most 1000 names per
category); when they don't allow it, into the unassigned bucket. The 168 h per category cap
shrinks the addition instead of failing the save. The week gets a `track_weeks` row / a new
`updated_at` like any save (`touchWeek`, the same add-to-row code as `PATCH …/hours`). The
session stores `active_seconds` and what it added (`track_centihours`, `track_subcat` name,
`track_note`); `track_added` in the responses is
`{"week":"2026-10-10","hours":0.22,"subcat":"Kyōtsu"|null,"note":"Created a “Kyōtsu” sub-category …"}`
(`hours` 0 with a note when the cap left no room; null when nothing was added and there's
nothing to say). `client_id` (8–64 of `A-Z a-z 0-9 _ -`, one per trainer run) makes the save
idempotent: a repeat for the same user updates that session (newer answers if there are more;
`active_seconds` only upwards; only the not-yet-added difference goes to Track) and answers 200.

**Watch time.** The site's video tools (Primed Listening) send `POST /v1/watch/sessions`
heartbeats while a video plays — about one a minute, plus one on pause / page hide — each with
the run's `client_id` (8–64 of `A-Z a-z 0-9 _ -`), `started` (RFC 3339, when playing started),
`active_seconds` (the run's **cumulative** playing seconds, a number ≥ 0) and optionally
`video_id` (an 11-character YouTube id). The first heartbeat for a `client_id` stores the run in
`watch_sessions` (201): its `started` must be at most 5 minutes ahead of the server (else 400;
ahead at all = now) and at most 24 h ago (else 400). Later ones update it (200) and their
`started` is ignored: `active_seconds` is clamped to the stored start → now (at most 24 h) and
never goes down. In the same transaction the difference between the run's time and what it
already counted (both in whole minutes, then hundredths like the stopwatch, so a run's
heartbeats add exactly its rounded total) goes to the **current** week's Active immersion, the
sub-category `Primed Listening` (an active one of that name in any case, else an archived one
restored, else created; when the sub-category limits don't allow it, unassigned) — the same code
and 168 h caps as Kyōtsu practice time; time a cap held back isn't added later. A run that
crosses the Sunday 03:00 UTC reset adds to both weeks. `track_added` is the run's total:
`{"week":<the week it last added to>,"hours":<all it added>,"subcat":…,"note":…}` (the note is
the latest thing worth saying, e.g. a sub-category created, and stays on later heartbeats).
Missing or 0 `active_seconds` (older cached trainer pages) adds nothing.

**Stale weeks.** `POST /v1/track/weeks/{key}` replaces the whole week, so a logger tab holding an
old copy would wipe time added meanwhile (trainer, stopwatch in another tab, API). The logger
sends `if_updated` (the week's `updated` as loaded, `""` if it wasn't logged); a mismatch is a
409 and it shows "changed elsewhere" with a Reload week button (its stopwatch takes the time out
of the stale form, reloads and adds it again, or keeps it queued if the form had other unsaved
edits). Without `if_updated` the save is unconditional, as before. Every change moves a week's
`updated_at` on by at least a second (`MAX(now, previous + 1)`), so two changes within the same
second still differ (`updated` can then run a second or two ahead of the clock).

**Ratings**: an optional self-rating per category per week, `1` 😞, `2` 😐, `3` 👌, `4` 🔥.
Week documents, list items, profile weeks and feed items have `"ratings":{"active":3,"anki":4}`
(rated categories only; `{}` when none). `POST /v1/track/weeks/{key}` takes the same object; an
unknown category or a value other than 1–4/null is a 400; null or absent clears (the save is
wholesale, like everything else in it). A week with only ratings is still a logged week. Ratings
are public wherever the week's hours are.

**Goals**: hours a user aims for per category in the current week. `POST
/v1/track/weeks/{key}/goals` `{"goals":{"active":35,"anki":3.5}}` (only the categories being set):
each > 0 and ≤ 168 with ≤ 2 decimals (null can't clear: 400); `{key}` must be the current week
(past: 409, future: 400). Write-once: re-sending a goal's current value is a no-op, a different
value is a 409 and nothing in the request is applied; categories without a goal can still be set
until the week ends. Answers the full week document. Week documents carry `"goals":{"active":35}`
(set ones only) and `"goals_open"` (true for the current week). The week save never touches
goals, and emptying a week keeps them. While a week runs its goals are private; once published,
the goal achievers board shows them for users who met at least one.

**Leaderboard** `GET /v1/track/leaderboard?period=&week=`: `period` is `last` (the single
published-through week; `week` is an alias for old frontends), `4w`, `12w`, `52w` or `all`
(default `4w`); every period ends at `week` (default and maximum: `published_through`; later
weeks are clamped, future ones are a 400).

```json
{"period":"last","from":"2026-10-03","to":"2026-10-03","weeks":["2026-10-03"],
 "published_through":"2026-10-03","current_week":"2026-10-10","current_week_end":"2026-10-18T03:00:00Z","ended_early":false,
 "users":[{"user":{"id":2,"name":"Alice",…},
   "totals":{"active":12,"passive":5,"phonetic":0,"output":1,"soroban":0,"anki":3,"all":21,"nopassive":16},
   "series":{"active":[12],…,"all":[21],"nopassive":[16],"kotu":[65],"kyotsu":[null]},
   "kotu":65,"kyotsu":null,"weeks_logged":1,
   "ratings":{"active":3},
   "summary":"Good week","summary_week":"2026-10-03"}]}
```

`totals` and `series` (one value per entry of `weeks`, null where the user logged nothing) have
every category plus `all` (every category) and `nopassive` (all − passive); series also have
`kotu`/`kyotsu` weekly averages. `kotu`/`kyotsu` are the period's averages of weekly averages.
`ratings` is present only for `period=last`. `summary`/`summary_week` are the latest non-empty,
non-private summary in the period (null if none; private summaries never appear here). Users are
sorted by active hours, then all, weeks logged, id (the frontend re-sorts per metric). Responses
also still carry an `immersion` (active + passive) total/series for cached old frontends; it is
not a metric any more.

**Goal achievers** `GET /v1/track/leaderboard/goals?week=` (default and maximum:
`published_through`): public users who met at least one of their goals that week. A goal is met
when the category's logged hours ≥ the goal. `percent` = (hours logged in the goal categories ÷
the sum of those goals − 1) × 100, one decimal (negative possible). Tier 1 (`all_met`) ranks above
tier 2 (met some) whatever the percentages; within a tier by exact percentage, then name.

```json
{"week":"2026-10-03","published_through":"2026-10-03",
 "users":[{"user":{…},"tier":1,"all_met":true,"goals_set":2,"goals_met":2,"percent":10,
   "goals":{"active":10,"passive":10},"logged":{"active":12,"passive":10}}]}
```

**Profiles** `GET /v1/users/{id}/track`: `{user, subcats, weeks, published_through,
first_score_week}`; each week has `week, totals, subtotals, kotu, kyotsu` (weekly averages),
`ratings`, `summary` (`""` if private, unless you're the owner, who also gets `summary_private`),
and day-by-day scores: `days` (the 7 label dates, Sunday … Saturday as in the week view: day i
of the week keyed by Saturday `k` is labelled `k` + 1 + i days, the UTC date most of its window
falls on),
`kotu_days` / `kyotsu_days` (7 effective values: manual, else the trainer's Tokyo / Keihan mode)
and `kotu_source` / `kyotsu_source` (`"manual"`, `"auto"` or null per day). `first_score_week` is the first
returned week with any Kotu or Kyōtsu value (null if none). Only logged weeks are listed, so
trainer sessions in a week with nothing logged don't appear here.

**Summaries are Markdown.** Every `summary` field (week POST, `PUT …/summary`, leaderboard,
profiles, `/v1/track/summaries`) is the author's raw Markdown source (GitHub-flavoured: headings,
emphasis, strikethrough, lists and task lists, quotes, code, tables, links; single newlines are
line breaks), stored and returned as text (cleaned and trimmed as before, ≤ 10,000 characters).
The API never renders or sanitizes it, so every client must treat it as untrusted text: the site
renders it with marked + DOMPurify under a strict allowlist (`static/js/fazon-md.js`: raw HTML
shown as text, links http/https/mailto only, images shown as links). Other user text (comments,
bios, names) is plain text.

Notifications: `reply` to the parent's author, `like` to the liked comment's author, `comment`
to admins for every new comment by someone else, and `page_like` to the article author — never two
for one comment or one (liker, article), never to yourself. Unliking or deleting withdraws them.
A `page_like` notification has the article's `page`/`page_title`, `comment_id` 0 and no snippet.

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

CSRF: every non-GET request (outside `/v1/api/`, see API keys) needs an `Origin` in
`FAZOND_ORIGINS` (403 otherwise) and, with a body, `Content-Type: application/json` (415); the avatar upload alone takes `image/jpeg`/`image/png`
instead (still not a CORS-safelisted type, so it still needs our preflight). Bodies are capped at 64 KB (Kyōtsu sessions 1 MB).
Comments are limited to 30 per user per 10 minutes, article likes/unlikes to 60; Kyōtsu sessions, watch heartbeats, avatar uploads, profile
edits, week saves and other Track changes to 60, 300, 20, 20, 120 and 120 per hour (429).

## MvJ sync

Optional: `fazond serve` mirrors the admin's (`FAZOND_ADMIN_EMAIL`) Track into the MvJ dojo
immersion tracker (mattvsjapan.com/immersion-tracking), so Track is the only place to log
(`internal/api/mvjsync.go`). That site is a Supabase app with no public API; fazond talks to the same PostgREST tables its
frontend uses: `weeks` (one per week, `start_date` = our week key) and `weekly_logs` (one row per
user and week, upserted whole), as the owner's MvJ account.

- **No password on the server.** Sign in from your own machine with
  `python3 backend/deploy/mvj_login.py root@<server>`: it reads the `FAZOND_MVJ_*` settings from
  the server's env, asks for the password, signs in as the MvJ login form does, and pipes only the
  session's refresh token to `fazond mvj-token` (kept in `settings`, key `mvj_refresh_token`)
  before restarting fazond. The sync refreshes that session (the browser's is untouched) and
  stores each rotated token at once; a leak exposes only this tracker session, never a password.
  If the session dies (MvJ signs everyone out, a rotation is lost), the journal says
  `no MvJ session` / `MvJ session refresh: …`: run the script again.

- Every minute it loads the weeks from `FAZOND_MVJ_FROM` to now and pushes each one whose hours,
  ratings or summary changed since its last push (whatever changed it: a save, the API, the
  Kyōtsu trainer). Weeks never logged here are never pushed; a week emptied here is pushed as
  zeros. After a restart every logged week in range is pushed once more (harmless: an upsert).
- The MvJ tracker has no Kotu / Kyōtsu scores, so the notes pushed are a results block, then the
  summary underneath (a blank line between). One `Kotu Results:` / `Kyōtsu Results:` section per
  test with any score that week: its days Sunday … Saturday on one line, comma-separated (the
  effective score, manual else the trainer's), `–` for a blank day before the last scored one and
  nothing after it:

  ```
  Kotu Results:

  96.2%, 90.9%, –, 93.7%

  Kyōtsu Results:

  84.7%, 84.0%

  <summary>
  ```

  The summary on Track stays as written; the block exists only in what's pushed. A score change
  is a change, so it's pushed within a minute.
- Track wins: edits made on the MvJ site to a mirrored week are overwritten by the next change
  here. Exceptions, taken from the MvJ row: the notes when our summary is private, and the rating
  of a category not rated here (🔥, the MvJ form's default, if it has none either).
- A week the MvJ site hasn't created yet is skipped and retried. Failures log a warning and back
  off, doubling up to an hour.
- Unofficial: if the MvJ site changes its tables, the sync breaks (watch the journal for
  `mvj sync`). Set it up with the `FAZOND_MVJ_*` lines in `deploy/env.example` and the script
  above; the Supabase URL and anon key are the `…supabase.co` host and `apikey` header of any request the tracker page makes.

## API keys

Any user can create up to 5 personal API keys in the Developers section of their own profile
(`/users/?id=<own id>`) to script their own Track data; the user-facing docs are the site's
`/developers/` page (`content/developers/_index.md`, `layouts/developers/section.html`), which
must stay in step with `internal/api/trackapi.go`.

- **Format and storage.** `fz_` + 32 random bytes in base64url (46 characters). Only the
  SHA-256 is stored (`api_keys.hash`, unique), plus a display prefix (`fz_` + 4 characters); the
  full key is in the creation response only. Lookup is by hash (its timing reveals nothing about
  the key) followed by a constant-time compare. Revoking deletes the row, so the key fails from
  the next request; deleting a user cascades to their keys. `last_used_at` is written at most
  once a minute per key. Key usage is logged at debug level only (key id, user, method, path;
  never key material).
- **Scope of a key.** Keys work only on `/v1/api/…` and only for the key owner's own Track data;
  everything else (comments, accounts, notifications, key management, admin) ignores the
  `Authorization` header and answers 401 without a session. `/v1/api/…` in turn ignores the
  session cookie: `Authorization: Bearer fz_…` or 401 (with `WWW-Authenticate: Bearer`).
  `read` keys get 403 on anything but GET/HEAD.
- **No CSRF/CORS there.** With no ambient credential there is nothing for another site to forge,
  so `/v1/api/…` needs no `Origin` (scripts send none) and takes any `Content-Type` (the body is
  parsed as JSON regardless). It sends **no** CORS headers and answers preflights with 403, so web
  pages elsewhere can't send it keys or read its answers. The path is cleaned before this check,
  so `/v1/api/../x` gets the normal rules.
- **Rate limits**, per key (in-memory sliding windows like the rest): 120 reads and 30 writes a
  minute; 429 with `Retry-After` (seconds). Creating keys: 20 an hour per user.
  `POST /v1/api/track/subcats` and `…/goals` reuse the site's code but not its per-user hourly
  limits.

Key-API week object (`GET …/weeks` returns `{from,to,current_week,weeks:[…]}`, every week of
the range oldest first, logged or not; default the last 12 weeks through the current one, at most
260; every write answers the updated week, and `?days=1` works on all of them):

```json
{"week":"2026-10-03","start":"2026-10-04T03:00:00Z","end":"2026-10-11T03:00:00Z","current":false,"logged":true,
 "totals":{"active":13.75,"passive":4,"phonetic":0,"output":0,"soroban":0,"anki":1,"all":18.75,"nopassive":14.75},
 "hours":{"active":{"unassigned":2,"subcats":[{"id":3,"name":"Anime","archived":false,"hours":10.5}]},"passive":{…},…},
 "ratings":{"active":4},"goals":{},"summary":"…","private":true,"kotu":85,"kyotsu":80,"updated":"2026-10-04T12:00:00Z",
 "days":["2026-10-04",…],"kotu_days":[80,null,…],"kyotsu_days":[null,70,…],"kyotsu_source":[null,"manual",…]}
```

`PATCH …/hours` entries are `{"category", "subcat" (name) | "subcat_id" | neither (the
unassigned bucket), "set" | "add"}`. Names match the category's non-archived sub-categories
ignoring case (an exact match wins if folding makes two equal; still ambiguous → 400); ids may
name archived ones (for old weeks). Values are JSON numbers or strings `"1.5"`, `"1:30"`,
`"-0:45"`, rounded to the nearest hundredth; `set` ≥ 0, `|value|` ≤ 168. Entries apply in order in
one transaction; any failure (unknown sub-category, a row below 0 or above 168, a category above
168, …) is a 400 naming the entry and nothing is written. A row at 0 is deleted. Only the named
rows change. Week rows follow the site's save: a write to a week with no row creates one (empty
scores/summary, public), `updated_at` is bumped, and the row is deleted again if nothing is left
in it (no hours, Kotu/Kyōtsu, summary or ratings), so ratings are never lost; goals live outside
the row. `PUT …/summary` takes `summary` (required; `""` clears; site limits: cleaned, ≤ 10,000
characters) and optional `private` (omitted = unchanged; public for a new week).

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
Tokyo mode's data (see Tokyo mode) goes in separately, never through git:

```sh
git clone --depth 1 https://github.com/Kuuuube/minimal-pairs.git /tmp/kmp
python3 deploy/hyojungo_build.py /tmp/kmp /tmp/hyojungo
rsync -a --delete /tmp/hyojungo/ root@SERVER:/var/lib/fazond/hyojungo/
ssh root@SERVER 'chown -R fazond: /var/lib/fazond/hyojungo'
```
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

Anyone can sign up on the site (`/account/signup/`); the admin can still add accounts with the CLI.
The admin (`FAZOND_ADMIN_EMAIL`/`FAZOND_ADMIN_NAME`) is created at startup if missing, and made an
admin again if their role was changed. That account is also the author of every article: article
likes are its XP and notify it. Run the CLI **as the fazond user** with the service's env, so files it creates stay
owned by fazond:

```sh
fz() { sudo -u fazond sh -c 'set -a; . /etc/fazond/env; exec /usr/local/bin/fazond "$@"' fazond "$@"; }
fz user add -email friend@example.com -name "Friend"     # -admin to make an admin
fz user list
fz login-link -email friend@example.com -next /articles/some-post/   # prints a magic link, sends nothing
```

**Sign-up.** `POST /v1/auth/signup` answers `202 {"ok":true}` whatever happens, so it never tells
whether an address has an account. A new address gets a confirmation email (a pending sign-up in
`signup_tokens`: email, name, next, sha256 of the token, 1 hour, single use; no account exists
until it's confirmed). An address with an account gets an ordinary login link with a line saying
"You already have an account". A filled-in honeypot (`website`, hidden on the form) or a
rate-limited request sends nothing (logged). It shares the login-link limits: 3 per address per 15
minutes and 10 per IP per hour, counting both kinds of request. Confirming
(`/v1/auth/signup/confirm`, via the site's verify page with `#s=`) creates the account as a `user`
and logs in; if the address got an account meanwhile it simply logs into that one, and any other
pending links for the address are spent. Expired tokens are purged hourly.

**Roles** (`users.role`; `users.is_admin` is kept equal to role = admin for rollbacks):

| | user | moderator | admin |
|---|---|---|---|
| delete own comments | ✓ | ✓ | ✓ |
| delete anyone's comment | | ✓ | ✓ |
| admin page: list users | | ✓ (no emails; search by name) | ✓ (with emails; search by name or email) |
| mute / unmute | | regular users only | anyone but themselves |
| change roles | | | anyone but themselves |
| delete accounts | | | anyone but themselves |

The last admin can never be demoted, muted or deleted, and can't delete their own account
(`DELETE /v1/me` → 409); nobody changes their own role or mutes/deletes themselves through the admin
API. Only admins get the "new comment" notifications. Every admin/moderation action is logged at
Info with the actor, the action and the target ids.

**Mute** (`muted_at`, `muted_by`): a muted user can't post comments or replies (403 `"You've been
blocked from commenting."`); likes, Track, profile and everything else still work. The site's
composer says so instead of showing the form.

**The admin page** is `/admin/` (not in the main nav; "Admin" in the account menu for moderators
and admins; "Not allowed" for others). It lists every user (50 at a time, newest first, search)
with name, email (admins), role, joined, last seen (newest session use), comment count, mute and
Track visibility, plus the controls the viewer's `can` flags allow. Deleting needs the user's name
typed in the page.

**Deleting an account** (`DELETE /v1/me` or `DELETE /v1/admin/users/{id}`, one transaction,
`db.DeleteUser`) removes the user row and everything referring to it: sessions, login and sign-up
tokens, API keys, avatar, Track weeks/hours/ratings/goals/sub-categories, Kyōtsu and watch sessions, likes
given (on comments and articles), notifications to or from them, and their comments. A comment nobody replied to is deleted
outright (leaf first, so chains of their own replies go too). A comment with replies stays as a
`[deleted]` tombstone exactly like an author-deleted one (body cleared, its likes and notifications
dropped) and is handed to the placeholder account id 0 (`db.GhostID`: no email, never listed,
can't log in, created on first use), so threads stay intact and no row points at the deleted user.
Mutes they gave keep `muted_at` with `muted_by` cleared. The daily backups still hold the data for
up to 14 days.

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
