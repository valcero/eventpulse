-- EventPulse core schema (PostgreSQL 16+)

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

CREATE TYPE delivery_status AS ENUM ('pending', 'success', 'failed');

CREATE TABLE applications (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    owner_id   UUID        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_applications_owner ON applications (owner_id);

CREATE TABLE endpoints (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id     UUID        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    url        TEXT        NOT NULL CHECK (url ~* '^https?://'),
    secret     TEXT        NOT NULL, -- HMAC-SHA256 signing key
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_endpoints_app_active ON endpoints (app_id) WHERE is_active;

CREATE TABLE events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id     UUID        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    event_type TEXT        NOT NULL,
    payload    JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_app_created ON events (app_id, created_at DESC);
CREATE INDEX idx_events_payload ON events USING GIN (payload jsonb_path_ops);

-- Range-partitioned by created_at. The partition key must be part of the
-- primary key, so the PK is (id, created_at). Because of this, other tables
-- cannot FK to deliveries(id); references are by (id) in application code.
CREATE TABLE deliveries (
    id            UUID            NOT NULL DEFAULT gen_random_uuid(),
    event_id      UUID            NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    endpoint_id   UUID            NOT NULL REFERENCES endpoints (id) ON DELETE CASCADE,
    status        delivery_status NOT NULL DEFAULT 'pending',
    status_code   INT,
    attempts      INT             NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_retry_at TIMESTAMPTZ     NOT NULL DEFAULT now(),
    created_at    TIMESTAMPTZ     NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Worker polling: due pending deliveries (FOR UPDATE SKIP LOCKED).
CREATE INDEX idx_deliveries_pending_due ON deliveries (next_retry_at) WHERE status = 'pending';
CREATE INDEX idx_deliveries_event ON deliveries (event_id);
CREATE INDEX idx_deliveries_endpoint_created ON deliveries (endpoint_id, created_at DESC);

-- Catch-all so inserts never fail if a monthly partition is missing.
CREATE TABLE deliveries_default PARTITION OF deliveries DEFAULT;

-- Creates the monthly partition containing `month_start` if absent.
CREATE OR REPLACE FUNCTION ensure_deliveries_partition(month_start DATE)
RETURNS TEXT LANGUAGE plpgsql AS $$
DECLARE
    from_ts DATE := date_trunc('month', month_start)::DATE;
    to_ts   DATE := (date_trunc('month', month_start) + INTERVAL '1 month')::DATE;
    part    TEXT := format('deliveries_%s', to_char(from_ts, 'YYYY_MM'));
BEGIN
    IF to_regclass(part) IS NULL THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF deliveries FOR VALUES FROM (%L) TO (%L)',
            part, from_ts, to_ts);
    END IF;
    RETURN part;
END $$;

-- Current month plus the next two.
SELECT ensure_deliveries_partition(CURRENT_DATE);
SELECT ensure_deliveries_partition((CURRENT_DATE + INTERVAL '1 month')::DATE);
SELECT ensure_deliveries_partition((CURRENT_DATE + INTERVAL '2 month')::DATE);
