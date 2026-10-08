package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/middleware"
	"github.com/chikenduhillary/ambiant-air-monitor-api/internal/models"
)

type registerReq struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Password  string `json:"password"`
	Condition string `json:"condition"`
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string         `json:"token"`
	User  models.AuthUser `json:"user"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.Name == "" || req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, email, and password are required"})
		return
	}
	if len(req.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		return
	}
	if req.Condition == "" {
		req.Condition = "Asthma"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	patientID := fmt.Sprintf("#%04d", time.Now().UnixNano()%10000)

	// First user ever becomes admin automatically.
	var userCount int
	h.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users").Scan(&userCount)
	role := "user"
	if userCount == 0 {
		role = "admin"
	}

	var id int64
	err = h.db.QueryRowContext(r.Context(), `
		INSERT INTO users (name, email, password_hash, condition, patient_id, threshold, role)
		VALUES ($1, $2, $3, $4, $5, 75, $6)
		RETURNING id
	`, req.Name, req.Email, string(hash), req.Condition, patientID, role).Scan(&id)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "email already registered"})
		return
	}

	user := models.AuthUser{
		ID:        id,
		Name:      req.Name,
		Email:     req.Email,
		Condition: req.Condition,
		PatientID: patientID,
		Threshold: 75,
		Role:      role,
	}

	token, err := h.issueToken(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not issue token"})
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{Token: token, User: user})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	var user models.AuthUser
	var hash string
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, name, email, password_hash, condition, patient_id, threshold, role, avatar_url
		FROM users WHERE email = $1
	`, req.Email).Scan(&user.ID, &user.Name, &user.Email, &hash, &user.Condition, &user.PatientID, &user.Threshold, &user.Role, &user.AvatarURL)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
		return
	}

	token, err := h.issueToken(user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not issue token"})
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token, User: user})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	idStr, _ := r.Context().Value(middleware.UserIDKey).(string)
	id, _ := strconv.ParseInt(idStr, 10, 64)

	var user models.AuthUser
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, name, email, condition, patient_id, threshold, role, avatar_url
		FROM users WHERE id = $1
	`, id).Scan(&user.ID, &user.Name, &user.Email, &user.Condition, &user.PatientID, &user.Threshold, &user.Role, &user.AvatarURL)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) issueToken(userID int64) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(h.secret)
}
