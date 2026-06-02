package db

import (
	"database/sql"
	"math"
	"math/rand"
	"time"

	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/aqi"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, db.Ping()
}

// Migrate creates tables that don't exist yet.
// The full schema (including indexes and views) should be applied once via
// supabase_schema.sql in the Supabase SQL editor.
func Migrate(db *sql.DB) error {
	_, err := db.Exec(`
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
			level      TEXT        NOT NULL CHECK (level IN ('warning','info','success')),
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
	`)
	return err
}

func Seed(db *sql.DB) error {
	if err := seedReadings(db); err != nil {
		return err
	}
	return seedAlerts(db)
}

const minExpectedReadings = 500

func seedReadings(db *sql.DB) error {
	var n int
	db.QueryRow("SELECT COUNT(*) FROM sensor_readings").Scan(&n)
	if n >= minExpectedReadings {
		return nil
	}
	if n > 0 {
		db.Exec("DELETE FROM sensor_readings")
	}

	stmt, err := db.Prepare(`
		INSERT INTO sensor_readings (timestamp, pm25, voc, temperature, humidity, aqi)
		VALUES ($1, $2, $3, $4, $5, $6)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	rng := rand.New(rand.NewSource(42))
	now := time.Now()

	for i := 30 * 24; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * time.Hour)
		h := float64(t.Hour())

		basePM := 15.0 + 8*math.Sin((h-8)*math.Pi/12)
		pm25 := round1(math.Max(1, basePM+rng.Float64()*6-3))
		voc := math.Round(350 + math.Cos(h/10)*80 + rng.Float64()*50)
		temp := round1(22 + math.Sin(h/8)*2 + rng.Float64()*0.5)
		hum := math.Round(55 + math.Cos(h/6)*8 + rng.Float64()*4)
		aqiVal := aqi.FromPM25(pm25)

		if _, err := stmt.Exec(t.UTC(), pm25, voc, temp, hum, aqiVal); err != nil {
			return err
		}
	}
	return nil
}

func seedAlerts(db *sql.DB) error {
	var n int
	db.QueryRow("SELECT COUNT(*) FROM alerts").Scan(&n)
	if n > 0 {
		return nil
	}

	now := time.Now()
	seed := []struct {
		msg, level string
		ago        time.Duration
	}{
		{"PM2.5 approaching personal threshold (35 µg/m³)", "warning", 2 * time.Minute},
		{"Outdoor AQI elevated — consider staying indoors", "info", 18 * time.Minute},
		{"Humidity returned to safe range", "success", 1 * time.Hour},
		{"VOC spike detected — 680 ppm peak", "warning", 3 * time.Hour},
	}

	stmt, err := db.Prepare(`INSERT INTO alerts (message, level, created_at) VALUES ($1, $2, $3)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range seed {
		if _, err := stmt.Exec(a.msg, a.level, now.Add(-a.ago).UTC()); err != nil {
			return err
		}
	}
	return nil
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
