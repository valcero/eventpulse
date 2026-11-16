-- ===== Applications =====

-- name: CreateApplication :one
INSERT INTO applications (name, owner_id) VALUES ($1, $2) RETURNING *;

-- name: GetApplication :one
SELECT * FROM applications WHERE id = $1;

-- name: ListApplications :many
SELECT * FROM applications ORDER BY created_at DESC LIMIT $1 OFFSET $2;

-- ===== Endpoints =====

-- name: CreateEndpoint :one
INSERT INTO endpoints (app_id, url, secret) VALUES ($1, $2, $3) RETURNING *;

-- name: GetEndpoint :one
SELECT * FROM endpoints WHERE id = $1;

-- A nil UUID (all zeros) for app_id means "all applications".
-- name: ListEndpoints :many
SELECT * FROM endpoints
WHERE sqlc.arg(app_id)::uuid = '00000000-0000-0000-0000-000000000000' OR app_id = sqlc.arg(app_id)
ORDER BY created_at DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: UpdateEndpoint :one
UPDATE endpoints SET url = $2, is_active = $3 WHERE id = $1 RETURNING *;

-- name: RotateEndpointSecret :one
UPDATE endpoints SET secret = $2 WHERE id = $1 RETURNING *;

-- name: DeleteEndpoint :execrows
DELETE FROM endpoints WHERE id = $1;

-- name: CountActiveEndpoints :one
SELECT count(*) FROM endpoints WHERE is_active;

-- ===== Events =====

-- name: CreateEvent :one
INSERT INTO events (app_id, event_type, payload) VALUES ($1, $2, $3) RETURNING *;

-- Fan out one event to all active endpoints of its app.
-- name: FanOutEvent :many
INSERT INTO deliveries (event_id, endpoint_id)
SELECT sqlc.arg(event_id)::uuid, e.id FROM endpoints e WHERE e.app_id = sqlc.arg(app_id) AND e.is_active
RETURNING id;

-- ===== Deliveries =====

-- Lease-based claim: pushes next_retry_at into the future so no other worker
-- picks the row up, and a crashed worker's rows become due again automatically.
-- name: ClaimPendingDeliveries :many
WITH claimed AS (
    UPDATE deliveries d
    SET next_retry_at = now() + sqlc.arg(lease_seconds)::float8 * interval '1 second'
    WHERE (d.id, d.created_at) IN (
        SELECT s.id, s.created_at FROM deliveries s
        WHERE s.status = 'pending' AND s.next_retry_at <= now()
        ORDER BY s.next_retry_at
        LIMIT sqlc.arg(batch_size)
        FOR UPDATE SKIP LOCKED
    )
    RETURNING d.id, d.created_at, d.event_id, d.endpoint_id, d.attempts
)
SELECT c.id, c.created_at, c.event_id, c.endpoint_id, c.attempts,
       ev.event_type, ev.payload,
       ep.url, ep.secret, ep.is_active
FROM claimed c
JOIN events ev ON ev.id = c.event_id
JOIN endpoints ep ON ep.id = c.endpoint_id;

-- name: MarkDeliverySuccess :exec
UPDATE deliveries
SET status = 'success', status_code = $3, attempts = attempts + 1
WHERE id = $1 AND created_at = $2;

-- name: MarkDeliveryRetry :exec
UPDATE deliveries
SET status_code = $3, attempts = attempts + 1, next_retry_at = $4
WHERE id = $1 AND created_at = $2;

-- name: MarkDeliveryFailed :exec
UPDATE deliveries
SET status = 'failed', status_code = $3, attempts = attempts + 1
WHERE id = $1 AND created_at = $2;

-- Reschedule without consuming an attempt (e.g. per-endpoint rate limit).
-- name: DeferDelivery :exec
UPDATE deliveries SET next_retry_at = $3 WHERE id = $1 AND created_at = $2;

-- name: RedeliverDelivery :execrows
UPDATE deliveries
SET status = 'pending', attempts = 0, status_code = NULL, next_retry_at = now()
WHERE id = $1;

-- name: ListDeliveries :many
SELECT d.id, d.event_id, d.endpoint_id, d.status, d.status_code, d.attempts,
       d.next_retry_at, d.created_at, ev.event_type, ev.payload, ep.url
FROM deliveries d
JOIN events ev ON ev.id = d.event_id
JOIN endpoints ep ON ep.id = d.endpoint_id
WHERE (sqlc.arg(app_id)::uuid = '00000000-0000-0000-0000-000000000000' OR ep.app_id = sqlc.arg(app_id))
  AND (sqlc.arg(status_filter)::text = '' OR d.status::text = sqlc.arg(status_filter))
ORDER BY d.created_at DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountPendingDeliveries :one
SELECT count(*) FROM deliveries WHERE status = 'pending';

-- name: DeliveryStatsSince :many
SELECT date_trunc('minute', created_at)::timestamptz AS bucket, status, count(*) AS total
FROM deliveries
WHERE created_at >= $1
GROUP BY 1, 2
ORDER BY 1;

-- ===== Maintenance =====

-- name: EnsureDeliveriesPartition :one
SELECT ensure_deliveries_partition($1::date)::text AS partition_name;
