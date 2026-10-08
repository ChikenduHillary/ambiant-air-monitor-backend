package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type deviceInfo struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"created_at"`
	LastSeenAt *string `json:"last_seen_at"`
}

// ListDevices returns the authenticated user's own devices. The key itself
// is never included — only CreateDevice's response ever shows it.
func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, name, created_at, last_seen_at FROM devices
		WHERE user_id = $1 ORDER BY created_at DESC
	`, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	devices := []deviceInfo{}
	for rows.Next() {
		var d deviceInfo
		var created time.Time
		var lastSeen *time.Time
		if err := rows.Scan(&d.ID, &d.Name, &created, &lastSeen); err != nil {
			continue
		}
		d.CreatedAt = created.UTC().Format(time.RFC3339)
		if lastSeen != nil {
			s := lastSeen.UTC().Format(time.RFC3339)
			d.LastSeenAt = &s
		}
		devices = append(devices, d)
	}
	writeJSON(w, http.StatusOK, devices)
}

type createDeviceReq struct {
	Name string `json:"name"`
}

// CreateDevice registers a new device for the authenticated user and
// returns its plaintext key — the one and only time it's ever shown, since
// only its SHA-256 hash is stored from here on.
func (h *Handler) CreateDevice(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)

	var req createDeviceReq
	json.NewDecoder(r.Body).Decode(&req)
	if req.Name == "" {
		req.Name = "AeroGuard Node"
	}

	rawKey, err := generateDeviceKey()
	if err != nil {
		http.Error(w, "failed to generate device key", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(sum[:])

	var id int64
	var createdAt time.Time
	err = h.db.QueryRowContext(r.Context(), `
		INSERT INTO devices (user_id, name, key_hash) VALUES ($1, $2, $3)
		RETURNING id, created_at
	`, userID, req.Name, keyHash).Scan(&id, &createdAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         id,
		"name":       req.Name,
		"created_at": createdAt.UTC().Format(time.RFC3339),
		"device_key": rawKey,
	})
}

// DeleteDevice revokes a device. Past readings/alerts it produced are kept
// (device_id is set NULL via the FK), only user_id still ties them to the
// account, so history doesn't disappear when a device is replaced.
func (h *Handler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	userID := currentUserID(r)
	id := chi.URLParam(r, "id")

	if _, err := h.db.ExecContext(r.Context(),
		`DELETE FROM devices WHERE id = $1 AND user_id = $2`, id, userID,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func generateDeviceKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "agd_" + hex.EncodeToString(b), nil
}
