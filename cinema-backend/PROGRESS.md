# cinema-backend — progress log

Go port of `/home/faizan/Language Learning/cinema-listings/cinemas.py` (+ `travel.py` estimate path).
Rules: do NOT edit the Python; do NOT touch anything in the Blog repo outside `cinema-backend/`;
no commits/pushes/deploys. Keep `go build ./... && go vet ./...` green between steps (unfinished
work = TODO stubs returning `errTODO`).

## Layout / decisions
- `go.mod` module `fazon.nexus/cinema`; deps: bogdanfinn/tls-client (+fhttp), PuerkitoBio/goquery.
- `internal/scrape/` core: `client.go` (Session = per-cinema tls-client Chrome_146 + Chrome headers,
  cookie jar; `Get/Post/GetOK/PostOK/GetJSON/PostJSON`, opts `Query/Header/Form/JSONBody/Timeout`;
  run-wide `Session.Memo(key, fn)` for shared caches like Picturehouse runtimes / TicketSource cfg),
  `show.go` (Show model w/ null JSON, `Normalize`, `ToInt`=_int, `Mins`=_mins, dates: midnight-UTC
  `time.Time`, `Days`, `InWindow`, `London`), `tags.go` (`Tag/Tags`=tag/tags, `Premium`, `Camel`, `Uniq`),
  `html.go` (`Doc`, `Text(sel, sep, strip)`=BeautifulSoup get_text, `TextS`, `Attr`, `Squash`),
  `run.go` (`Fetcher`, `Cinema{Relay}`, `Result` = relay upload format, `Run` ≤8 workers, `FetchOne`).
- Fetchers in sub-packages (so parallel work never breaks each other's build):
  - `internal/scrape/chains/` — Vista (Odeon/Curzon), Webedia, Vue, Picturehouse, Light, Reel — LEAD
  - `internal/scrape/ticketing/` — Indy GraphQL, TicketSource (5 venues incl. M&S film society, Peak FS), Spektrix (5 venues) — subagent B
  - `internal/scrape/venues/` — Savoy CCM (3), Plaza Skipton, Penistone, Pictureville, Stockport Plaza, Tideswell — subagent C
- `internal/registry/` — the 57 cinemas in Python order; Relay=true for 6 Odeon, 4 Cineworld, 5 Vue,
  Curzon Knutsford, Plaza Cinema Skipton (17). `Select(only, relay)`, `ByName`.
- `internal/listings/` — `Build()` port (DONE), `places.go` (PLACES/HOME/ESTIMATE generated from travel.py).
- `cmd/cinema` — `scrape [--only] [--days] [--from] [--out] [--raw-out] [--relay|--server]`, `build --raw`.
- `internal/store` (DONE): per-cinema `<slug>.json` Record{shows (last good), updated, error, attempted, source}; atomic writes; `Apply(result, source)`, `Entries()`+generation, `Prune`.
- `cmd/cinemad` (DONE, tested locally): env/flags CINEMAD_*; scrape loop (non-relay, start + every 2h); GET /v1/listings.json (gzip, ETag=content hash, 304, Cache-Control, CORS, `generated`=newest data time), OPTIONS preflight, POST /v1/ingest (Bearer, gzip body ok, size limit, registry+relay validation, `?any=1`), GET /healthz; graceful shutdown OK.
- `cmd/cinema-relay` (DONE, tested vs local cinemad): relay cinemas / --all / --only (adds ?any=1 if non-relay), gzip upload w/ retries, token env/file, --dry-run; exit 1 on upload failure (401 verified).
- `deploy/` (DONE): cinemad.service (hardened), env.example, Caddyfile.snippet, cinema-relay.{service,timer} (user units). README.md (DONE).
- Core `Resp.Doc()` honours Latin-1 Content-Type (moved from venues/penistone `docByCharset`); all fetchers use `r.Doc()`.

## Public API schema changes vs Python listings.json (frontend requirements, coordinator 2026-10-07)
- PRIVACY: no top-level `home`, no `travel.home`, no "Home" row/column in `travel.places/mins/coords`
  (cinemas only; `travel.estimate` kept so the page computes home legs from the visitor's postcode).
  HOME/postcode removed from the Go code and from test golden data entirely.
- `generated` is RFC 3339 with London offset (`2026-10-07T14:05:00+01:00`); server sets it to the newest
  data time (so unchanged data keeps its ETag). Per-cinema `updated` RFC 3339 London offset, `stale` bool.
- CORS default origins: `https://fazon.nexus,http://localhost:1313,http://127.0.0.1:*` (`host:*` = any port).

## Deliberate deviations (bug fixes)
- Film titles are HTML-unescaped in Build (The Light had `We&#39;re…`, `Minions &amp; Monsters`).
- Shows/films iterate in registry order (Python used thread completion order → title-vote ties nondeterministic).
- Fetchers use status-checked requests where Python silently parsed error pages (keeps last-good data on the server).
- Odeon Trafford Centre slug `trafford-centre` 404s (Python only worked because the 404 page also embeds the Vista token) → `manchester-trafford-centre` (registry URL + show URLs). Vista token page fetch tolerates non-2xx.

## Parity tooling (scratchpad, not in repo)
`P=/tmp/claude-1000/-home-faizan-Language-Learning/a407f989-e1ba-4f2e-87ab-05d71223d1a5/scratchpad/parity`
- `python3 $P/pydump.py OUTDIR [--only x]` → OUTDIR/raw.json + listings.json (Python reference)
- `go run ./cmd/cinema scrape --only x --raw-out $P/go/raw.json --out ''`
- `python3 $P/cmpraw.py PY_RAW GO_RAW [--only substr] [-v]` → per-cinema counts / film sets / field diffs
- `python3 $P/cmpbuild.py A.json B.json` → exact listings comparison for the same raw input

## Status
| Component | Status | Owner |
|---|---|---|
| core scrape pkg | done, builds | lead |
| registry | done | lead |
| listings.Build | done; build parity on identical raw input: 9833/9833 shows identical except 20 The Light shows whose titles had HTML entities (intended fix); travel matrix identical | lead |
| chains (Vista/Webedia/Vue/PH/Light/Reel) | DONE: all 36 chain cinemas identical to Python (counts, film sets, 0 field diffs; Trafford only URL fix) | lead |
| ticketing (Indy/TicketSource/Spektrix) | DONE (see B notes) | subagent B |
| venues (Savoy/Skipton/Penistone/Pictureville/StockportPlaza/Tideswell) | DONE (see C notes) | subagent C |
| store / cinemad / relay | DONE, smoke-tested locally (9833 shows served after relay upload + server scrape) | lead |
| deploy / README / unit tests | DONE; `go test ./...` + `go vet ./...` green | lead |

## Next steps
ALL DONE. Remaining for the user: review, commit, deploy (README). Not exercised: systemd units under real systemd on the server.

## Parity results
Full run 2026-10-07, Python and Go concurrently: all 57 cinemas identical in-window counts (9835 raw / 9833
after dedupe each), identical film sets, 0 raw field diffs except Odeon Trafford Centre URLs (slug fix).
Final listings.json: identical shows except 20 The Light shows (HTML-entity titles, intended fix) + the
Trafford URLs; 1 film-title tie ("All Rivers ... To/to The/the Sea") resolved by registry order instead of
thread completion order. Travel matrix identical.

## Subagent B notes (ticketing) — B edits ONLY this section, with the Edit tool
**Status: DONE** — all 13 ticketing cinemas ported; stubs.go deleted; `go test ./internal/scrape/ticketing/` passes.
Files: `ticketing/indy.go` (Indy GraphQL), `ticketsource.go` (TS search + 5 venues + M&S film society),
`spektrix.go` (generic `spektrixVenue.fetch` + 5 venue configs), `util.go` (`truthy`, `str`, ordered-key `obj`,
`headRunes`, `upperFirst`), `ticketing_test.go` (table tests, expectations generated from the Python functions).

Parity (raw, Python vs Go run back-to-back 2026-10-07; zero field diffs everywhere):
| cinema | 7d py/go | 60d py/go |
|---|---|---|
| Savoy / Rex / Northern Light | 14/14, 39/39, 69/69 | 94/94, 221/221, 115/115 |
| Leigh / Southport Bijou / Partington | 2/2, 4/4, 0/0 | 27/27, 63/63, 2/2 (incl. a Peak-FS "Subtitled") |
| Altrincham Little / Stockport Garrick | 0/0, 0/0 | 2/2, 0/0 → 365d: 9/9, 2/2 |
| HOME / Showroom / Storyhouse | 114/114, 115/115, 30/30 | 220/220, 419/419, 124/124 |
| Altrincham Garrick / Buxton | 0/0, 1/1 | 1/1, 4/4 |

Quirks / deviations:
- Spektrix `_spx_formats` iterates instance keys in JSON order (tag order matters) → instances decode into
  `obj` (map + key order). Python `v in (None, False, ...)` also treats `0` as unset — kept.
- TicketSource cfg is `s.Memo("ts-cfg")`; any failure (or empty field) → hard-coded fallback, never an error.
- M&S film society: an unparseable month / impossible day now skips that item (Python's strptime/date
  would raise and lose the whole society listing). Not hit by current data.
- Indy: if `"use strict"` isn't found before the ids, search from the bundle start (Python sliced from -1).
- Status-checked requests everywhere Python parsed bodies unchecked (Indy home page/bundle, M&S page,
  Peak FS page — the last stays best-effort, Spektrix plan names stay best-effort).
- Altrincham Little Theatre: error when both sources fail, or one fails and there are no rows (as Python);
  the error message joins both errors.

## Subagent C notes (venues) — C edits ONLY this section, with the Edit tool
All 8 venues ported (stubs.go deleted): `formats.go` (svFormats/fmtTags/nearestYear/guessYear/monthAbbr/urljoin
helpers), `savoy.go` (Cultplex/HebdenBridge/PlazaCrosby), `skipton.go`, `penistone.go`, `pictureville.go`,
`stockportplaza.go`, `tideswell.go`.
Parity 2026-10-07 (exact ordered field-by-field match of every raw show, Python vs Go):
| cinema | 7d py/go | 60d py/go |
|---|---|---|
| Cultplex | 12/12 | 52/52 |
| Hebden Bridge | 16/16 | 63/63 |
| Liverpool Plaza (Crosby) | 54/54 | 91/91 |
| Plaza Skipton | 16/16 | 24/24 |
| Penistone (Live Performance rows dropped inside `Penistone`) | 10/10 | 56/56 |
| Pictureville | 51/51 | 164/164 |
| Stockport Plaza | 1/1 | 7/7 |
| Tideswell | 2/2 | 6/6 |
Quirks: Penistone serves `charset=ISO-8859-1` → `docByCharset` decodes Latin-1 like Python requests (core
`scrape.Doc` assumes UTF-8; without it "Comédie" broke). Python crashes (ValueError/AttributeError) on bad
dates/months/missing card parts are kept as returned errors. Ticketing pkg was mid-edit during testing, so
parity used `go build -overlay` with a registry copy minus ticketing (scratch `$P/c/overlay.json`); re-verified
7d with plain `go run` once the tree built. `venues_test.go` = table tests for svFormats/fmtTags/nearestYear/
guessYear/spFilm/monthAbbr (expected values from the Python). STATUS: DONE; build/vet/test green.
