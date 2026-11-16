# EventPulse

A cloud-native webhook delivery and event streaming platform. Applications publish events to a REST API. EventPulse delivers them to registered HTTPS endpoints with signed requests, retries failures with exponential backoff, and exposes delivery health in a dashboard and in Prometheus/Grafana.

## Features

- **Ingestion API** (Go, `net/http`): register apps and endpoints, publish events. Each event and its delivery fan-out commit in one transaction.
- **Concurrent worker pool**: configurable goroutine pool, 5s request timeout, graceful shutdown that finishes in-flight deliveries.
- **Retries**: exponential backoff with jitter, 5 attempts by default, then a terminal `failed` state. Failed deliveries can be redelivered manually.
- **Request signing**: `X-EventPulse-Signature` header, HMAC-SHA256 over `<timestamp>.<body>`, per-endpoint secrets that can be rotated.
- **PostgreSQL 16**: type-safe queries via `sqlc`, JSONB payloads, `deliveries` range-partitioned by month with automatic partition creation.
- **Redis**: per-application ingest rate limiting, per-endpoint delivery rate limiting, and a lock for partition maintenance. It fails open if Redis is down.
- **Dashboard** (Next.js, TypeScript, Tailwind): overview charts, endpoint manager with signing secrets, delivery log with payload inspection and a "Redeliver" button.
- **Observability**: Prometheus metrics, a provisioned Grafana dashboard, and sample alert rules.
- **Deployment**: multi-stage Dockerfiles, Docker Compose, and Kubernetes manifests (Deployments, Services, HPA, ServiceMonitor).

## Architecture

```
Client ──POST /v1/applications/{id}/events──▶ API ──tx──▶ events + deliveries (pending)
                                                              │  lease-based claim
Dashboard (Next.js) ──/api proxy──▶ API         Worker pool ◀─┘  (FOR UPDATE SKIP LOCKED)
                                                   │ HMAC-signed POST, 5s timeout
                                                   ▼
                              success │ retry (backoff + jitter) │ failed after max attempts

API + workers ──/metrics──▶ Prometheus ──▶ Grafana
```

Workers claim rows with `UPDATE ... WHERE (id, created_at) IN (SELECT ... FOR UPDATE SKIP LOCKED)`, which pushes `next_retry_at` into the future (a lease). Any number of workers can run concurrently. No transaction stays open during HTTP calls, and a crashed worker's rows become due again when the lease expires.

## Prerequisites

- Docker with Compose v2 (required to run the stack)
- Go 1.26+ and Node.js 20+ (only for development outside Docker)

## Quick start

```powershell
docker compose up -d --build                    # postgres, redis, api, worker, frontend, prometheus, grafana
docker compose --profile demo up -d receiver    # optional demo webhook target
```

| Service    | URL                                  |
|------------|--------------------------------------|
| Dashboard  | http://localhost:3100                |
| API        | http://localhost:8080                |
| Prometheus | http://localhost:9091                |
| Grafana    | http://localhost:3101 (admin / admin) |
| Postgres   | localhost:5433                       |
| Redis      | localhost:6380                       |

The API applies database migrations on startup. All host ports can be changed in `.env` (copy `.env.example`).

### Send your first event

```powershell
$b = "http://localhost:8080"; $j = "application/json"
$app = Invoke-RestMethod -Method Post "$b/v1/applications" -ContentType $j -Body '{"name":"Demo"}'
$ep  = Invoke-RestMethod -Method Post "$b/v1/applications/$($app.id)/endpoints" -ContentType $j -Body '{"url":"http://receiver:9000/hook"}'

# Start the demo receiver with the endpoint's secret so it verifies signatures
$env:RECEIVER_SECRET = $ep.secret; $env:RECEIVER_FAIL_PERCENT = "0"
docker compose --profile demo up -d --force-recreate receiver

Invoke-RestMethod -Method Post "$b/v1/applications/$($app.id)/events" -ContentType $j `
  -Body '{"event_type":"order.created","payload":{"order_id":42}}'
docker compose logs receiver --tail 5
```

Set `RECEIVER_FAIL_PERCENT` to e.g. `50` to watch retries in the dashboard.

## API reference

| Method and path | Purpose |
|---|---|
| `POST /v1/applications` | Create an application `{name}` |
| `GET /v1/applications`, `GET /v1/applications/{id}` | List / fetch applications |
| `POST /v1/applications/{id}/endpoints` | Register an endpoint `{url}`. The signing secret is generated and returned |
| `GET /v1/endpoints?app_id=` | List endpoints |
| `PATCH /v1/endpoints/{id}` | Update `{url?, is_active?}` |
| `POST /v1/endpoints/{id}/rotate-secret` | Generate a new signing secret |
| `DELETE /v1/endpoints/{id}` | Delete an endpoint and its deliveries |
| `POST /v1/applications/{id}/events` | Publish `{event_type, payload}`. Responds `202 {event_id, deliveries}` |
| `GET /v1/deliveries?app_id=&status=&limit=&offset=` | Delivery log (`status`: pending, success, failed) |
| `POST /v1/deliveries/{id}/redeliver` | Reset a delivery to pending with 0 attempts |
| `GET /v1/stats?window=60` | Dashboard aggregates over the last N minutes (max 1440) |
| `GET /healthz`, `/readyz`, `/metrics` | Liveness, readiness (checks Postgres), Prometheus metrics |

Errors are returned as `{"error": "message"}`. When `API_KEY` is set, `/v1/*` requires `Authorization: Bearer <key>`. Ingestion returns `429` with `Retry-After` when the per-application rate limit is exceeded.

## Verifying webhook signatures

Each delivery is a `POST` with a JSON body and these headers:

| Header | Value |
|---|---|
| `X-EventPulse-Signature` | `t=<unix seconds>,v1=<hex HMAC-SHA256>` |
| `X-EventPulse-Event` | Event type |
| `X-EventPulse-Delivery-Id` | Delivery UUID (use it to de-duplicate) |

Body: `{"id": "<event id>", "type": "<event type>", "data": <your payload>}`.

To verify, compute `HMAC_SHA256(secret, "<t>.<raw body>")`, compare with `v1` in constant time, and reject timestamps older than a few minutes to prevent replay. A reference implementation is in `backend/cmd/receiver/main.go`.

## Delivery semantics

- **At-least-once.** Any 2xx response is success. Anything else, including network errors and timeouts, is retried. Receivers should de-duplicate on `X-EventPulse-Delivery-Id`.
- **Backoff:** `BASE_BACKOFF * 2^(n-1)`, capped at `MAX_BACKOFF`, with jitter uniformly in `[d/2, d]`. After `MAX_ATTEMPTS` the delivery becomes `failed`.
- **Redirects are not followed.**
- **SSRF protection:** when `ALLOW_PRIVATE_TARGETS=false`, connections to loopback, private, link-local and multicast addresses are refused after DNS resolution. Docker Compose sets it to `true` so the demo receiver works. Kubernetes sets it to `false`.
- **Disabled endpoints:** deliveries already queued for an endpoint that is later disabled are marked `failed`.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | required | Postgres connection string |
| `REDIS_URL` | empty | Redis URL. Rate limiting and locks are disabled if empty |
| `HTTP_ADDR` | `:8080` | API listen address |
| `METRICS_ADDR` | `:9090` | Worker metrics and health address |
| `API_KEY` | empty | Bearer token required for `/v1/*` |
| `MIGRATE_ON_START` | `false` | Run migrations when the API starts (Compose and Kubernetes enable it) |
| `WORKER_CONCURRENCY` | `32` | Goroutines delivering in parallel per worker |
| `DISPATCH_TIMEOUT` | `5s` | Per-request timeout |
| `MAX_ATTEMPTS` | `5` | Attempts before a delivery is `failed` |
| `BASE_BACKOFF` / `MAX_BACKOFF` | `5s` / `1h` | Backoff bounds |
| `POLL_INTERVAL` | `500ms` | Idle polling interval |
| `CLAIM_LEASE` | `DISPATCH_TIMEOUT + 30s` | How long a claimed delivery is reserved |
| `INGEST_RATE_PER_SEC` | `200` | Events per second per application (0 = unlimited) |
| `ENDPOINT_RATE_PER_SEC` | `50` | Deliveries per second per endpoint (0 = unlimited) |
| `ALLOW_PRIVATE_TARGETS` | `false` | Allow delivery to private/loopback addresses |
| `CORS_ORIGIN` | empty | Value for `Access-Control-Allow-Origin` (not needed with the dashboard proxy) |
| `LOG_LEVEL` | `info` | `debug` enables request logging |

The frontend reads `BACKEND_URL` (default `http://localhost:8080`) and `API_KEY` at runtime. It proxies browser calls through `/api/*`, so the key never reaches the browser.

## Metrics

| Metric | Type | Notes |
|---|---|---|
| `eventpulse_events_ingested_total` | Counter | API |
| `eventpulse_deliveries_total{status="success\|failed"}` | Counter | Terminal results, exported by workers |
| `eventpulse_delivery_retries_total` | Counter | Failed attempts that were rescheduled |
| `eventpulse_webhook_dispatch_duration_seconds` | Histogram | Outbound request time, exported by workers |
| `eventpulse_queue_backlog_depth` | Gauge | Pending deliveries. Reported by API and worker, so query it with `max()` |
| `eventpulse_http_requests_total`, `eventpulse_http_request_duration_seconds` | Counter, Histogram | API requests by route |

The Grafana dashboard (`observability/grafana/dashboards/eventpulse.json`) is provisioned automatically. Sample alerts are in `observability/prometheus/alerts.yml`.

## Kubernetes

Postgres and Redis are expected to be managed services.

```bash
docker build -t <registry>/eventpulse-backend  ./backend
docker build -t <registry>/eventpulse-frontend ./frontend
# push both images, then update the image names in k8s/*.yaml

kubectl apply -f k8s/namespace.yaml
kubectl -n eventpulse create secret generic eventpulse-secrets \
  --from-literal=DATABASE_URL='postgres://user:pass@host:5432/eventpulse?sslmode=require' \
  --from-literal=REDIS_URL='redis://:pass@host:6379/0' \
  --from-literal=API_KEY="$(openssl rand -hex 32)"
kubectl apply -k k8s
```

- `hpa.yaml` scales workers from 2 to 20 replicas on 60% CPU. To scale on queue depth instead, use `hpa-queue-backlog.example.yaml` (needs prometheus-adapter).
- `servicemonitor.yaml` needs the Prometheus Operator CRDs. Remove it from `kustomization.yaml` if they are not installed, or add the label your Prometheus selects on.
- The manifests have not been applied to a live cluster. Review resource sizes and add an Ingress for your environment.

## Project layout

```
backend/
  cmd/api, cmd/worker, cmd/receiver   entrypoints (receiver is a demo webhook target)
  db/migrations                       SQL schema (embedded, applied with golang-migrate)
  db/queries                          sqlc query definitions
  internal/api                        router, handlers, middleware
  internal/dispatcher                 worker pool, backoff, HMAC signing
  internal/store                      sqlc-generated code
  internal/ratelimit, metrics, config, platform
frontend/src/app                      pages: overview, endpoints, deliveries, /api proxy
frontend/src/components, src/lib     UI components, API client, SWR hooks
observability/                        Prometheus config and alerts, Grafana provisioning
k8s/                                  Kubernetes manifests
docker-compose.yml, .env.example
```

## Development

```powershell
# Backend
cd backend
docker run --rm -v "${PWD}:/src" -w /src sqlc/sqlc generate   # after editing db/queries or migrations
go build ./... ; go vet ./...

# Run the API against the Compose Postgres/Redis
$env:DATABASE_URL = "postgres://eventpulse:eventpulse@localhost:5433/eventpulse?sslmode=disable"
$env:REDIS_URL = "redis://localhost:6380/0"; $env:MIGRATE_ON_START = "true"
go run ./cmd/api        # and, in another terminal: go run ./cmd/worker

# Frontend
cd frontend; npm install; npm run dev    # http://localhost:3000, set BACKEND_URL if the API is elsewhere
```

New schema changes go in a new numbered pair under `backend/db/migrations/` (for example `000002_name.up.sql` and `.down.sql`). Do not edit applied migrations.

## Troubleshooting

- **"port is already allocated"**: another program uses that host port. Override the matching `*_HOST_PORT` variable in `.env`.
- **Deliveries stay `pending` with HTTP 401**: the receiver's secret does not match the endpoint's current secret (for example after rotating it).
- **Deliveries fail with "destination ... is not permitted"**: `ALLOW_PRIVATE_TARGETS=false` and the endpoint resolves to a private address.
- **Dashboard shows "backend unavailable"**: check `BACKEND_URL` and that the API container is healthy (`docker compose ps`).
- **Reset all data**: `docker compose down -v`.

## Limitations

- No automated test suite yet. Behavior was verified with manual end-to-end runs.
- Authentication is a single shared API key. There is no per-user or per-application access control, so `owner_id` is stored but not enforced.
- `/metrics` is unauthenticated. Do not expose it publicly.
- Old `deliveries` partitions are never dropped automatically. Add a retention job if you need one.
