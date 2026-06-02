-- Run this entire file in your Supabase project:
-- Dashboard → SQL Editor → New query → paste → Run

-- ── Tables ───────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT        NOT NULL,
    email         TEXT        NOT NULL UNIQUE,
    password_hash TEXT        NOT NULL DEFAULT '',
    condition     TEXT        NOT NULL DEFAULT 'Asthma',
    patient_id    TEXT        NOT NULL DEFAULT '',
    threshold     INTEGER     NOT NULL DEFAULT 75,
    role          TEXT        NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sensor_readings (
    id          BIGSERIAL PRIMARY KEY,
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    pm25        FLOAT       NOT NULL,
    voc         FLOAT       NOT NULL,
    temperature FLOAT       NOT NULL,
    humidity    FLOAT       NOT NULL,
    aqi         INTEGER     NOT NULL
);

CREATE TABLE IF NOT EXISTS alerts (
    id         BIGSERIAL PRIMARY KEY,
    message    TEXT        NOT NULL,
    level      TEXT        NOT NULL CHECK (level IN ('warning', 'info', 'success')),
    read       BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS symptom_logs (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      REFERENCES users(id),
    symptoms   JSONB       NOT NULL DEFAULT '[]',
    severity   INTEGER     NOT NULL CHECK (severity BETWEEN 1 AND 3),
    triggers   JSONB       NOT NULL DEFAULT '[]',
    notes      TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Indexes ──────────────────────────────────────────────────────────────────

CREATE INDEX IF NOT EXISTS idx_sensor_readings_timestamp ON sensor_readings (timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_alerts_created_at        ON alerts (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_symptom_logs_user_id     ON symptom_logs (user_id);
CREATE INDEX IF NOT EXISTS idx_symptom_logs_created_at  ON symptom_logs (created_at DESC);

-- ── Daily aggregate view ──────────────────────────────────────────────────────
-- Used by GET /api/v1/sensors/daily

CREATE OR REPLACE VIEW daily_aggregates AS
SELECT
    DATE(sr.timestamp AT TIME ZONE 'UTC') AS date,
    ROUND(AVG(sr.pm25)::NUMERIC, 1)       AS pm25,
    ROUND(AVG(sr.voc)::NUMERIC,  0)       AS voc,
    ROUND(AVG(sr.aqi)::NUMERIC,  0)::INT  AS aqi,
    COALESCE(s.symptoms, 0)               AS symptoms
FROM sensor_readings sr
LEFT JOIN (
    SELECT
        DATE(created_at AT TIME ZONE 'UTC') AS date,
        COUNT(*)                            AS symptoms
    FROM symptom_logs
    GROUP BY DATE(created_at AT TIME ZONE 'UTC')
) s ON s.date = DATE(sr.timestamp AT TIME ZONE 'UTC')
GROUP BY DATE(sr.timestamp AT TIME ZONE 'UTC'), s.symptoms
ORDER BY date ASC;
