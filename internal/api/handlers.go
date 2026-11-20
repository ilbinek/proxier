package api

import (
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"proxier/internal/app"
	"proxier/internal/auth"
	"proxier/internal/model"
	"proxier/internal/store"
)

func idParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

// ---------- auth ----------

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	need, err := s.auth.NeedsSetup(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	authed := false
	if c, err := r.Cookie(cookieName); err == nil {
		authed, _ = s.auth.Validate(r.Context(), c.Value, s.clientIP(r))
	}
	writeJSON(w, http.StatusOK, map[string]any{"needs_setup": need, "authenticated": authed, "version": s.app.Version})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	ip := s.clientIP(r)
	if err := s.auth.Setup(r.Context(), req.Token, req.Password, ip); err != nil {
		if err == auth.ErrBadSetupToken {
			s.app.Emit("warn", "setup_failed", "Setup attempted with an invalid token", nil, ip)
		}
		s.fail(w, err)
		return
	}
	s.app.Emit("info", "setup_complete", "Admin password set", nil, ip)
	tok, err := s.auth.NewSession(r.Context(), ip, r.UserAgent())
	if err != nil {
		s.internal(w, err)
		return
	}
	s.setCookie(w, r, tok)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	ip := s.clientIP(r)
	tok, err := s.auth.Login(r.Context(), req.Password, ip, r.UserAgent())
	if err != nil {
		switch err {
		case auth.ErrBadCredentials:
			s.app.Emit("warn", "login_failed", "Failed login attempt", nil, ip)
		case auth.ErrRateLimited:
			s.app.Emit("warn", "login_throttled", "Login attempts throttled", nil, ip)
		}
		s.fail(w, err)
		return
	}
	s.app.Emit("info", "login", "Admin logged in", nil, ip)
	s.setCookie(w, r, tok)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	tok, _ := r.Context().Value(tokenKey).(string)
	_ = s.auth.Logout(r.Context(), tok)
	s.clearCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	tok, _ := r.Context().Value(tokenKey).(string)
	ip := s.clientIP(r)
	if err := s.auth.ChangePassword(r.Context(), req.Current, req.New, tok, ip); err != nil {
		if err == auth.ErrBadCredentials {
			writeJSON(w, http.StatusBadRequest, errBody{Error: "current password is wrong", Fields: map[string]string{"current": "wrong password"}})
			return
		}
		if err == auth.ErrWeakPassword {
			writeJSON(w, http.StatusBadRequest, errBody{Error: err.Error(), Fields: map[string]string{"new": err.Error()}})
			return
		}
		s.fail(w, err)
		return
	}
	s.app.Emit("warn", "password_changed", "Admin password changed; other sessions revoked", nil, ip)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- rules ----------

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	views, err := s.app.RuleViews(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) getRule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	v, err := s.app.RuleView(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	var rule model.Rule
	if !decode(w, r, &rule) {
		return
	}
	if err := s.app.CreateRule(r.Context(), &rule); err != nil {
		s.fail(w, err)
		return
	}
	v, err := s.app.RuleView(r.Context(), rule.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) checkRule(w http.ResponseWriter, r *http.Request) {
	var rule model.Rule
	if !decode(w, r, &rule) {
		return
	}
	if err := s.app.CheckRule(r.Context(), &rule); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var rule model.Rule
	if !decode(w, r, &rule) {
		return
	}
	rule.ID = id
	if err := s.app.UpdateRule(r.Context(), &rule); err != nil {
		s.fail(w, err)
		return
	}
	v, err := s.app.RuleView(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.app.DeleteRule(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setEnabled(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(w, r)
		if !ok {
			return
		}
		if err := s.app.SetRuleEnabled(r.Context(), id, enabled); err != nil {
			s.fail(w, err)
			return
		}
		v, err := s.app.RuleView(r.Context(), id)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

// ---------- connections & bans ----------

func (s *Server) listConnections(w http.ResponseWriter, r *http.Request) {
	ruleID, _ := strconv.ParseInt(r.URL.Query().Get("rule"), 10, 64)
	writeJSON(w, http.StatusOK, s.app.Proxy.Registry.List(ruleID))
}

func (s *Server) kick(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	e := s.app.Proxy.Registry.Get(id)
	if e == nil || !s.app.Proxy.Kick(id) {
		writeErr(w, http.StatusNotFound, "connection not found")
		return
	}
	rid := e.RuleID
	s.app.Emit("info", "conn_kicked", fmt.Sprintf("Kicked %s %s", strings.ToUpper(string(e.Proto)), e.Client), &rid, e.Client.Addr().Unmap().String())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) banConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Global bool   `json:"global"`
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	b, err := s.app.BanConnection(r.Context(), chi.URLParam(r, "id"), req.Global, req.Reason)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) listBans(w http.ResponseWriter, r *http.Request) {
	bans, err := s.app.Store.ListBans(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bans)
}

func (s *Server) createBan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CIDR       string `json:"cidr"`
		RuleID     *int64 `json:"rule_id"`
		Reason     string `json:"reason"`
		ExpiresInS int64  `json:"expires_in_s"` // 0 = permanent
	}
	if !decode(w, r, &req) {
		return
	}
	b := &model.Ban{CIDR: req.CIDR, RuleID: req.RuleID, Reason: req.Reason}
	if req.ExpiresInS > 0 {
		exp := time.Now().Unix() + req.ExpiresInS
		b.ExpiresAt = &exp
	}
	if err := s.app.AddBan(r.Context(), b); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) deleteBan(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.app.RemoveBan(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- stats & events ----------

func (s *Server) statsLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"history":     s.app.Stats.History(),
		"connections": s.app.Proxy.Registry.Count(),
	})
}

var trafficRanges = map[string]struct{ span, bucket time.Duration }{
	"1h":  {time.Hour, time.Minute},
	"6h":  {6 * time.Hour, 5 * time.Minute},
	"24h": {24 * time.Hour, 15 * time.Minute},
	"7d":  {7 * 24 * time.Hour, time.Hour},
	"30d": {30 * 24 * time.Hour, 6 * time.Hour},
	"1y":  {365 * 24 * time.Hour, 24 * time.Hour},
}

func (s *Server) statsTraffic(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rg, ok := trafficRanges[q.Get("range")]
	if !ok {
		rg = trafficRanges["24h"]
	}
	var ruleID *int64
	if v := q.Get("rule"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid rule")
			return
		}
		ruleID = &id
	}
	bucket := int64(rg.bucket.Seconds())
	to := time.Now().Unix()
	from := (to - int64(rg.span.Seconds())) / bucket * bucket
	pts, err := s.app.Store.QueryTraffic(r.Context(), ruleID, from, to+1, bucket)
	if err != nil {
		s.internal(w, err)
		return
	}
	// Fill gaps with zeros so charts have a continuous axis.
	byTS := make(map[int64]model.TrafficPoint, len(pts))
	for _, p := range pts {
		byTS[p.TS] = p
	}
	filled := make([]model.TrafficPoint, 0, (to-from)/bucket+1)
	var sumIn, sumOut, sumConns uint64
	for ts := from; ts <= to; ts += bucket {
		p, ok := byTS[ts]
		if !ok {
			p = model.TrafficPoint{TS: ts}
		}
		sumIn += p.BytesIn
		sumOut += p.BytesOut
		sumConns += p.ConnsOpened
		filled = append(filled, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bucket_s": bucket, "points": filled,
		"total_in": sumIn, "total_out": sumOut, "total_conns": sumConns,
	})
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	eq := store.EventQuery{Level: q.Get("level"), Type: q.Get("type"), Q: strings.TrimSpace(q.Get("q"))}
	if v := q.Get("rule"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			eq.RuleID = &id
		}
	}
	eq.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	eq.Limit, _ = strconv.Atoi(q.Get("limit"))
	evs, err := s.app.Store.ListEvents(r.Context(), eq)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, evs)
}

// ---------- presets ----------

func (s *Server) listPresets(w http.ResponseWriter, r *http.Request) {
	ps, err := s.app.Presets(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) createPreset(w http.ResponseWriter, r *http.Request) {
	var req app.PresetFromRulesRequest
	if !decode(w, r, &req) {
		return
	}
	p, err := s.app.PresetFromRules(r.Context(), req)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) deletePreset(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeletePreset(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) applyPreset(w http.ResponseWriter, r *http.Request) {
	var req app.ApplyPresetRequest
	if !decode(w, r, &req) {
		return
	}
	rules, err := s.app.ApplyPreset(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rules)
}

// ---------- settings, system, export ----------

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.app.GetSettings(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var st app.Settings
	if !decode(w, r, &st) {
		return
	}
	if err := s.app.SaveSettings(r.Context(), st); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Cfg
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         s.app.Version,
		"go_version":      runtime.Version(),
		"os":              runtime.GOOS + "/" + runtime.GOARCH,
		"started_at":      s.app.StartedAt.UnixMilli(),
		"listen":          cfg.Listen,
		"tls_mode":        cfg.TLSMode,
		"acme_domain":     cfg.ACMEDomain,
		"data_dir":        cfg.DataDir,
		"trust_proxy":     cfg.TrustProxy,
		"interface_ips":   app.InterfaceIPs(),
		"guessed_address": app.GuessPublicAddress(),
		"connections":     s.app.Proxy.Registry.Count(),
	})
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	doc, err := s.app.Export(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	b, err := app.MarshalExport(doc)
	if err != nil {
		s.internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="proxier-export-%s.json"`, time.Now().Format("2006-01-02")))
	w.Write(b)
}

func (s *Server) importConfig(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode != "replace" {
		mode = "merge"
	}
	var doc app.ExportDoc
	if !decode(w, r, &doc) {
		return
	}
	res, err := s.app.Import(r.Context(), &doc, mode)
	if err != nil {
		if _, ok := err.(model.FieldErrors); ok {
			s.fail(w, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
