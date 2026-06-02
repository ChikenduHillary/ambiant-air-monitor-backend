package handlers

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/models"
)

type Handler struct {
	db                 *sql.DB
	secret             []byte
	googleClientID     string
	googleClientSecret string
	googleRedirectURL  string
	frontendURL        string
}

type Config struct {
	JWTSecret          []byte
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	FrontendURL        string
}

func New(db *sql.DB, cfg Config) *Handler {
	return &Handler{
		db:                 db,
		secret:             cfg.JWTSecret,
		googleClientID:     cfg.GoogleClientID,
		googleClientSecret: cfg.GoogleClientSecret,
		googleRedirectURL:  cfg.GoogleRedirectURL,
		frontendURL:        cfg.FrontendURL,
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ── health ───────────────────────────────────────────────────────────────────

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// ── sensors ──────────────────────────────────────────────────────────────────

func (h *Handler) GetCurrentReading(w http.ResponseWriter, r *http.Request) {
	var s models.SensorReading
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, timestamp, pm25, voc, temperature, humidity, aqi
		FROM sensor_readings ORDER BY timestamp DESC LIMIT 1
	`).Scan(&s.ID, &s.Timestamp, &s.PM25, &s.VOC, &s.Temperature, &s.Humidity, &s.AQI)
	if err != nil {
		http.Error(w, "no readings available", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *Handler) GetHourlyReadings(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, timestamp, pm25, voc, temperature, humidity, aqi
		FROM sensor_readings
		WHERE timestamp >= NOW() - INTERVAL '60 minutes'
		ORDER BY timestamp ASC
	`)
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

func (h *Handler) GetDailyReadings(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 90 {
		days = 30
	}

	// Use the daily_aggregates view (created by supabase_schema.sql).
	// Falls back to inline query if view is not available.
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT date, pm25, voc, aqi, symptoms
		FROM daily_aggregates
		WHERE date >= (NOW() - ($1::int || ' days')::interval)::date
		ORDER BY date ASC
	`, days)
	if err != nil {
		// Fallback: inline aggregation without symptom join
		rows, err = h.db.QueryContext(r.Context(), `
			SELECT
				DATE(timestamp AT TIME ZONE 'UTC')       AS day,
				ROUND(AVG(pm25)::numeric, 1)             AS avg_pm25,
				ROUND(AVG(voc)::numeric,  0)             AS avg_voc,
				ROUND(AVG(aqi)::numeric,  0)::int        AS avg_aqi,
				0                                        AS symptoms
			FROM sensor_readings
			WHERE timestamp >= NOW() - ($1::int || ' days')::interval
			GROUP BY day
			ORDER BY day ASC
		`, days)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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

// ── alerts ───────────────────────────────────────────────────────────────────

func (h *Handler) ListAlerts(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, message, level, read, created_at
		FROM alerts
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	alerts := []models.Alert{}
	for rows.Next() {
		var a models.Alert
		if err := rows.Scan(&a.ID, &a.Message, &a.Level, &a.Read, &a.CreatedAt); err != nil {
			continue
		}
		alerts = append(alerts, a)
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (h *Handler) MarkAlertRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.db.ExecContext(r.Context(), `UPDATE alerts SET read = TRUE WHERE id = $1`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── symptoms ─────────────────────────────────────────────────────────────────

func (h *Handler) ListSymptoms(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, symptoms, severity, triggers, notes, created_at
		FROM symptom_logs
		ORDER BY created_at DESC
		LIMIT 50
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	logs := []models.SymptomLog{}
	for rows.Next() {
		var s models.SymptomLog
		var sympJSON, trigJSON []byte
		if err := rows.Scan(&s.ID, &sympJSON, &s.Severity, &trigJSON, &s.Notes, &s.CreatedAt); err != nil {
			continue
		}
		json.Unmarshal(sympJSON, &s.Symptoms)
		json.Unmarshal(trigJSON, &s.Triggers)
		if s.Symptoms == nil {
			s.Symptoms = []string{}
		}
		if s.Triggers == nil {
			s.Triggers = []string{}
		}
		logs = append(logs, s)
	}
	writeJSON(w, http.StatusOK, logs)
}

type createSymptomReq struct {
	Symptoms []string `json:"symptoms"`
	Severity int      `json:"severity"`
	Triggers []string `json:"triggers"`
	Notes    string   `json:"notes"`
}

func (h *Handler) CreateSymptom(w http.ResponseWriter, r *http.Request) {
	var req createSymptomReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Severity < 1 || req.Severity > 3 {
		http.Error(w, "severity must be 1, 2, or 3", http.StatusBadRequest)
		return
	}
	if req.Symptoms == nil {
		req.Symptoms = []string{}
	}
	if req.Triggers == nil {
		req.Triggers = []string{}
	}

	sympJSON, _ := json.Marshal(req.Symptoms)
	trigJSON, _ := json.Marshal(req.Triggers)
	now := time.Now().UTC()

	var id int64
	err := h.db.QueryRowContext(r.Context(), `
		INSERT INTO symptom_logs (symptoms, severity, triggers, notes, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, sympJSON, req.Severity, trigJSON, req.Notes, now).Scan(&id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, models.SymptomLog{
		ID:        id,
		Symptoms:  req.Symptoms,
		Severity:  req.Severity,
		Triggers:  req.Triggers,
		Notes:     req.Notes,
		CreatedAt: now,
	})
}

func (h *Handler) GetSymptom(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var s models.SymptomLog
	var sympJSON, trigJSON []byte
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, symptoms, severity, triggers, notes, created_at
		FROM symptom_logs WHERE id = $1
	`, id).Scan(&s.ID, &sympJSON, &s.Severity, &trigJSON, &s.Notes, &s.CreatedAt)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.Unmarshal(sympJSON, &s.Symptoms)
	json.Unmarshal(trigJSON, &s.Triggers)
	writeJSON(w, http.StatusOK, s)
}

// helper kept for compatibility — pgx scans timestamps natively
func round1dp(v float64) float64 {
	return math.Round(v*10) / 10
}
