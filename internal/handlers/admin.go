package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/models"
)

// ── Stats ─────────────────────────────────────────────────────────────────────

func (h *Handler) AdminStats(w http.ResponseWriter, r *http.Request) {
	var stats models.AdminStats

	h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&stats.TotalUsers)
	h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sensor_readings`).Scan(&stats.TotalReadings)
	h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM alerts`).Scan(&stats.TotalAlerts)
	h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM alerts WHERE read = FALSE`).Scan(&stats.ActiveAlerts)
	h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM symptom_logs`).Scan(&stats.TotalSymptoms)
	h.db.QueryRowContext(r.Context(), `
		SELECT ROUND(AVG(aqi)::numeric, 1)
		FROM sensor_readings
		WHERE timestamp >= NOW() - INTERVAL '24 hours'
	`).Scan(&stats.AvgAQIToday)

	var lastTs time.Time
	if err := h.db.QueryRowContext(r.Context(), `SELECT timestamp FROM sensor_readings ORDER BY timestamp DESC LIMIT 1`).Scan(&lastTs); err == nil {
		s := lastTs.UTC().Format(time.RFC3339)
		stats.LastReadingAt = &s
	}

	writeJSON(w, http.StatusOK, stats)
}

// ── Users ─────────────────────────────────────────────────────────────────────

func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT
			u.id, u.name, u.email, u.condition, u.patient_id, u.threshold, u.role,
			u.created_at,
			(SELECT COUNT(*) FROM symptom_logs sl WHERE sl.user_id = u.id) AS symptoms_count
		FROM users u
		ORDER BY u.created_at DESC
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := []models.AdminUser{}
	for rows.Next() {
		var u models.AdminUser
		var ts time.Time
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Condition, &u.PatientID,
			&u.Threshold, &u.Role, &ts, &u.SymptomsCount); err != nil {
			continue
		}
		u.CreatedAt = ts.UTC().Format(time.RFC3339)
		users = append(users, u)
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) AdminGetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var u models.AdminUser
	var ts time.Time
	err := h.db.QueryRowContext(r.Context(), `
		SELECT
			u.id, u.name, u.email, u.condition, u.patient_id, u.threshold, u.role,
			u.created_at,
			(SELECT COUNT(*) FROM symptom_logs sl WHERE sl.user_id = u.id) AS symptoms_count
		FROM users u WHERE u.id = $1
	`, id).Scan(&u.ID, &u.Name, &u.Email, &u.Condition, &u.PatientID,
		&u.Threshold, &u.Role, &ts, &u.SymptomsCount)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	u.CreatedAt = ts.UTC().Format(time.RFC3339)
	writeJSON(w, http.StatusOK, u)
}

type updateUserReq struct {
	Role      *string `json:"role"`
	Threshold *int    `json:"threshold"`
	Condition *string `json:"condition"`
	Name      *string `json:"name"`
}

func (h *Handler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req updateUserReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Build update dynamically
	query := `UPDATE users SET`
	args := []any{}
	idx := 1

	if req.Name != nil {
		query += ` name = $` + strconv.Itoa(idx) + `,`
		args = append(args, *req.Name)
		idx++
	}
	if req.Role != nil {
		if *req.Role != "user" && *req.Role != "admin" {
			http.Error(w, "role must be 'user' or 'admin'", http.StatusBadRequest)
			return
		}
		query += ` role = $` + strconv.Itoa(idx) + `,`
		args = append(args, *req.Role)
		idx++
	}
	if req.Threshold != nil {
		query += ` threshold = $` + strconv.Itoa(idx) + `,`
		args = append(args, *req.Threshold)
		idx++
	}
	if req.Condition != nil {
		query += ` condition = $` + strconv.Itoa(idx) + `,`
		args = append(args, *req.Condition)
		idx++
	}

	if idx == 1 {
		http.Error(w, "nothing to update", http.StatusBadRequest)
		return
	}

	// Trim trailing comma, add WHERE
	query = query[:len(query)-1] + ` WHERE id = $` + strconv.Itoa(idx)
	args = append(args, id)

	if _, err := h.db.ExecContext(r.Context(), query, args...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated user
	h.AdminGetUser(w, r)
}

func (h *Handler) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Prevent deleting the last admin
	var adminCount int
	h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&adminCount)
	var targetRole string
	h.db.QueryRowContext(r.Context(), `SELECT role FROM users WHERE id = $1`, id).Scan(&targetRole)
	if targetRole == "admin" && adminCount <= 1 {
		http.Error(w, "cannot delete the last admin", http.StatusConflict)
		return
	}

	if _, err := h.db.ExecContext(r.Context(), `DELETE FROM users WHERE id = $1`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── Alerts ────────────────────────────────────────────────────────────────────

func (h *Handler) AdminListAlerts(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, message, level, read, created_at
		FROM alerts ORDER BY created_at DESC LIMIT $1
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

type broadcastAlertReq struct {
	Message string `json:"message"`
	Level   string `json:"level"`
}

func (h *Handler) AdminBroadcastAlert(w http.ResponseWriter, r *http.Request) {
	var req broadcastAlertReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}
	if req.Level != "warning" && req.Level != "info" && req.Level != "success" {
		req.Level = "info"
	}

	var a models.Alert
	err := h.db.QueryRowContext(r.Context(), `
		INSERT INTO alerts (message, level) VALUES ($1, $2)
		RETURNING id, message, level, read, created_at
	`, req.Message, req.Level).Scan(&a.ID, &a.Message, &a.Level, &a.Read, &a.CreatedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (h *Handler) AdminDeleteAlert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.db.ExecContext(r.Context(), `DELETE FROM alerts WHERE id = $1`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) AdminMarkAllAlertsRead(w http.ResponseWriter, r *http.Request) {
	if _, err := h.db.ExecContext(r.Context(), `UPDATE alerts SET read = TRUE WHERE read = FALSE`); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Sensor Readings ───────────────────────────────────────────────────────────

func (h *Handler) AdminListReadings(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, timestamp, pm25, voc, temperature, humidity, aqi
		FROM sensor_readings ORDER BY timestamp DESC LIMIT $1
	`, limit)
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

func (h *Handler) AdminDeleteReading(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.db.ExecContext(r.Context(), `DELETE FROM sensor_readings WHERE id = $1`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// AdminExportReadings returns sensor readings as CSV.
func (h *Handler) AdminExportReadings(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT timestamp, pm25, voc, temperature, humidity, aqi
		FROM sensor_readings ORDER BY timestamp DESC LIMIT 10000
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="sensor_readings.csv"`)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("timestamp,pm25,voc,temperature,humidity,aqi\n"))

	for rows.Next() {
		var s models.SensorReading
		if err := rows.Scan(&s.Timestamp, &s.PM25, &s.VOC, &s.Temperature, &s.Humidity, &s.AQI); err != nil {
			continue
		}
		line := s.Timestamp.UTC().Format(time.RFC3339) + "," +
			strconv.FormatFloat(s.PM25, 'f', 1, 64) + "," +
			strconv.FormatFloat(s.VOC, 'f', 0, 64) + "," +
			strconv.FormatFloat(s.Temperature, 'f', 1, 64) + "," +
			strconv.FormatFloat(s.Humidity, 'f', 0, 64) + "," +
			strconv.Itoa(s.AQI) + "\n"
		w.Write([]byte(line))
	}
}

// ── Symptom Logs ──────────────────────────────────────────────────────────────

func (h *Handler) AdminListSymptoms(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT sl.id, sl.symptoms, sl.severity, sl.triggers, sl.notes, sl.created_at,
		       u.name AS user_name, u.email AS user_email
		FROM symptom_logs sl
		LEFT JOIN users u ON u.id = sl.user_id
		ORDER BY sl.created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type adminSymptomLog struct {
		models.SymptomLog
		UserName  string `json:"user_name"`
		UserEmail string `json:"user_email"`
	}

	logs := []adminSymptomLog{}
	for rows.Next() {
		var s adminSymptomLog
		var sympJSON, trigJSON []byte
		if err := rows.Scan(&s.ID, &sympJSON, &s.Severity, &trigJSON, &s.Notes, &s.CreatedAt,
			&s.UserName, &s.UserEmail); err != nil {
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
