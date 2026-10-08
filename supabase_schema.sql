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

-- One row per physical field device, owned by one user. key_hash is the
-- SHA-256 hex digest of the device's X-Device-Key — the plaintext key is
-- shown once at creation time (CreateDevice) and never stored.
CREATE TABLE IF NOT EXISTS devices (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    key_hash     TEXT        NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ
);

ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS user_id   BIGINT REFERENCES users(id)   ON DELETE CASCADE;
ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS device_id BIGINT REFERENCES devices(id) ON DELETE SET NULL;
ALTER TABLE alerts          ADD COLUMN IF NOT EXISTS user_id   BIGINT REFERENCES users(id)   ON DELETE CASCADE;
ALTER TABLE alerts          ADD COLUMN IF NOT EXISTS device_id BIGINT REFERENCES devices(id) ON DELETE SET NULL;

-- ── Indexes ──────────────────────────────────────────────────────────────────

CREATE INDEX IF NOT EXISTS idx_sensor_readings_timestamp ON sensor_readings (timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_sensor_readings_user_id   ON sensor_readings (user_id);
CREATE INDEX IF NOT EXISTS idx_alerts_created_at        ON alerts (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alerts_user_id           ON alerts (user_id);
CREATE INDEX IF NOT EXISTS idx_symptom_logs_user_id     ON symptom_logs (user_id);
CREATE INDEX IF NOT EXISTS idx_symptom_logs_created_at  ON symptom_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_devices_user_id          ON devices (user_id);
