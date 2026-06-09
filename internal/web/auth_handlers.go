package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if body.Email == "" || body.Password == "" {
		writeError(w, 400, "bad_request", "email and password required")
		return
	}
	cfg, err := s.store.GetAuthConfig(r.Context())
	if err != nil || cfg.Provider != "local" {
		writeError(w, 400, "bad_request", "local auth not enabled")
		return
	}
	localProvider := auth.NewLocalProvider(s.store)
	info, err := localProvider.VerifyPassword(r.Context(), body.Email, body.Password)
	if err != nil {
		writeError(w, 401, "unauthorized", "invalid credentials")
		return
	}
	sessionToken, tokenHash := generateToken()
	user, _ := s.store.GetUserByEmail(r.Context(), info.Email)
	if user == nil {
		writeError(w, 401, "unauthorized", "user not found")
		return
	}
	sess := storage.Session{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := s.store.CreateSession(r.Context(), sess); err != nil {
		writeError(w, 500, "internal_error", "create session failed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
	writeJSON(w, map[string]string{"ok": "true", "email": info.Email})
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetAuthConfig(r.Context())
	if err != nil || cfg.Provider != "oidc" {
		writeError(w, 400, "bad_request", "OIDC not configured")
		return
	}
	stateToken, stateHash := generateToken()
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    stateHash,
		Path:     "/auth/oidc/callback",
		HttpOnly: true,
		Secure:   true,
		MaxAge:   300,
	})
	p, err := auth.NewOIDCProvider(r.Context(), cfg.OIDCIssuer, cfg.OIDCClientID, s.oidcSecret, cfg.OIDCRedirectURL)
	if err != nil {
		writeError(w, 500, "internal_error", "OIDC provider init failed")
		return
	}
	http.Redirect(w, r, p.AuthURL(stateToken), http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	cfg, _ := s.store.GetAuthConfig(r.Context())
	if cfg == nil || cfg.Provider != "oidc" {
		writeError(w, 400, "bad_request", "OIDC not configured")
		return
	}
	// Validate CSRF state before touching the auth code
	stateParam := r.URL.Query().Get("state")
	stateCookie, err := r.Cookie("oidc_state")
	if err != nil || stateParam == "" || auth.HashSHA256(stateParam) != stateCookie.Value {
		writeError(w, 400, "bad_request", "invalid oauth state")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oidc_state", Value: "", Path: "/auth/oidc/callback", MaxAge: -1})

	p, err := auth.NewOIDCProvider(r.Context(), cfg.OIDCIssuer, cfg.OIDCClientID, s.oidcSecret, cfg.OIDCRedirectURL)
	if err != nil {
		writeError(w, 500, "internal_error", "OIDC provider init failed")
		return
	}
	info, err := p.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		writeError(w, 401, "unauthorized", "OIDC exchange failed")
		return
	}
	user, _ := s.store.GetUserByExternalID(r.Context(), info.ExternalID)
	if user == nil {
		user, _ = s.store.GetUserByEmail(r.Context(), info.Email)
	}
	role := "member"
	if user != nil {
		role = user.Role
	}
	uid, err := s.store.UpsertUser(r.Context(), storage.User{
		Email:      info.Email,
		Name:       info.Name,
		ExternalID: info.ExternalID,
		Role:       role,
	})
	if err != nil {
		writeError(w, 500, "internal_error", "upsert user failed")
		return
	}
	if user == nil || user.TeamID == "" {
		if team, _ := s.store.ResolveTeamByEmail(r.Context(), info.Email); team != nil {
			_ = s.store.AssignUserToTeam(r.Context(), uid, team.ID, role)
		}
	}
	sessionToken, tokenHash := generateToken()
	_ = s.store.CreateSession(r.Context(), storage.Session{
		UserID:    uid,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		tokenHash := auth.HashSHA256(cookie.Value)
		_ = s.store.DeleteSession(r.Context(), tokenHash)
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", MaxAge: -1, Path: "/"})
	writeJSON(w, map[string]any{"ok": true})
}

func generateToken() (raw, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	raw = hex.EncodeToString(b)
	hash = auth.HashSHA256(raw)
	return
}
