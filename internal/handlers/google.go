package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// googleOAuthConfig is built lazily from env vars on first call.
func (h *Handler) googleOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     h.googleClientID,
		ClientSecret: h.googleClientSecret,
		RedirectURL:  h.googleRedirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

// GoogleLogin redirects the user to Google's OAuth consent page.
func (h *Handler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if h.googleClientID == "" || h.googleClientSecret == "" {
		http.Error(w, "Google OAuth not configured — set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET", http.StatusNotImplemented)
		return
	}

	state := randomState()
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	url := h.googleOAuthConfig().AuthCodeURL(state, oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// GoogleCallback handles the OAuth callback from Google.
func (h *Handler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	// Validate state
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", MaxAge: -1, Path: "/"})

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	tokenOAuth, err := h.googleOAuthConfig().Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "failed to exchange code: "+err.Error(), http.StatusInternalServerError)
		return
	}

	gUser, err := fetchGoogleUser(tokenOAuth.AccessToken)
	if err != nil {
		http.Error(w, "failed to fetch Google user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Upsert user: find by email or create new
	userID, err := h.upsertGoogleUser(r.Context(), gUser)
	if err != nil {
		http.Error(w, "failed to upsert user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jwtToken, err := h.issueToken(userID)
	if err != nil {
		http.Error(w, "failed to issue token", http.StatusInternalServerError)
		return
	}

	// Redirect to frontend with token as query param; frontend stores it.
	frontendURL := fmt.Sprintf("%s?token=%s", h.frontendURL+"/auth/callback", jwtToken)
	http.Redirect(w, r, frontendURL, http.StatusTemporaryRedirect)
}

type googleUserInfo struct {
	Sub   string `json:"sub"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func fetchGoogleUser(accessToken string) (*googleUserInfo, error) {
	resp, err := http.Get("https://www.googleapis.com/oauth2/v3/userinfo?access_token=" + accessToken)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var u googleUserInfo
	return &u, json.NewDecoder(resp.Body).Decode(&u)
}

func (h *Handler) upsertGoogleUser(ctx context.Context, g *googleUserInfo) (int64, error) {
	// Return existing user if found
	var id int64
	err := h.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, g.Email).Scan(&id)
	if err == nil {
		return id, nil
	}

	// First Google user gets admin role
	var count int
	h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	role := "user"
	if count == 0 {
		role = "admin"
	}

	patientID := fmt.Sprintf("#%04d", time.Now().UnixNano()%10000)
	err = h.db.QueryRowContext(ctx, `
		INSERT INTO users (name, email, password_hash, condition, patient_id, threshold, role)
		VALUES ($1, $2, '', 'Asthma', $3, 75, $4)
		RETURNING id
	`, g.Name, g.Email, patientID, role).Scan(&id)
	return id, err
}

func randomState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}
