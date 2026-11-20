// Package api is proxier's HTTP JSON API, WebSocket feed and UI host.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"proxier/internal/app"
	"proxier/internal/auth"
	"proxier/internal/model"
	"proxier/internal/store"
)

const cookieName = "proxier_session"

type Server struct {
	app  *app.App
	auth *auth.Service
	hub  *Hub
	log  *slog.Logger
	tls  bool
}

// New builds the root handler. ui serves the embedded single-page app.
func New(a *app.App, authSvc *auth.Service, ui http.Handler, tlsEnabled bool) http.Handler {
	s := &Server{app: a, auth: authSvc, log: a.Log, tls: tlsEnabled}
	s.hub = NewHub(a)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(s.securityHeaders)

	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.NoCache)
		r.Use(s.csrf)
		r.Use(bodyLimit(1 << 20))

		r.Get("/auth/status", s.authStatus)
		r.Post("/auth/setup", s.setup)
		r.Post("/auth/login", s.login)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.logout)
			r.Post("/auth/password", s.changePassword)

			r.Get("/rules", s.listRules)
			r.Post("/rules", s.createRule)
			r.Post("/rules/check", s.checkRule)
			r.Get("/rules/{id}", s.getRule)
			r.Put("/rules/{id}", s.updateRule)
			r.Delete("/rules/{id}", s.deleteRule)
			r.Post("/rules/{id}/enable", s.setEnabled(true))
			r.Post("/rules/{id}/disable", s.setEnabled(false))

			r.Get("/connections", s.listConnections)
			r.Delete("/connections/{id}", s.kick)
			r.Post("/connections/{id}/ban", s.banConnection)

			r.Get("/bans", s.listBans)
			r.Post("/bans", s.createBan)
			r.Delete("/bans/{id}", s.deleteBan)

			r.Get("/stats/live", s.statsLive)
			r.Get("/stats/traffic", s.statsTraffic)

			r.Get("/events", s.listEvents)

			r.Get("/presets", s.listPresets)
			r.Post("/presets", s.createPreset)
			r.Delete("/presets/{id}", s.deletePreset)
			r.Post("/presets/{id}/apply", s.applyPreset)

			r.Get("/settings", s.getSettings)
			r.Put("/settings", s.putSettings)
			r.Get("/system", s.system)

			r.Get("/export", s.export)
			r.With(bodyLimit(16<<20)).Post("/import", s.importConfig)

			r.Get("/ws", s.hub.ServeWS)
		})
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, http.StatusNotFound, "not found")
		})
	})
	r.Handle("/*", ui)
	return r
}

// Hub exposes the WebSocket hub (for shutdown).
func (s *Server) Hub() *Hub { return s.hub }

// ---------- middleware ----------

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
			"script-src 'self'; connect-src 'self' ws: wss:; font-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if s.isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// csrf requires a custom header on state-changing requests. Browsers can't
// send it cross-site without a CORS preflight, which we never approve.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("X-Proxier") != "1" {
				writeErr(w, http.StatusForbidden, "missing X-Proxier header")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func bodyLimit(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

type ctxKey int

const tokenKey ctxKey = 1

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "not logged in")
			return
		}
		ok, err := s.auth.Validate(r.Context(), c.Value, s.clientIP(r))
		if err != nil {
			s.internal(w, err)
			return
		}
		if !ok {
			s.clearCookie(w, r)
			writeErr(w, http.StatusUnauthorized, "session expired")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tokenKey, c.Value)))
	})
}

func (s *Server) clientIP(r *http.Request) string {
	if s.app.Cfg.TrustProxy {
		if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
			return v
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			parts := strings.Split(v, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return s.app.Cfg.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: s.isHTTPS(r), MaxAge: int(auth.SessionTTL.Seconds()),
	})
}

func (s *Server) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: s.isHTTPS(r), MaxAge: -1, Expires: time.Unix(0, 0)})
}

// ---------- JSON helpers ----------

type errBody struct {
	Error  string            `json:"error"`
	Fields map[string]string `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errBody{Error: msg})
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	s.log.Error("api error", "err", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

// fail maps domain errors to HTTP responses.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var fe model.FieldErrors
	switch {
	case errors.As(err, &fe):
		writeJSON(w, http.StatusBadRequest, errBody{Error: "validation failed", Fields: fe})
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, auth.ErrRateLimited):
		writeErr(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, auth.ErrBadCredentials), errors.Is(err, auth.ErrBadSetupToken):
		writeErr(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, auth.ErrWeakPassword):
		writeJSON(w, http.StatusBadRequest, errBody{Error: err.Error(), Fields: map[string]string{"password": err.Error()}})
	case errors.Is(err, auth.ErrAlreadySetup):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		s.internal(w, err)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else if errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "empty request body")
		} else {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		}
		return false
	}
	return true
}
