package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/aqi"
	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/middleware"
	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/models"
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

// GetDeviceCurrentReading returns the latest reading for whichever device the
// X-Device-Key belongs to — authenticated by the device's own key rather than
// a user's login, so anyone holding that key (e.g. a team testing one shared
// unit) can view its data without needing an account on it.
func (h *Handler) GetDeviceCurrentReading(w http.ResponseWriter, r *http.Request) {
	deviceID, _ := r.Context().Value(middleware.DeviceIDKey).(int64)

	var s models.SensorReading
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, timestamp, pm25, voc, temperature, humidity, aqi
		FROM sensor_readings WHERE device_id = $1 ORDER BY timestamp DESC LIMIT 1
	`, deviceID).Scan(&s.ID, &s.Timestamp, &s.PM25, &s.VOC, &s.Temperature, &s.Humidity, &s.AQI)
	if err != nil {
		http.Error(w, "no readings available", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// GetDeviceHourlyReadings is GetDeviceCurrentReading's counterpart for the
// last 60 minutes of readings from the same device-key-authenticated device.
func (h *Handler) GetDeviceHourlyReadings(w http.ResponseWriter, r *http.Request) {
	deviceID, _ := r.Context().Value(middleware.DeviceIDKey).(int64)

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, timestamp, pm25, voc, temperature, humidity, aqi
		FROM sensor_readings
		WHERE device_id = $1 AND timestamp >= NOW() - INTERVAL '60 minutes'
		ORDER BY timestamp ASC
	`, deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	readings := []models.SensorReading{}
	for rows.Next() {
		var s models.SensorReading
		if err := rows.Scan(&s.ID, &s.Timestamp, &s.PM25, &s.VOC, &s.Temperature, &s.Humidity, &s.AQI); err != nil {
			continue
		}
		readings = append(readings, s)
	}
	writeJSON(w, http.StatusOK, readings)
}

// GetDeviceDailyReadings is GetDeviceCurrentReading's counterpart for daily
// aggregates. Symptom counts aren't included (symptom_logs belongs to a
// user, not a device, which isn't known here) — always 0.
func (h *Handler) GetDeviceDailyReadings(w http.ResponseWriter, r *http.Request) {
	deviceID, _ := r.Context().Value(middleware.DeviceIDKey).(int64)
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 90 {
		days = 30
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT
			DATE(timestamp AT TIME ZONE 'UTC')   AS day,
			ROUND(AVG(pm25)::numeric, 1)         AS avg_pm25,
			ROUND(AVG(voc)::numeric,  0)         AS avg_voc,
			ROUND(AVG(aqi)::numeric,  0)::int    AS avg_aqi,
			0                                     AS symptoms
		FROM sensor_readings
		WHERE device_id = $1 AND timestamp >= NOW() - ($2::int || ' days')::interval
		GROUP BY DATE(timestamp AT TIME ZONE 'UTC')
		ORDER BY day ASC
	`, deviceID, days)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := []models.DailyAggregate{}
	for rows.Next() {
		var d models.DailyAggregate
		var date time.Time
		if err := rows.Scan(&date, &d.PM25, &d.VOC, &d.AQI, &d.Symptoms); err != nil {
			continue
		}
		d.Date = date.UTC().Format("2006-01-02")
		result = append(result, d)
	}
	writeJSON(w, http.StatusOK, result)
}
