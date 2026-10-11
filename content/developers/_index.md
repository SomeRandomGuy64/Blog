+++
title = "Developer API"
description = "Script your own Track data with a personal API key: read your weeks, log hours, write summaries and set goals."
# Rendered by layouts/developers/section.html. Not in the main menu (linked from the Developers
# section of your own profile and the footer). Keep in step with backend/internal/api/trackapi.go.
outputs = ["html"]
+++

The Track API lets your own scripts read and update **your own** Track data: hours per category and
sub-category, weekly summaries, goals and your list of sub-categories. Ratings and Kotu / Kyōtsu
scores can be read too. Sync hours from Anki or a media player, back up your log, or build your own
charts.

A key only ever reaches the data of the account that created it, and only the Track endpoints
below. It can't read or post comments, see notifications, change your profile or manage keys.

## Quick start

1. Open your profile (account menu → **Your profile**) and scroll down to **Developers**.
2. Create a key: give it a name and pick **Read only** or **Read & write**.
3. Copy the key. It's shown once; only a fingerprint of it is stored.
4. Try it:

```sh
export FAZON_API_KEY="fz_..."   # your key
curl -s -H "Authorization: Bearer $FAZON_API_KEY" https://api.fazon.nexus/v1/api/track/weeks
```

## API keys

- **Format.** Keys look like `fz_` followed by 43 characters. Your profile lists each key by name
  with its first characters (`fz_ab12…`), its access level, when it was created and when it was
  last used (updated at most once a minute).
- **Access.** *Read only* keys can call the `GET` endpoints. *Read & write* keys can also log hours,
  write summaries, set goals and create sub-categories. A read-only key gets `403` on a write.
- **Limits.** Up to 5 keys per account. Revoke the ones you don't use.
- **Revoking** a key (on your profile) is immediate: the next request with it gets `401`.
- **Keep it secret.** Anyone with a read & write key can change your Track data, and a read-only key
  can read all of it, private summaries included. Keep keys in an environment variable or a
  password manager, not in code you share. If one leaks, revoke it and create a new one.
- **Scripts only.** The API is meant for scripts, command-line tools and servers. Web pages on other
  sites can't call it: it sends no CORS headers, so browsers block those requests.

## Authentication

Send the key in the `Authorization` header of every request:

```http
Authorization: Bearer fz_...
```

Keys only work on the `/v1/api/…` endpoints on this page, and those endpoints only accept keys:
being logged in to the site in a browser doesn't count. A missing, malformed, unknown or revoked key
gets `401` with a `WWW-Authenticate: Bearer` header.

## Basics

- **Base URL:** `https://api.fazon.nexus`. Every path below starts with `/v1/api/track`.
- **JSON** in and out. Send request bodies as JSON (`Content-Type: application/json` is good
  practice; it isn't required). Bodies are limited to 64 KB.
- **Times** are RFC 3339 in UTC, e.g. `2026-10-11T03:00:00Z`.
- **Errors** are `{"error":"a message for humans"}` with an HTTP status (see [Errors](#errors)).
- **Rate limits**, per key: **120 reads** and **30 writes** per minute (sliding window). Over the
  limit you get `429` with a `Retry-After` header (seconds to wait).

## Weeks, categories and hours

**Weeks** run from **Sunday 03:00 UTC to the next Sunday 03:00 UTC** (Saturday evening in the
Americas). A week's **key** is the Saturday *before* it starts, as `YYYY-MM-DD`: week `2026-10-10`
runs from `2026-10-11T03:00:00Z` to `2026-10-18T03:00:00Z`. Anything you did before 03:00 UTC on a
Sunday belongs to the previous week. You can read and write past weeks and the current week (back to
`2000-01-01`); a future week, a date that isn't a Saturday or a malformed key gets `400`.

The week's 7 days are labelled by date, Saturday to Friday: day *i* runs from Sunday 03:00 UTC + *i*
days for 24 hours, so each label is the date just before most of its window (the same calendar as
the site).

The current week's key in Python:

```python
from datetime import datetime, timedelta, timezone

def week_key(t=None):
    """The key (Saturday label) of the Track week containing t, by default now."""
    t = (t or datetime.now(timezone.utc)) - timedelta(hours=3)
    sunday = t.date() - timedelta(days=(t.weekday() + 1) % 7)
    return (sunday - timedelta(days=1)).isoformat()
```

**Categories** (ids, in display order): `active` (active immersion), `passive` (passive
immersion), `phonetic` (phonetic training), `output`, `soroban`, `anki`.

**Sub-categories** are yours to define per category (say *Anime* and *YouTube* under `active`).
Hours go either to a sub-category or to the category's **unassigned** bucket. Sub-categories can be
renamed, reordered and archived on the site; archived ones keep their hours in old weeks.

**Hours** come back as decimal hours built from exact hundredths of an hour (`1.17` = 1 h 10.2 min),
so `round(hours * 60)` gives whole minutes. When you send hours you can use:

| Form | Example | Meaning |
|---|---|---|
| a number | `1.5`, `0.75`, `-0.5` | decimal hours |
| a decimal string | `"1.5"` | the same |
| an `"h:mm"` string | `"1:30"`, `"0:05"`, `"-0:45"` | hours and minutes |

Values are rounded to the nearest hundredth of an hour (36 seconds). Negative values are only
allowed as an `add`. A week holds at most **168 hours** per sub-category row and per category.

**Ratings** (`1` 😞, `2` 😐, `3` 👌, `4` 🔥 per category) and **Kotu / Kyōtsu** daily scores are
set on the site; the API reads them. A day's Kyōtsu score is the one you entered, else the result of
the Kyōtsu trainer sessions you finished that day.

## The week object

Every week endpoint returns weeks in this shape (pretty-printed here):

```json
{
  "week": "2026-10-03",
  "start": "2026-10-04T03:00:00Z",
  "end": "2026-10-11T03:00:00Z",
  "current": false,
  "logged": true,
  "totals": {
    "active": 13.75, "all": 18.75, "anki": 1, "nopassive": 14.75,
    "output": 0, "passive": 4, "phonetic": 0, "soroban": 0
  },
  "hours": {
    "active": {
      "unassigned": 2,
      "subcats": [
        {"id": 12, "name": "Anime", "archived": false, "hours": 10.5},
        {"id": 13, "name": "YouTube", "archived": false, "hours": 1.25}
      ]
    },
    "anki": {"unassigned": 0, "subcats": [{"id": 15, "name": "Core 2k", "archived": false, "hours": 1}]},
    "output": {"unassigned": 0, "subcats": []},
    "passive": {"unassigned": 4, "subcats": []},
    "phonetic": {"unassigned": 0, "subcats": []},
    "soroban": {"unassigned": 0, "subcats": []}
  },
  "ratings": {"active": 4},
  "goals": {"active": 12},
  "summary": "Finished my first show without subtitles.",
  "private": false,
  "kotu": 85,
  "kyotsu": 80,
  "updated": "2026-10-10T21:14:03Z"
}
```

| Field | |
|---|---|
| `week`, `start`, `end` | the week's key and its exact start and end |
| `current` | `true` for the week running now |
| `logged` | `true` if anything is logged (hours, scores, a summary or ratings). Unlogged weeks are listed too, with zeros |
| `totals` | hours per category, plus `all` (every category) and `nopassive` (all but passive) |
| `hours` | every category: its `unassigned` hours and each sub-category with hours (`id`, `name`, `archived`, `hours`), in your display order |
| `ratings` | rated categories only (`{}` if none) |
| `goals` | categories with a goal for that week only (`{}` if none) |
| `summary`, `private` | the week's summary as Markdown source (`""` if none; see below) and whether it's hidden from others |
| `kotu`, `kyotsu` | the week's averages of the daily percentages (1 decimal), `null` without any |
| `updated` | when the week last changed, `null` if it isn't logged |

Add `?days=1` to any week endpoint (reads and writes) for the day-by-day scores too:

```json
{
  "days": ["2026-10-03", "2026-10-04", "2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09"],
  "kotu_days": [80, null, 90, null, null, null, null],
  "kyotsu_days": [null, 70, 90, null, null, null, null],
  "kyotsu_source": [null, "manual", "auto", null, null, null, null]
}
```

`kyotsu_source` says where each day's Kyōtsu value comes from: `"manual"` (entered on the site),
`"auto"` (the trainer) or `null` (no value).

## Endpoints

### `GET /v1/api/track/weeks`

Your weeks in a range, **oldest first, every week of the range** whether logged or not.

| Query | |
|---|---|
| `from` | first week key (default: 11 weeks before `to`, so 12 weeks in all) |
| `to` | last week key (default: the current week) |
| `days` | `1` to include the day-by-day scores |

At most 260 weeks (5 years) per request; `from` after `to` is a `400`.

```sh
curl -s -H "Authorization: Bearer $FAZON_API_KEY" \
  "https://api.fazon.nexus/v1/api/track/weeks?from=2026-01-03&to=2026-10-10"
```

```json
{"from": "2026-01-03", "to": "2026-10-10", "current_week": "2026-10-10", "weeks": [ ... ]}
```

### `GET /v1/api/track/weeks/{week}`

One week (any past or current week, logged or not), as a week object.

```sh
curl -s -H "Authorization: Bearer $FAZON_API_KEY" \
  "https://api.fazon.nexus/v1/api/track/weeks/2026-10-10?days=1"
```

### `GET /v1/api/track/subcats`

Your sub-categories, archived ones included, in display order (by category, then your order):

```json
{"subcats": [
  {"id": 12, "category": "active", "name": "Anime", "archived": false, "position": 0},
  {"id": 13, "category": "active", "name": "YouTube", "archived": false, "position": 1},
  {"id": 15, "category": "anki", "name": "Core 2k", "archived": false, "position": 0}
]}
```

### `POST /v1/api/track/subcats`

*Read & write.* Create a sub-category, with the site's rules: a name of 1–40 characters (trimmed,
no control characters) that isn't already used by an active sub-category of that category (ignoring
case), and at most 30 active sub-categories per category. Answers `201` with the new sub-category.
A taken name or a full category is a `409`.

```sh
curl -s -X POST -H "Authorization: Bearer $FAZON_API_KEY" -H "Content-Type: application/json" \
  -d '{"category":"active","name":"Anime"}' https://api.fazon.nexus/v1/api/track/subcats
```

```json
{"id": 12, "category": "active", "name": "Anime", "archived": false, "position": 0}
```

Renaming, reordering, archiving and deleting sub-categories is done on the site.

### `PATCH /v1/api/track/weeks/{week}/hours`

*Read & write.* Change some of a week's hours and nothing else. The body is a list of entries; each
targets one row and either **sets** it or **adds** to it:

```json
{"entries": [
  {"category": "active", "subcat": "anime", "add": "1:30"},
  {"category": "anki", "add": 0.75},
  {"category": "passive", "subcat_id": 14, "set": 3}
]}
```

| Entry field | |
|---|---|
| `category` | required: one of the category ids |
| `subcat` | a sub-category **name**: matched against that category's *active* (non-archived) sub-categories, ignoring case |
| `subcat_id` | or a sub-category **id** (from `GET …/subcats`); this also reaches archived ones, for old weeks |
| *(neither)* | the category's unassigned hours |
| `set` | the row's new value, ≥ 0 |
| `add` | a change to the row's current value; may be negative, but the result can't go below 0 |

Each entry needs exactly one of `set` or `add`, and at most one of `subcat` or `subcat_id`.

- Entries are applied **in order** (two `add`s to the same row both count), **all or nothing**: if
  any entry fails, nothing is changed and you get a `400` naming the entry (`entries[1]: …`).
- After the changes, each row must be between 0 and 168 hours, and each category at most 168 hours.
- A row that ends up at 0 is removed.
- Only the rows you name change. Your summary, scores, ratings, goals and every other row stay as
  they are.
- Writing to a week with nothing logged starts it; removing the last thing in a week (here, its last
  hours, if it has no scores, summary or ratings) un-logs it again, just like clearing it on the site.
- 1 to 100 entries per request.

A sub-category name you don't have is a `400` that says so; create it first with
[`POST /v1/api/track/subcats`](#post-v1apitracksubcats):

```json
{"error": "entries[0]: you have no sub-category named \"Drama\" in active; create it first with POST /v1/api/track/subcats {\"category\":\"active\",\"name\":\"Drama\"}"}
```

Answers the updated week object.

```sh
curl -s -X PATCH -H "Authorization: Bearer $FAZON_API_KEY" -H "Content-Type: application/json" \
  -d '{"entries":[{"category":"anki","add":"0:45"},{"category":"active","subcat":"Anime","add":"1:30"}]}' \
  https://api.fazon.nexus/v1/api/track/weeks/2026-10-10/hours
```

### `PUT /v1/api/track/weeks/{week}/summary`

*Read & write.* Set the week's summary and, optionally, whether it's private. Nothing else changes.

Summaries are **Markdown** (GitHub-flavoured: headings, bold/italic, strikethrough, lists and task
lists, quotes, code, tables, links; a single newline is a line break). The site shows them rendered,
with raw HTML shown as text, only `http`/`https`/`mailto` links, and images shown as links. The API
stores and returns the source as-is, so if you display summaries yourself, treat them as untrusted
text and sanitise any HTML you render from them.

```json
{"summary": "Finished my first show without subtitles.", "private": false}
```

| Field | |
|---|---|
| `summary` | required; Markdown (GitHub-flavoured), up to 10,000 characters (trimmed; `""` clears it) |
| `private` | optional; `true` hides the summary from everyone else. Leave it out to keep the current setting (new weeks start public) |

Public summaries show on your profile, the leaderboard and the summaries feed once the week is
published, as on the site. Answers the updated week object.

```sh
curl -s -X PUT -H "Authorization: Bearer $FAZON_API_KEY" -H "Content-Type: application/json" \
  -d '{"summary":"Lots of anime this week.","private":true}' \
  https://api.fazon.nexus/v1/api/track/weeks/2026-10-10/summary
```

### `POST /v1/api/track/weeks/{week}/goals`

*Read & write.* Set this week's goals: hours you aim for per category. The rules are the site's:

- Only for the **current** week (a past week is a `409`, a future one a `400`).
- Each goal is more than 0 and at most 168 hours, with at most 2 decimals. Goals can't be cleared.
- **Write-once:** once a category has a goal for the week it can't be changed. Sending the same value
  again is fine; a different value is a `409`, and then nothing in that request is applied.
  Categories without a goal can still get one until the week ends.

```sh
curl -s -X POST -H "Authorization: Bearer $FAZON_API_KEY" -H "Content-Type: application/json" \
  -d '{"goals":{"active":35,"anki":3.5}}' \
  https://api.fazon.nexus/v1/api/track/weeks/2026-10-10/goals
```

Answers the week object. Goals are private while the week runs; once it's published, the goal
achievers board shows them if you met at least one.

## Python example

Standard library only. Logs today's study into the current week in one atomic request, then prints
the last 12 weeks:

```python
import json, os, urllib.error, urllib.request
from datetime import datetime, timedelta, timezone

API = "https://api.fazon.nexus/v1/api/track"
KEY = os.environ["FAZON_API_KEY"]

def call(method, path, body=None):
    req = urllib.request.Request(
        API + path, method=method,
        data=None if body is None else json.dumps(body).encode(),
        headers={"Authorization": "Bearer " + KEY, "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        raise SystemExit(f"{e.code}: {json.load(e)['error']}")

def week_key(t=None):
    t = (t or datetime.now(timezone.utc)) - timedelta(hours=3)
    sunday = t.date() - timedelta(days=(t.weekday() + 1) % 7)
    return (sunday - timedelta(days=1)).isoformat()

week = call("PATCH", f"/weeks/{week_key()}/hours", {"entries": [
    {"category": "anki", "add": "0:45"},
    {"category": "active", "subcat": "Anime", "add": "1:30"},
]})
print(f"{week['week']}: {week['totals']['all']} h so far")

for w in call("GET", "/weeks")["weeks"]:
    print(w["week"], f"{w['totals']['active']:6.2f} h active", w["kotu"], w["kyotsu"])
```

## Errors

| Status | When |
|---|---|
| `400` | invalid input: a bad week key or a future week, an unknown category or sub-category, a value out of range, invalid JSON. The message says what's wrong |
| `401` | no key, a malformed or unknown key, or a revoked one |
| `403` | a read-only key used for a write |
| `404` | no such endpoint (or the wrong method for it) |
| `409` | a conflict: a sub-category name you already use, too many sub-categories, a goal that's already set, goals for a past week |
| `413` | request body over 64 KB |
| `429` | rate limit; wait for `Retry-After` seconds |
| `500` | something broke on our side; try again later |
