package simulator

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"math/rand"
	"time"

	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/aqi"
)

type Simulator struct {
	db  *sql.DB
	rng *rand.Rand
}

func New(db *sql.DB) *Simulator {
	return &Simulator{
		db:  db,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Run generates a sensor reading every 30 seconds and fires alerts on threshold breaches.
func (s *Simulator) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	slog.Info("sensor simulator started", "interval", "30s")
	for {
		select {
		case <-ctx.Done():
			slog.Info("sensor simulator stopped")
			return
		case <-ticker.C:
			if err := s.record(ctx); err != nil {
				slog.Error("simulator record error", "error", err)
			}
		}
	}
}

func (s *Simulator) record(ctx context.Context) error {
	now := time.Now()
	hour := float64(now.Hour())

	// Diurnal pattern: peaks during commute hours (8 AM, 6 PM).
	basePM := 15.0 + 8*math.Sin((hour-8)*math.Pi/12)
	pm25 := round1(math.Max(1, basePM+s.rng.Float64()*6-3))
	voc := math.Round(350 + math.Cos(hour/10)*60 + s.rng.Float64()*30)
	temp := round1(22 + math.Sin(hour/8)*2 + s.rng.Float64()*0.5)
	hum := math.Round(55 + math.Cos(hour/6)*8 + s.rng.Float64()*4)
	aqiVal := aqi.FromPM25(pm25)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sensor_readings (timestamp, pm25, voc, temperature, humidity, aqi) VALUES ($1, $2, $3, $4, $5, $6)`,
		now.UTC(), pm25, voc, temp, hum, aqiVal,
	)
	if err != nil {
		return err
	}

	slog.Debug("reading recorded", "pm25", pm25, "voc", voc, "temp", temp, "hum", hum, "aqi", aqiVal)

	// Threshold-based alert generation.
	if pm25 > 35 {
		s.db.ExecContext(ctx, `INSERT INTO alerts (message, level) VALUES ($1, 'warning')`,
			"PM2.5 exceeded safe threshold (35 µg/m³)")
	} else if aqiVal > 75 {
		s.db.ExecContext(ctx, `INSERT INTO alerts (message, level) VALUES ($1, 'info')`,
			"AQI elevated — consider limiting outdoor activity")
	}

	return nil
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
