# cinema-backend

Go backend for fazon.nexus/cinema: scrapes UK cinema showtimes (port of
`cinema-listings/cinemas.py`) and serves `listings.json` at `https://cinema-api.fazon.nexus`.

17 cinemas (Odeon ×6, Cineworld ×4, Vue ×5, Curzon Knutsford, Plaza Cinema Skipton) return 403 to
the Hetzner IP, so they are flagged `Relay` in `internal/registry` and scraped by `cinema-relay`
on the home PC, which uploads raw results to the server. The server scrapes the other 40 itself.

```
cmd/cinemad        server: scrape loop + HTTP API
cmd/cinema-relay   home PC: scrape relay cinemas, upload
cmd/cinema         local CLI for testing / parity
internal/scrape    impersonating HTTP client (tls-client, Chrome 146), Show model, tag normalisation, runner
  chains/          Vista (Odeon, Curzon), Webedia (Cineworld, Everyman, Showcase), Vue, Picturehouse, The Light, Reel
  ticketing/       Indy GraphQL, TicketSource, Spektrix (+ their venues)
  venues/          Savoy CCM, Plaza Skipton, Penistone, Pictureville, Stockport Plaza, Tideswell
internal/registry  the 57 cinemas (name, chain, URL, Relay flag, fetcher)
internal/listings  build(): window, dedupe, Classic rule, runtime medians, end estimates, travel matrix; places
internal/store     per-cinema last-good results on disk
deploy/            systemd units, env example, Caddy snippet
```

## Build

```sh
go test ./... && go vet ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/cinemad ./cmd/cinemad
go build -trimpath -o bin/cinema-relay ./cmd/cinema-relay
go build -o bin/cinema ./cmd/cinema
```

Local scrape (same output as `python3 cinemas.py`):

```sh
bin/cinema scrape [--only odeon,vue] [--days 16] [--out listings.json] [--raw-out raw.json] [--relay|--server]
bin/cinema build --raw raw.json --out listings.json   # rebuild from a raw dump, no network
```

## API

- `GET /v1/listings.json` — today..today+6 (Europe/London), the frontend's schema plus per-cinema
  `updated` (last successful scrape, RFC 3339) and `stale` (no success in 12 h); `generated` is RFC 3339.
  For privacy there is no `home` and no Home row in `travel` (the page adds home legs itself from
  `travel.estimate` and the visitor's postcode). gzip, ETag/304,
  `Cache-Control: public, max-age=300`, CORS for `CINEMAD_ORIGINS` (default fazon.nexus, localhost:1313, 127.0.0.1:any).
- `POST /v1/ingest` — `Authorization: Bearer <token>`, body (optionally gzip)
  `{"results":[{"name","shows":[{film,start,screen,formats,url,end,duration}],"error","fetched"}]}`.
  Only relay cinemas unless `?any=1`. A result with an error keeps the cinema's previous shows.
- `GET /healthz`

## Deploy (Hetzner)

```sh
# DNS: A record cinema-api.fazon.nexus -> the server's IP
openssl rand -hex 32                              # ingest token
scp bin/cinemad deploy/cinemad.service deploy/env.example root@SERVER:/tmp/
ssh root@SERVER '
  useradd --system --no-create-home --shell /usr/sbin/nologin cinemad
  install -m 0755 /tmp/cinemad /usr/local/bin/cinemad
  install -d -m 0750 -o root -g cinemad /etc/cinemad
  install -m 0640 -o root -g cinemad /tmp/env.example /etc/cinemad/env   # then edit: set CINEMAD_INGEST_TOKEN
  install -m 0644 /tmp/cinemad.service /etc/systemd/system/cinemad.service
  systemctl daemon-reload && systemctl enable --now cinemad
  journalctl -u cinemad -n 50 --no-pager
  curl -s localhost:8090/healthz'
```

Add `deploy/Caddyfile.snippet` to `/etc/caddy/Caddyfile`, then `systemctl reload caddy`.
Upgrade: copy the new binary to `/usr/local/bin/cinemad`, `systemctl restart cinemad` (data in
`/var/lib/cinemad` survives).

## Relay (home PC)

```sh
install -Dm0755 bin/cinema-relay ~/.local/bin/cinema-relay
install -Dm0600 /dev/stdin ~/.config/cinema-relay/token <<< 'THE-TOKEN'
cinema-relay --dry-run                            # scrape only, print a summary
cinema-relay                                      # scrape + upload; exits 1 if the upload fails
cp deploy/cinema-relay.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now cinema-relay.timer
loginctl enable-linger "$USER"                    # optional: run while logged out
journalctl --user -u cinema-relay -n 30
```

`--all` scrapes every cinema, `--only x,y` a subset (non-relay cinemas are sent with `?any=1`).

## Adding a cinema

1. Write or reuse a fetcher (`scrape.Fetcher`: `func(*scrape.Session, from, to time.Time) ([]scrape.Show, error)`)
   in the package for its platform. Use `s.GetOK`/`GetJSON`/`PostJSON` (Chrome-impersonating, one
   cookie jar per cinema run); start/end are naive UK wall-clock `2006-01-02T15:04`.
2. Add it to `internal/registry/registry.go` (set `Relay: true` if it 403s from the server:
   `curl -sI <url>` on the box).
3. Add its coordinates to `listings.Places` (the registry test fails otherwise).
4. `go test ./... && bin/cinema scrape --only <name>`, then deploy the server (and the relay if it's a relay cinema).

## Notes

- Expected values in the unit tests (`tags_cases_test.go`, `title_cases_test.go`,
  `listings/testdata/rules_*.json`, the ticketing/venues tables) were generated from the Python
  reference functions.
- Travel times are always the straight-line estimate; the page fetches Google times itself with
  the user's own key.
