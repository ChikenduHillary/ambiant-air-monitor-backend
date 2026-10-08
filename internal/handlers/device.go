package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/aqi"
	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/middleware"
)

type deviceReading struct {
	PM1_0       float64 `json:"pm1_0"`
	PM25        float64 `json:"pm2_5"`
	PM10        float64 `json:"pm10"`
	VOC         float64 `json:"voc_ppm"`
	Temperature float64 `json:"temperature"`
	Humidity    float64 `json:"humidity"`
}

// deviceReadingIntervalSec must match the firmware's SLEEP_INTERVAL_SEC.
// Batches arrive oldest-first with no per-reading timestamp, so timestamps
// are back-filled at this cadence, ending at the time the batch is received.
const deviceReadingIntervalSec = 60

// IngestReadings accepts a batch of sensor readings pushed by a field device
// (see the AAQPHM firmware's buildBatchJsonPayload).
func (h *Handler) IngestReadings(w http.ResponseWriter, r *http.Request) {
	deviceID, _ := r.Context().Value(middleware.DeviceIDKey).(int64)
	userID, _ := r.Context().Value(middleware.DeviceUserIDKey).(int64)

	var readings []deviceReading
	if err := json.NewDecoder(r.Body).Decode(&readings); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if len(readings) == 0 {
		http.Error(w, "empty reading batch", http.StatusBadRequest)
		return
	}
	if len(readings) > 500 {
		http.Error(w, "batch too large", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	n := len(readings)
	inserted := 0

	for i, rd := range readings {
		if rd.PM25 < 0 || rd.PM25 > 2000 || rd.Temperature < -40 || rd.Temperature > 85 || rd.Humidity < 0 || rd.Humidity > 100 {
			continue
		}
		ts := now.Add(-time.Duration((n-1-i)*deviceReadingIntervalSec) * time.Second)
		aqiVal := aqi.FromPM25(rd.PM25)

		_, err := h.db.ExecContext(r.Context(),
			`INSERT INTO sensor_readings (timestamp, pm25, voc, temperature, humidity, aqi, user_id, device_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			ts, rd.PM25, rd.VOC, rd.Temperature, rd.Humidity, aqiVal, userID, deviceID,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		inserted++

		if rd.PM25 > 35 {
			h.db.ExecContext(r.Context(), `INSERT INTO alerts (message, level, user_id, device_id) VALUES ($1, 'warning', $2, $3)`,
				"PM2.5 exceeded safe threshold (35 µg/m³)", userID, deviceID)
		} else if aqiVal > 75 {
			h.db.ExecContext(r.Context(), `INSERT INTO alerts (message, level, user_id, device_id) VALUES ($1, 'info', $2, $3)`,
				"AQI elevated — consider limiting outdoor activity", userID, deviceID)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "inserted": inserted})
}
