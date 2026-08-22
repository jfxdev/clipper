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
  per-paste size limit, and per-IP rate limiting. Text only — no file
  attachments.

## Quick start

```bash
# start a local Redis (or point REDIS_ADDR at one you already have)
docker compose up -d redis

# build the frontend and copy it into the Go binary, then build the binary
make build-frontend
make build

STORE_BACKEND=redis REDIS_ADDR=localhost:6379 ./backend/clipper
```

Open `http://localhost:8080`.

## Configuration

All configuration is via environment variables (see
`backend/internal/config/config.go`):

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `STORE_BACKEND` | `memory` | `memory` \| `redis` \| `mongo` \| `dynamo` |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | `localhost:6379` / `""` / `0` | Redis backend |
| `MONGO_URI` / `MONGO_DATABASE` / `MONGO_COLLECTION` | `mongodb://localhost:27017` / `clipper` / `pastes` | MongoDB backend |
| `DYNAMO_TABLE` / `DYNAMO_ENDPOINT` / `DYNAMO_REGION` | `clipper_pastes` / `""` / `us-east-1` | DynamoDB backend (`DYNAMO_ENDPOINT` for dynamodb-local) |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | `5` / `10` | Per-IP token bucket for `POST /api/paste` and `GET /api/paste/{id}` |
| `MAX_PASTE_SIZE_BYTES` | `2097152` (2MB) | Max size of the (already-encrypted) paste payload |
| `TRUST_PROXY` | `false` | Trust `X-Forwarded-For` for rate limiting; only enable behind a trusted reverse proxy |

## Development

```bash
# backend
cd backend && go test ./...              # unit tests (no external deps)
go test -tags=integration ./...          # + real redis/mongo/dynamodb-local

# frontend
cd frontend && npm install
npm run dev                              # dev server
npm test -- --run                        # crypto round-trip tests etc.
```

`docker-compose.yml` provides local Redis, MongoDB, and dynamodb-local for
running the integration test suite.
