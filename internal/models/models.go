package models

import "time"

type SensorReading struct {
	ID          int64     `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	PM25        float64   `json:"pm25"`
	VOC         float64   `json:"voc"`
	Temperature float64   `json:"temperature"`
	Humidity    float64   `json:"humidity"`
	AQI         int       `json:"aqi"`
}

type DailyAggregate struct {
	Date     string  `json:"date"`
	PM25     float64 `json:"pm25"`
	VOC      float64 `json:"voc"`
	AQI      int     `json:"aqi"`
	Symptoms int     `json:"symptoms"`
}

type Alert struct {
	ID        int64     `json:"id"`
	Message   string    `json:"message"`
	Level     string    `json:"level"` // "warning" | "info" | "success"
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

type SymptomLog struct {
	ID        int64     `json:"id"`
	Symptoms  []string  `json:"symptoms"`
	Severity  int       `json:"severity"` // 1–3
	Triggers  []string  `json:"triggers"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
}

type AuthUser struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	Condition string  `json:"condition"`
	PatientID string  `json:"patient_id"`
	Threshold int     `json:"threshold"`
	Role      string  `json:"role"`
	AvatarURL *string `json:"avatar_url"`
}

// ── Admin types ───────────────────────────────────────────────────────────────

type AdminStats struct {
	TotalUsers     int     `json:"total_users"`
	TotalReadings  int     `json:"total_readings"`
	TotalAlerts    int     `json:"total_alerts"`
	TotalSymptoms  int     `json:"total_symptoms"`
	ActiveAlerts   int     `json:"active_alerts"`
	AvgAQIToday    float64 `json:"avg_aqi_today"`
	LastReadingAt  *string `json:"last_reading_at"`
}

type AdminUser struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Email         string  `json:"email"`
	Condition     string  `json:"condition"`
	PatientID     string  `json:"patient_id"`
	Threshold     int     `json:"threshold"`
	Role          string  `json:"role"`
	CreatedAt     string  `json:"created_at"`
	ReadingsCount int     `json:"readings_count"`
	SymptomsCount int     `json:"symptoms_count"`
}
