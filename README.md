<div align="center">

# ⚡ EventPulse

**Reliable webhook delivery and event streaming, built for scale and observability.**

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Next.js](https://img.shields.io/badge/Next.js-14-000000?logo=nextdotjs&logoColor=white)
![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)
![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)
![Prometheus](https://img.shields.io/badge/Prometheus-metrics-E6522C?logo=prometheus&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)
![Kubernetes](https://img.shields.io/badge/Kubernetes-manifests-326CE5?logo=kubernetes&logoColor=white)

</div>

EventPulse accepts events through a REST API and delivers them to registered HTTP endpoints. Every request is signed with HMAC-SHA256. Failed deliveries are retried with exponential backoff and jitter. A dashboard and a Grafana board show what is happening.

## Contents

- [Features](#features)
- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Dashboard](#dashboard)
- [API reference](#api-reference)
- [Verifying signatures](#verifying-signatures)
- [Delivery semantics](#delivery-semantics)
- [Configuration](#configuration)
- [Observability](#observability)
- [Kubernetes](#kubernetes)
- [Project structure](#project-structure)
- [Development](#development)
- [Troubleshooting](#troubleshooting)
- [Limitations](#limitations)

## Features

| Area | What you get |
|---|---|
| **Ingestion** | REST API for applications, endpoints and events. An event and its delivery fan-out commit in one transaction. |
| **Worker pool** | Configurable goroutine pool, 5 s request timeout, graceful shutdown that finishes in-flight deliveries. |
| **Retries** | Exponential backoff with jitter, 5 attempts by default, then a terminal `failed` state. Manual redelivery from the UI or API. |
| **Security** | Per-endpoint HMAC-SHA256 signatures, rotatable secrets, optional API key, SSRF protection, no redirect following. |
| **Storage** | PostgreSQL 16 with type-safe `sqlc` queries, JSONB payloads, and `deliveries` range-partitioned by month. |
| **Coordination** | Redis rate limiting per application (ingest) and per endpoint (delivery). Fails open if Redis is down. |
| **Dashboard** | Next.js and Tailwind: throughput charts, endpoint manager, delivery log with payload inspection. |
| **Observability** | Prometheus metrics, a provisioned Grafana dashboard, and sample alert rules. |
| **Deployment** | Multi-stage Dockerfiles, Docker Compose, Kubernetes manifests with an HPA and a ServiceMonitor. |

## Architecture

```mermaid
flowchart LR
    C[Your service] -->|POST /v1/applications/:id/events| API
    UI[Dashboard<br/>Next.js] -->|/api proxy| API
    API -->|tx: event + pending deliveries| PG[(PostgreSQL)]
    API <-->|rate limits| R[(Redis)]
    PG -->|lease-based claim<br/>FOR UPDATE SKIP LOCKED| W[Worker pool]
    W <-->|rate limits, locks| R
    W -->|signed POST, 5s timeout| E[Customer endpoints]
    W -->|result + next retry| PG
    API -. /metrics .-> P[Prometheus]
    W -. /metrics .-> P
    P --> G[Grafana]
```

**How workers share the queue.** A worker claims due rows with `UPDATE ... WHERE (id, created_at) IN (SELECT ... FOR UPDATE SKIP LOCKED)`. This pushes `next_retry_at` into the future, which acts as a lease. Any number of workers can run in parallel. No database transaction stays open during an HTTP call. If a worker crashes, its rows become due again when the lease expires.

**Delivery state machine**

```mermaid
stateDiagram-v2
    [*] --> pending
    pending --> success: 2xx response
    pending --> pending: error or non-2xx<br/>(attempts < max, wait backoff)
    pending --> failed: attempts reach max
    failed --> pending: redeliver
    success --> pending: redeliver
```

## Quick start

**Requirements:** Docker with Compose v2.

```bash
git clone https://github.com/valcero/eventpulse.git
cd eventpulse
cp .env.example .env          # optional: change ports and settings
docker compose up -d --build
```

| Service | URL | Notes |
|---|---|---|
| Dashboard | http://localhost:3100 | |
| API | http://localhost:8080 | `GET /healthz` |
| Grafana | http://localhost:3101 | login `admin` / `admin` |
| Prometheus | http://localhost:9091 | |
| PostgreSQL | `localhost:5433` | user, password and database: `eventpulse` |
| Redis | `localhost:6380` | |

The API applies database migrations on startup. If a port is already in use, change the matching `*_HOST_PORT` value in `.env`.

### Send your first event

The demo `receiver` service verifies signatures and can simulate failures.

<details open>
<summary><b>PowerShell</b></summary>

```powershell
$b = "http://localhost:8080"; $j = "application/json"
$app = Invoke-RestMethod -Method Post "$b/v1/applications" -ContentType $j -Body '{"name":"Demo"}'
$ep  = Invoke-RestMethod -Method Post "$b/v1/applications/$($app.id)/endpoints" -ContentType $j `
         -Body '{"url":"http://receiver:9000/hook"}'

# Start the demo receiver with the endpoint's secret
$env:RECEIVER_SECRET = $ep.secret; $env:RECEIVER_FAIL_PERCENT = "0"
docker compose --profile demo up -d --force-recreate receiver

Invoke-RestMethod -Method Post "$b/v1/applications/$($app.id)/events" -ContentType $j `
  -Body '{"event_type":"order.created","payload":{"order_id":42}}'
docker compose logs receiver --tail 5
```
</details>

<details>
<summary><b>bash / curl</b></summary>

```bash
B=http://localhost:8080
APP=$(curl -s -X POST $B/v1/applications -H 'Content-Type: application/json' -d '{"name":"Demo"}')
APP_ID=$(echo "$APP" | jq -r .id)
EP=$(curl -s -X POST $B/v1/applications/$APP_ID/endpoints -H 'Content-Type: application/json' \
       -d '{"url":"http://receiver:9000/hook"}')

RECEIVER_SECRET=$(echo "$EP" | jq -r .secret) RECEIVER_FAIL_PERCENT=0 \
  docker compose --profile demo up -d --force-recreate receiver

curl -s -X POST $B/v1/applications/$APP_ID/events -H 'Content-Type: application/json' \
  -d '{"event_type":"order.created","payload":{"order_id":42}}'
docker compose logs receiver --tail 5
```
</details>

Set `RECEIVER_FAIL_PERCENT=50` to watch retries happen in the dashboard.

## Dashboard

| Page | What it does |
|---|---|
| **Overview** | Success, failure and backlog counts, success rate, active endpoints, deliveries-per-minute chart, success vs. failure ratio. Windows: 15 m, 1 h, 6 h, 24 h. Refreshes every 5 s. |
| **Endpoints** | Create applications, add and disable endpoints, reveal or copy signing secrets, rotate secrets, delete endpoints, send a test event. |
| **Deliveries** | Filter by application and status, inspect the payload and IDs, see HTTP status and attempt count, redeliver. |

The browser only talks to the dashboard's own `/api/*` route. That route forwards to the Go API and adds the API key on the server, so the key never reaches the browser.

## API reference

Base path `/v1`. Errors are returned as `{"error": "message"}`.

| Method and path | Purpose |
|---|---|
| `POST /applications` | Create an application: `{name}` |
| `GET /applications`, `GET /applications/{id}` | List or fetch applications |
| `POST /applications/{id}/endpoints` | Register an endpoint: `{url}`. The signing secret is generated and returned |
| `GET /endpoints?app_id=` | List endpoints |
| `PATCH /endpoints/{id}` | Update `{url?, is_active?}` |
| `POST /endpoints/{id}/rotate-secret` | Generate a new signing secret |
| `DELETE /endpoints/{id}` | Delete an endpoint and its deliveries |
| `POST /applications/{id}/events` | Publish `{event_type, payload}`. Returns `202 {event_id, deliveries}` |
| `GET /deliveries?app_id=&status=&limit=&offset=` | Delivery log. `status` is `pending`, `success` or `failed` |
| `POST /deliveries/{id}/redeliver` | Reset a delivery to `pending` with 0 attempts |
| `GET /stats?window=60` | Aggregates for the last N minutes (maximum 1440) |

Outside `/v1`: `GET /healthz` (liveness), `GET /readyz` (checks Postgres), `GET /metrics`.

When `API_KEY` is set, every `/v1/*` call needs `Authorization: Bearer <key>`. Ingestion returns `429` with a `Retry-After` header when the per-application rate limit is exceeded.

## Verifying signatures

Each delivery is a `POST` with a JSON body:

```json
{ "id": "<event id>", "type": "order.created", "data": { "order_id": 42 } }
```

| Header | Value |
|---|---|
| `X-EventPulse-Signature` | `t=<unix seconds>,v1=<hex HMAC-SHA256>` |
| `X-EventPulse-Event` | Event type |
| `X-EventPulse-Delivery-Id` | Delivery UUID. Use it to de-duplicate |

To verify a request:

1. Read `t` and `v1` from the signature header.
2. Compute `HMAC_SHA256(secret, "<t>.<raw request body>")` as hex.
3. Compare with `v1` in constant time.
4. Reject the request if `t` is more than a few minutes old.

```go
mac := hmac.New(sha256.New, []byte(secret))
mac.Write([]byte(t + "."))
mac.Write(rawBody)
ok := hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(v1))
```

A complete example lives in [`backend/cmd/receiver/main.go`](backend/cmd/receiver/main.go).

## Delivery semantics

- **At-least-once.** Any 2xx response counts as success. Network errors, timeouts and other statuses are retried, so receivers should de-duplicate on `X-EventPulse-Delivery-Id`.
- **Backoff.** `BASE_BACKOFF × 2^(n-1)`, capped at `MAX_BACKOFF`, with jitter uniformly in `[d/2, d]`. After `MAX_ATTEMPTS` the delivery becomes `failed`.
- **No redirects.** Redirect responses are treated as failures.
- **SSRF protection.** With `ALLOW_PRIVATE_TARGETS=false`, connections to loopback, private, link-local and multicast addresses are refused after DNS resolution. Docker Compose sets it to `true` so the demo receiver works. The Kubernetes config sets it to `false`.
- **Disabled endpoints.** Deliveries already queued for an endpoint that is later disabled are marked `failed`.
- **Partitions.** Workers create monthly `deliveries` partitions for the next three months. A default partition catches anything outside them.

## Configuration

All services are configured through environment variables. Docker Compose reads overrides from `.env`.

<details>
<summary><b>Backend variables</b></summary>

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | required | Postgres connection string |
| `REDIS_URL` | empty | Redis URL. Rate limiting and locks are disabled if empty |
| `HTTP_ADDR` | `:8080` | API listen address |
| `METRICS_ADDR` | `:9090` | Worker metrics and health address |
| `API_KEY` | empty | Bearer token required for `/v1/*` |
| `MIGRATE_ON_START` | `false` | Run migrations when the API starts. Compose and Kubernetes enable it |
| `WORKER_CONCURRENCY` | `32` | Goroutines delivering in parallel per worker |
| `DISPATCH_TIMEOUT` | `5s` | Per-request timeout |
| `MAX_ATTEMPTS` | `5` | Attempts before a delivery is `failed` |
| `BASE_BACKOFF` | `5s` | First retry delay (before jitter) |
| `MAX_BACKOFF` | `1h` | Maximum retry delay |
| `POLL_INTERVAL` | `500ms` | Idle polling interval |
| `CLAIM_LEASE` | `DISPATCH_TIMEOUT + 30s` | How long a claimed delivery is reserved |
| `INGEST_RATE_PER_SEC` | `200` | Events per second per application (`0` = unlimited) |
| `ENDPOINT_RATE_PER_SEC` | `50` | Deliveries per second per endpoint (`0` = unlimited) |
| `ALLOW_PRIVATE_TARGETS` | `false` | Allow delivery to private and loopback addresses |
| `CORS_ORIGIN` | empty | Value for `Access-Control-Allow-Origin` (not needed with the dashboard proxy) |
| `DEFAULT_OWNER_ID` | `00000000-…-0001` | `owner_id` used when a request does not provide one |
| `LOG_LEVEL` | `info` | `debug` also logs every request |
</details>

<details>
<summary><b>Frontend variables</b></summary>

| Variable | Default | Description |
|---|---|---|
| `BACKEND_URL` | `http://localhost:8080` | Go API base URL, read at runtime |
| `API_KEY` | empty | Forwarded to the API as a bearer token |
</details>

## Observability

| Metric | Type | Notes |
|---|---|---|
| `eventpulse_events_ingested_total` | Counter | Events accepted by the API |
| `eventpulse_deliveries_total{status="success\|failed"}` | Counter | Terminal results, exported by workers |
| `eventpulse_delivery_retries_total` | Counter | Failed attempts that were rescheduled |
| `eventpulse_webhook_dispatch_duration_seconds` | Histogram | Outbound request time, exported by workers |
| `eventpulse_queue_backlog_depth` | Gauge | Pending deliveries. API and worker both report it, so query with `max()` |
| `eventpulse_http_requests_total` | Counter | API requests by method, route and status |
| `eventpulse_http_request_duration_seconds` | Histogram | API latency by method and route |

- The Grafana dashboard at [`observability/grafana/dashboards/eventpulse.json`](observability/grafana/dashboards/eventpulse.json) is provisioned automatically.
- Sample alerts (high failure ratio, growing backlog) are in [`observability/prometheus/alerts.yml`](observability/prometheus/alerts.yml).

## Kubernetes

Manifests are in [`k8s/`](k8s). PostgreSQL and Redis are expected to be managed services.

```bash
# Build and push images, then update the image names in k8s/api.yaml, worker.yaml and frontend.yaml
docker build -t <registry>/eventpulse-backend  ./backend
docker build -t <registry>/eventpulse-frontend ./frontend

kubectl apply -f k8s/namespace.yaml
kubectl -n eventpulse create secret generic eventpulse-secrets \
  --from-literal=DATABASE_URL='postgres://user:pass@host:5432/eventpulse?sslmode=require' \
  --from-literal=REDIS_URL='redis://:pass@host:6379/0' \
  --from-literal=API_KEY="$(openssl rand -hex 32)"
kubectl apply -k k8s
```

| File | Contents |
|---|---|
| `api.yaml`, `worker.yaml`, `frontend.yaml` | Deployments and Services. The API also has a PodDisruptionBudget |
| `hpa.yaml` | Worker autoscaling, 2 to 20 replicas at 60% CPU |
| `hpa-queue-backlog.example.yaml` | Optional: scale on queue depth through prometheus-adapter. Use instead of `hpa.yaml` |
| `servicemonitor.yaml` | Prometheus Operator scrape config for API and worker |
| `configmap.yaml`, `secret.example.yaml` | Settings and a secret template (never commit real values) |

Notes:
- `servicemonitor.yaml` needs the Prometheus Operator CRDs. Remove it from `kustomization.yaml` if they are not installed, or add the label your Prometheus selects on.
- The manifests render with `kubectl kustomize k8s` but have not been applied to a live cluster. Review resource sizes and add an Ingress for your environment.

## Project structure

```
eventpulse/
├── backend/
│   ├── cmd/                  api, worker, receiver (demo webhook target)
│   ├── db/
│   │   ├── migrations/       SQL schema, embedded and applied with golang-migrate
│   │   └── queries/          sqlc query definitions
│   ├── internal/
│   │   ├── api/              router, handlers, middleware
│   │   ├── dispatcher/       worker pool, backoff, HMAC signing
│   │   ├── store/            sqlc-generated code
│   │   └── config, metrics, platform, ratelimit
│   ├── sqlc.yaml
│   └── Dockerfile
├── frontend/
│   ├── src/app/              overview, endpoints, deliveries pages and the /api proxy
│   ├── src/components/       charts and UI components
│   ├── src/lib/              API client and SWR hooks
│   └── Dockerfile
├── observability/
│   ├── prometheus/           scrape config and alert rules
│   └── grafana/              provisioning and dashboard JSON
├── k8s/                      Kubernetes manifests
├── docker-compose.yml
└── .env.example
```

## Development

**Backend** (Go 1.26+)

```bash
cd backend
docker run --rm -v "$PWD:/src" -w /src sqlc/sqlc generate   # after editing db/queries or db/migrations
go build ./... && go vet ./...

# Run against the Compose Postgres and Redis
export DATABASE_URL='postgres://eventpulse:eventpulse@localhost:5433/eventpulse?sslmode=disable'
export REDIS_URL='redis://localhost:6380/0' MIGRATE_ON_START=true ALLOW_PRIVATE_TARGETS=true
go run ./cmd/api          # and in a second terminal: go run ./cmd/worker
```

**Frontend** (Node.js 20+)

```bash
cd frontend
npm install
npm run dev               # http://localhost:3000, set BACKEND_URL if the API is elsewhere
npm run typecheck
```

**Schema changes.** Add a new numbered pair under `backend/db/migrations/` (for example `000002_add_x.up.sql` and `.down.sql`) and re-run `sqlc generate`. Do not edit migrations that have already been applied.

## Troubleshooting

<details>
<summary><b>"port is already allocated" on startup</b></summary>

Another program is using that host port. Set the matching `*_HOST_PORT` variable in `.env` and run `docker compose up -d` again.
</details>

<details>
<summary><b>Deliveries stay <code>pending</code> with HTTP 401</b></summary>

The receiver's secret does not match the endpoint's current secret, for example after a rotation. Update the receiver, then use **Redeliver**.
</details>

<details>
<summary><b>Deliveries fail with "destination … is not permitted"</b></summary>

`ALLOW_PRIVATE_TARGETS` is `false` and the endpoint resolves to a private or loopback address. This is intended in production. For local testing set it to `true`.
</details>

<details>
<summary><b>Dashboard shows "backend unavailable"</b></summary>

Check `BACKEND_URL` and that the API container is healthy with `docker compose ps`.
</details>

<details>
<summary><b>Start over with a clean database</b></summary>

`docker compose down -v` removes all containers and volumes, including data.
</details>

## Limitations

- There is no automated test suite yet. Behavior was checked with manual end-to-end runs.
- Authentication is a single shared API key. `owner_id` is stored but not enforced, so there is no per-user or per-application access control.
- `/metrics` is unauthenticated. Do not expose it publicly.
- Old `deliveries` partitions are not dropped automatically. Add a retention job if you need one.
- The repository has no license file yet. Add one before publishing.
