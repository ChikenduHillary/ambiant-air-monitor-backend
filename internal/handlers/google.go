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

func (h *Handler) googleOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     h.googleClientID,
		ClientSecret: h.googleClientSecret,
		RedirectURL:  h.googleRedirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

// oauthState is JSON-encoded then base64-encoded into the OAuth state param.
// This lets us carry the mobile app_redirect through the Google OAuth round-trip
// without needing a separate cookie.
type oauthState struct {
	CSRF        string `json:"c"`
	AppRedirect string `json:"r,omitempty"` // non-empty for mobile clients
}

func buildState(appRedirect string) (string, error) {
	b := make([]byte, 12)
	rand.Read(b)
	s := oauthState{
		CSRF:        base64.RawURLEncoding.EncodeToString(b),
		AppRedirect: appRedirect,
	}
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func parseState(raw string) (*oauthState, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var s oauthState
	return &s, json.Unmarshal(data, &s)
}

// GoogleLogin redirects to Google's OAuth consent page.
// Optional query param: app_redirect — the URI the mobile app wants the token delivered to.
func (h *Handler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if h.googleClientID == "" || h.googleClientSecret == "" {
		http.Error(w, "Google OAuth not configured — set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET", http.StatusNotImplemented)
		return
	}

	appRedirect := r.URL.Query().Get("app_redirect")
	stateStr, err := buildState(appRedirect)
	if err != nil {
		http.Error(w, "state error", http.StatusInternalServerError)
		return
	}

	// Store the CSRF portion in a cookie for validation in the callback.
	parsed, _ := parseState(stateStr)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_csrf",
		Value:    parsed.CSRF,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	url := h.googleOAuthConfig().AuthCodeURL(stateStr, oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// GoogleCallback handles the redirect from Google after user consent.
func (h *Handler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	stateStr := r.URL.Query().Get("state")
	state, err := parseState(stateStr)
	if err != nil {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	// Validate CSRF
	csrfCookie, err := r.Cookie("oauth_csrf")
	if err != nil || csrfCookie.Value != state.CSRF {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oauth_csrf", MaxAge: -1, Path: "/"})

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

	// Mobile clients pass app_redirect; deliver token there.
	// Web clients use the standard frontend callback page.
	var dest string
	if state.AppRedirect != "" {
		dest = state.AppRedirect + "?token=" + jwtToken
	} else {
		dest = h.frontendURL + "/auth/callback?token=" + jwtToken
	}
	http.Redirect(w, r, dest, http.StatusTemporaryRedirect)
}

type googleUserInfo struct {
	Sub     string `json:"sub"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Picture string `json:"picture"`
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
	var id int64
	if err := h.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, g.Email).Scan(&id); err == nil {
		// Keep the avatar fresh in case the user's Google photo changed.
		if g.Picture != "" {
			h.db.ExecContext(ctx, `UPDATE users SET avatar_url = $1 WHERE id = $2`, g.Picture, id)
		}
		return id, nil
	}

	var count int
	h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	role := "user"
	if count == 0 {
		role = "admin"
	}

	var avatarURL *string
	if g.Picture != "" {
		avatarURL = &g.Picture
	}

	patientID := fmt.Sprintf("#%04d", time.Now().UnixNano()%10000)
	err := h.db.QueryRowContext(ctx, `
		INSERT INTO users (name, email, password_hash, condition, patient_id, threshold, role, avatar_url)
		VALUES ($1, $2, '', 'Asthma', $3, 75, $4, $5)
		RETURNING id
	`, g.Name, g.Email, patientID, role, avatarURL).Scan(&id)
	return id, err
}
