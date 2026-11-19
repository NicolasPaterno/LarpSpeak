package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/larpspeak/call-signaling/internal/app"
	"github.com/larpspeak/call-signaling/internal/domain"
)

const refreshCookie = "refresh"

func (h *Handler) registerAuth(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/register", h.handleRegister)
	mux.HandleFunc("POST /auth/login", h.handleLogin)
	mux.HandleFunc("POST /auth/refresh", h.handleRefresh)
	mux.HandleFunc("POST /auth/logout", h.handleLogout)
	mux.HandleFunc("GET /auth/public-key", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"alg": "EdDSA", "key": base64.RawURLEncoding.EncodeToString(h.Auth.PublicKey())})
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		if user, ok := h.user(w, r); ok {
			writeJSON(w, http.StatusOK, map[string]string{"userId": user})
		}
	})
	mux.HandleFunc("POST /sse-ticket", func(w http.ResponseWriter, r *http.Request) {
		if user, ok := h.user(w, r); ok {
			writeJSON(w, http.StatusOK, map[string]string{"ticket": h.Auth.Mint(user, domain.TokenSSE, "", time.Minute)})
		}
	})
	// SSE: EventSource cannot send headers, so it uses ?ticket=; CLI clients may send Bearer.
	mux.HandleFunc("GET /notifications", func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("ticket"); t != "" {
			c, err := h.Auth.Verify(t, domain.TokenSSE)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			h.SSE(w, r, c.Sub)
			return
		}
		if user, ok := h.user(w, r); ok {
			h.SSE(w, r, user)
		}
	})
}

// user returns the caller from the Bearer access token, or answers 401.
func (h *Handler) user(w http.ResponseWriter, r *http.Request) (string, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if ok {
		if c, err := h.Auth.Verify(tok, domain.TokenAccess); err == nil {
			return c.Sub, true
		}
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	return "", false
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func decodeCreds(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	var c credentials
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return c, false
	}
	return c, true
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	c, ok := decodeCreds(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	switch err := h.Auth.Register(ctx, c.Username, c.Password); {
	case errors.Is(err, domain.ErrTaken):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrBadCredentials):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username: 3-32 of a-z 0-9 _ -; password: 8-128 chars"})
	case err != nil:
		h.Svc.Log.Error("register failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
	default:
		writeJSON(w, http.StatusCreated, map[string]string{"userId": c.Username})
	}
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	c, ok := decodeCreds(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	t, err := h.Auth.Login(ctx, c.Username, c.Password)
	h.answerTokens(w, t, err)
}

func (h *Handler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	ck, err := r.Cookie(refreshCookie)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	t, err := h.Auth.Refresh(ctx, ck.Value)
	h.answerTokens(w, t, err)
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie(refreshCookie); err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := h.Auth.Logout(ctx, ck.Value); err != nil {
			h.Svc.Log.Error("logout failed", "err", err)
		}
	}
	h.setRefresh(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) answerTokens(w http.ResponseWriter, t app.Tokens, err error) {
	switch {
	case errors.Is(err, domain.ErrRefreshRace):
		// Do not touch the cookie: this answer can reach the browser after the
		// winning tab's Set-Cookie, and clearing it would log both tabs out.
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "refresh token just rotated, retry"})
	case app.IsAuthError(err):
		h.setRefresh(w, "", -1)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
	case err != nil:
		h.Svc.Log.Error("auth failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "try again"})
	default:
		h.setRefresh(w, t.Refresh, int(h.Auth.RefreshTTL.Seconds()))
		writeJSON(w, http.StatusOK, map[string]any{
			"accessToken": t.Access, "expiresIn": int(h.Auth.AccessTTL.Seconds()), "userId": t.UserID,
		})
	}
}

// setRefresh: httpOnly so page scripts never see it; SameSite=Strict so other
// sites cannot make the browser send it (CSRF on /auth/refresh and /auth/logout).
func (h *Handler) setRefresh(w http.ResponseWriter, v string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookie, Value: v, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: h.CookieSecure, SameSite: http.SameSiteStrictMode,
	})
}
