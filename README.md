# Flash Sale Inventory Reservation

A reservation service for a flash sale: hold stock atomically in PostgreSQL, expire the hold after
five minutes unless it is confirmed, and hand the units back to the available pool when it does.
Go and Gin on the backend, React and Vite on a small dashboard, one compose file to start all three.

## Run everything

Requires Docker with the Compose v2 plugin. Go 1.26 and Node are only needed if you want to run the
services outside of Docker.

```bash
cp .env.example .env
docker compose up --build
```

| What            | Where                        |
| --------------- | ---------------------------- |
| Dashboard       | http://localhost:5173        |
| API             | http://localhost:8080        |
| Health / ready  | `/healthz` and `/readyz`     |
| PostgreSQL      | localhost:5433 (see `.env`)  |

Migrations in `db/migrations` run automatically the first time the data volume is created. After
editing a migration, recreate the volume so `initdb` picks the change up:

```bash
docker compose down -v && docker compose up --build
```

Stop with `Ctrl+C`, or `docker compose down`. The backend traps SIGTERM and drains in-flight
requests before closing the pool; the log ends with `shutdown complete`.

The seeded catalogue is `item_4021` (100 units), `item_4022` (40), `item_5555` (5) and `item_0000`
(0). Any other id is the "not found" case.

## Run the parts separately

Postgres on its own:

```bash
docker compose up -d postgres
```

Backend on the host. It reads its configuration from the environment only, and it talks to the
published port (5433), not the container port:

```bash
cd backend
export DATABASE_URL="postgres://flashsale:flashsale@localhost:5433/flashsale?sslmode=disable"
go run ./cmd/server
```

PowerShell: `$env:DATABASE_URL = "postgres://flashsale:flashsale@localhost:5433/flashsale?sslmode=disable"`.

Frontend on the host. Vite proxies `/api` to `http://localhost:8080`, so no CORS configuration is
involved and nothing needs rebuilding when the backend moves:

```bash
cd frontend
npm install
npm run dev
```

## Endpoints

| Method | Path                          | Purpose                     |
| ------ | ----------------------------- | --------------------------- |
| POST   | `/api/v1/inventory/reserve`   | hold units for a user       |
| POST   | `/api/v1/inventory/confirm`   | commit a hold, spend stock  |
| GET    | `/api/v1/inventory/stock`     | current breakdown for an id |

Errors come back as `{"status":"error","code":"...","message":"...","request_id":"..."}`, with a
`details` object naming the offending field when the request itself was malformed.

Reserve:

```bash
curl -s -X POST localhost:8080/api/v1/inventory/reserve \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"usr_9981","item_id":"item_4021","quantity":2}'
```

```json
{
  "status": "success",
  "reservation_id": "res_883291",
  "item_id": "item_4021",
  "quantity": 2,
  "expires_at": "2026-07-20T16:35:00Z"
}
```

Confirm, then read the stock:

```bash
curl -s -X POST localhost:8080/api/v1/inventory/confirm \
  -H 'Content-Type: application/json' \
  -d '{"reservation_id":"res_883291"}'

curl -s "localhost:8080/api/v1/inventory/stock?item_id=item_4021"
```

The failures worth trying, each with the status it returns:

```bash
# 409 INSUFFICIENT_STOCK, the item exists but is fully held
curl -s -X POST localhost:8080/api/v1/inventory/reserve \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"usr_1","item_id":"item_0000","quantity":1}'

# 404 ITEM_NOT_FOUND
curl -s -X POST localhost:8080/api/v1/inventory/reserve \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"usr_1","item_id":"item_ghost","quantity":1}'

# 422 INVALID_INPUT, details point at quantity
curl -s -X POST localhost:8080/api/v1/inventory/reserve \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"usr_1","item_id":"item_4021","quantity":0}'

# 400 INVALID_JSON
curl -s -X POST localhost:8080/api/v1/inventory/reserve -H 'Content-Type: application/json' -d 'nope'

# 404 RESERVATION_NOT_FOUND
curl -s -X POST localhost:8080/api/v1/inventory/confirm \
  -H 'Content-Type: application/json' \
  -d '{"reservation_id":"res_000000"}'

# 409 ALREADY_CONFIRMED on the second call, and RESERVATION_EXPIRED once the hold lapsed
curl -s -X POST localhost:8080/api/v1/inventory/confirm \
  -H 'Content-Type: application/json' \
  -d '{"reservation_id":"res_883291"}'

# 422, the query parameter is missing
curl -s localhost:8080/api/v1/inventory/stock
```

Waiting five minutes is a slow way to see the expiry path. Shorten it instead:

```bash
RESERVATION_TTL=20s go run ./cmd/server
```

## Tests

```bash
cd backend
go test -v ./...
```

The unit, contract and configuration tests need no database. The integration and stress tests
connect to `DATABASE_URL`, or to the compose Postgres on `localhost:5433` if that is unset, and
skip with a printed reason when neither is reachable. Bring the stack up first if you want the
whole suite to run rather than partly skip:

```bash
docker compose up -d postgres
go test -v ./internal/inventory/
```

`-race` needs a C toolchain, which a Windows host usually does not have. The Debian Go image ships
one, so run the exact command from the brief inside a container on the compose network:

```bash
docker run --rm --network flash-sale-inventory_default \
  -v "$PWD:/src" -w /src \
  -e DATABASE_URL="postgres://flashsale:flashsale@postgres:5432/flashsale?sslmode=disable" \
  golang:1.26 go test -race -count=1 ./...
```

`stress_test.go` is the one that matters for the brief's no-over-selling requirement: sixty
callers race for one hundred units, and the test then asks Postgres whether the holds, the
counters and the reservation rows still agree, and whether any caller got a 500. A second test
races confirmations against the expiry sweep to prove the same unit is never spent twice.

## Configuration

| Variable          | Default | Meaning                                          |
| ----------------- | ------- | ------------------------------------------------ |
| `DATABASE_URL`    | none    | required; the server refuses to start without it |
| `HTTP_ADDR`       | `:8080` | listen address                                   |
| `GIN_MODE`        | release | Gin log mode                                     |
| `RESERVATION_TTL` | `5m`    | how long a hold survives without confirmation    |
| `REAPER_INTERVAL` | `5s`    | how often expired holds are swept                |
| `REAPER_BATCH`    | `500`   | units of work per sweep                          |
| `STATEMENT_TIMEOUT` | `5s`  | per-statement limit sent to Postgres             |
| `HANDLER_TIMEOUT` | `10s`   | budget for a write that must finish              |
| `SHUTDOWN_TIMEOUT` | `15s`  | drain budget; must exceed `STATEMENT_TIMEOUT`    |
| `MAX_CONNS`       | `25`    | pool size                                        |

`POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `POSTGRES_HOST_PORT` and `FRONTEND_HOST_PORT`
are read by Compose. `.env.example` holds working placeholders; `.env` is git-ignored.

## Layout

```
backend/
  cmd/server/          wiring, signals, shutdown order
  internal/config/     environment
  internal/inventory/  model, service, repository, handlers, reaper, tests
  internal/platform/pg connection pool
  internal/server      router and health probes
  internal/transport/httpx  request id, error envelope, readiness
db/migrations/         schema and seed, applied by the postgres container
frontend/              React + Vite + TypeScript dashboard
docker-compose.yaml
```

`ARCHITECTURE.md` covers the locking strategy, what happens at ten replicas, and the trade-offs.
