# clipper

A minimal, PrivateBin-style encrypted text sharing service. All encryption
happens client-side (AES-256-GCM via the browser's WebCrypto API) — the
server only ever stores an opaque ciphertext blob and never sees the
plaintext or the decryption key, which travels solely in the URL fragment
(`#...`) and is never sent over the network.

- **Backend**: Go, single binary, pluggable storage (`Store` interface with
  Redis, MongoDB, DynamoDB, and an in-memory implementation for tests).
- **Frontend**: React + TypeScript + Vite + Tailwind CSS + shadcn/ui,
  embedded into the Go binary via `go:embed` so one process serves both the
  API and the static app.
- **Features**: time-based expiration, burn-after-read, an optional
  additional password (mixed into the encryption key via PBKDF2), a
  per-paste size limit, and per-client rate limiting and storage quotas.
  Text only — no file attachments.

## Security model

The short version, with the full statement (and the known limits) in
[SECURITY.md](SECURITY.md):

- **The server cannot read a paste.** Encryption happens in the browser;
  the key travels only in the URL fragment, which is never sent over the
  network.
- **A paste ID grants nothing on its own.** Reads require a *read token* —
  `base64url(SHA-256(key fragment))` — sent in the `X-Paste-Read-Token`
  header. An ID that leaks into a proxy log or a chat preview cannot be
  used to fetch the ciphertext, and cannot be used to destroy a
  burn-after-read message either: the token is checked inside the same
  atomic operation that performs the burn.
- **The blob header is authenticated.** The format version and PBKDF2
  iteration count travel outside the ciphertext but are bound in as AES-GCM
  additional data, so a hostile server cannot rewrite them unnoticed.
- **Message length is padded** to 256-byte blocks, so the stored size does
  not reveal how short a secret is.
- **Every response carries a strict CSP** (`default-src 'none'`,
  `script-src 'self'`, no inline script), `no-store`, `no-referrer` and
  `frame-ancestors 'none'`. An XSS here would hand over the decryption key
  in `location.hash`, so the policy is the load-bearing control, not a
  formality.
- **Pastes must expire.** `MAX_EXPIRE_SECONDS` caps the retention window
  and there is no "never" option.

Report a vulnerability through the [private advisory
form](https://github.com/jfxdev/clipper/security/advisories/new), never a
public issue. `/.well-known/security.txt` carries the same pointer.

## Quick start

```bash
# start a local Redis (or point REDIS_ADDR at one you already have)
docker compose up -d redis

# build the frontend and copy it into the Go binary, then build the binary
make build-frontend
make build

STORE_BACKEND=redis REDIS_ADDR=localhost:6379 REDIS_PASSWORD=devpassword ./backend/clipper
```

Open `http://localhost:8080`.

## Configuration

All configuration is via environment variables (see
`backend/internal/config/config.go`):

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `STORE_BACKEND` | `memory` | `memory` \| `redis` \| `mongo` \| `dynamo` |
| `MODE` | `""` | Operations this instance serves: `read` (retrieval only) \| `write` (creation only) \| `""` (both). See [Split read/write instances](#split-readwrite-instances) |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | `localhost:6379` / `""` / `0` | Redis backend |
| `MONGO_URI` / `MONGO_DATABASE` / `MONGO_COLLECTION` | `mongodb://localhost:27017` / `clipper` / `pastes` | MongoDB backend |
| `DYNAMO_TABLE` / `DYNAMO_ENDPOINT` / `DYNAMO_REGION` | `clipper_pastes` / `""` / `us-east-1` | DynamoDB backend (`DYNAMO_ENDPOINT` for dynamodb-local) |
| `REDIS_TLS` | `false` | Connect to Redis over TLS (certificate verification is always on) |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | `5` / `10` | Per-client token bucket. IPv6 clients are bucketed per `/64`, not per address |
| `GLOBAL_RATE_LIMIT_RPS` / `GLOBAL_RATE_LIMIT_BURST` | `200` / `400` | Ceiling shared by all clients, so a flood spread over many addresses is still capped |
| `RATE_LIMIT_MAX_CLIENTS` | `100000` | Cap on tracked client buckets; beyond it new clients share an overflow bucket instead of allocating |
| `QUOTA_PASTES_PER_DAY` / `QUOTA_BYTES_PER_DAY` | `200` / `67108864` (64MB) | Rolling 24h storage allowance per client |
| `MAX_PASTE_SIZE_BYTES` | `2097152` (2MB) | Max size of the (already-encrypted) paste payload |
| `MAX_EXPIRE_SECONDS` | `2592000` (30d) | Longest lifetime a paste may request. Pastes must expire; "never" is not offered |
| `TRUSTED_PROXIES` | `""` | Comma-separated CIDRs. `X-Forwarded-For` is only read when the direct peer is in this set |
| `TRUST_PROXY` | `false` | Blunt alternative: trust `X-Forwarded-For` from any direct peer. Only safe when nothing but the reverse proxy can reach the port |
| `HSTS_MAX_AGE_SECONDS` | `0` | Emit `Strict-Transport-Security` when > 0. Turn on once TLS terminates in front |

Startup logs a warning for configurations that are valid but risky in
production (in-memory store, `X-Forwarded-For` handling that will either
collapse or be spoofable, plaintext Redis over a network).

### Split read/write instances

`MODE` restricts which operations a single instance serves, so ingest and
serving can be scaled and exposed separately over one shared store:

- `MODE=write` — only `POST /api/paste` (creation). Reads return `403`.
- `MODE=read` — only `GET /api/paste/{id}` (retrieval). Creation returns `403`.
- `MODE=""` (default) — both.

`GET /api/health` and `GET /api/config` are served in every mode. A disabled
operation answers `403` with a generic body that never names the mode, so the
response does not reveal the deployment topology; the frontend localizes the
condition from the status code. `GET /api/config` reports capabilities
(`{"createEnabled":…,"readEnabled":…}`), not the mode name, so the SPA can
render the right UI up front — e.g. a read-only instance shows a notice in
place of the create form instead of letting a submit fail. Split mode only
makes sense over a **shared** backend (`redis`,
`mongo`, `dynamo`) — with `memory` each process has its own store, so a
write-only node's pastes are invisible to a read-only node, and startup warns
about this.

## Development

```bash
# backend
cd backend && go test ./...              # unit tests (no external deps)
go test -race ./...                      # burn-after-read is a concurrency contract
go test -tags=integration ./...          # + real redis/mongo/dynamodb-local

cd ..                                    # the Make targets live at the root
make test-fuzz                           # fuzz the ID/envelope validators
make audit                               # govulncheck + npm audit

# frontend
cd frontend && npm install
npm run dev                              # dev server
npm test -- --run                        # crypto round-trip tests etc.
```

`docker-compose.yml` provides local Redis, MongoDB, and dynamodb-local for
running the integration test suite. All three are bound to `127.0.0.1` and
the ones that support it require a password, so a laptop on an untrusted
network is not exposing an open database.

## Container image

`make docker-build` produces a `FROM scratch` image: the only files in it
are the static binary and a CA bundle. There is no shell, no package
manager and no libc to pivot to, and the health check is the binary probing
itself (`clipper -healthcheck`) because there is no `curl` to call. Base
images are pinned by digest and the compose reference deployment runs it
read-only, with all capabilities dropped and `no-new-privileges` set.
