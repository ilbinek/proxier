// Package app wires the store, engine, events and stats together and holds the
// business logic used by the HTTP API.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"proxier/internal/config"
	"proxier/internal/events"
	"proxier/internal/model"
	"proxier/internal/proxy"
	"proxier/internal/stats"
	"proxier/internal/store"
)

type App struct {
	Cfg       *config.Config
	Store     *store.Store
	Proxy     *proxy.Manager
	Events    *events.Bus
	Stats     *stats.Collector
	Log       *slog.Logger
	Version   string
	StartedAt time.Time

	mu sync.Mutex // serializes mutations + Apply
}

func New(cfg *config.Config, st *store.Store, log *slog.Logger, version string) *App {
	a := &App{Cfg: cfg, Store: st, Log: log, Version: version, StartedAt: time.Now()}
	a.Events = events.NewBus(st, log)
	a.Proxy = proxy.NewManager(log, a.Events.Publish)
	a.Stats = stats.NewCollector(a.Proxy, st, log, a.retention)
	return a
}

// Emit publishes an audit/system event.
func (a *App) Emit(level, typ, msg string, ruleID *int64, clientIP string) {
	a.Events.Publish(model.Event{Level: level, Type: typ, Message: msg, RuleID: ruleID, ClientIP: clientIP})
}

// Reload loads rules and bans from the DB and applies them to the engine.
func (a *App) Reload(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reloadLocked(ctx)
}

func (a *App) reloadLocked(ctx context.Context) error {
	bans, err := a.Store.ListBans(ctx)
	if err != nil {
		return err
	}
	a.Proxy.Bans.Replace(bans)
	rules, err := a.Store.ListRules(ctx)
	if err != nil {
		return err
	}
	a.Proxy.Apply(rules)
	a.Proxy.KickBanned()
	return nil
}

// Run starts background workers; it returns when ctx is done.
func (a *App) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); a.Events.Run(ctx) }()
	go func() { defer wg.Done(); a.Proxy.Run(ctx) }()
	go func() { defer wg.Done(); a.Stats.Run(ctx) }()
	go a.banExpiry(ctx)
	wg.Wait()
}

func (a *App) banExpiry(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := a.Store.PurgeExpiredBans(ctx); err == nil && n > 0 {
				a.Reload(ctx)
			}
		}
	}
}

// ---------- rules ----------

type RuleView struct {
	*model.Rule
	Status   proxy.RuleStatus `json:"status"`
	TotalIn  uint64           `json:"total_in"`
	TotalOut uint64           `json:"total_out"`
}

func (a *App) RuleViews(ctx context.Context) ([]RuleView, error) {
	rules, err := a.Store.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	status := a.Proxy.Status()
	totals, err := a.Store.TrafficTotals(ctx)
	if err != nil {
		return nil, err
	}
	unflushed := a.Stats.Unflushed()
	out := make([]RuleView, 0, len(rules))
	for _, r := range rules {
		st, ok := status[r.ID]
		if !ok {
			st = proxy.RuleStatus{State: proxy.StateDisabled}
		}
		t, u := totals[r.ID], unflushed[r.ID]
		out = append(out, RuleView{Rule: r, Status: st, TotalIn: t[0] + u[0], TotalOut: t[1] + u[1]})
	}
	return out, nil
}

func (a *App) RuleView(ctx context.Context, id int64) (*RuleView, error) {
	views, err := a.RuleViews(ctx)
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].ID == id {
			return &views[i], nil
		}
	}
	return nil, store.ErrNotFound
}

// uiPort returns the TCP port the web UI listens on, or 0 if unknown.
func (a *App) uiPort() (string, int) {
	host, p, err := net.SplitHostPort(a.Cfg.Listen)
	if err != nil {
		return "", 0
	}
	n, _ := strconv.Atoi(p)
	return host, n
}

func ipOverlap(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	pa, e1 := netip.ParseAddr(a)
	pb, e2 := netip.ParseAddr(b)
	if e1 != nil || e2 != nil {
		return a == b
	}
	return pa.Unmap() == pb.Unmap() || pa.IsUnspecified() || pb.IsUnspecified()
}

func protoOverlap(a, b model.Protocol) bool {
	return (a.HasTCP() && b.HasTCP()) || (a.HasUDP() && b.HasUDP())
}

// CheckRule validates r and checks it against other rules, the UI port and
// ports used by other processes. existing are the current rules.
func (a *App) checkRule(r *model.Rule, existing []*model.Rule) error {
	r.Normalize()
	if err := r.Validate(); err != nil {
		return err
	}
	if !r.Enabled {
		return nil
	}
	fe := model.FieldErrors{}
	for _, o := range existing {
		if o.ID == r.ID || !o.Enabled || !protoOverlap(r.Protocol, o.Protocol) || !ipOverlap(r.ListenIP, o.ListenIP) {
			continue
		}
		if r.ListenPortStart <= o.ListenPortEnd && o.ListenPortStart <= r.ListenPortEnd {
			fe["listen_port_start"] = fmt.Sprintf("overlaps with rule %q (%s)", o.Name, portRange(o))
			return fe
		}
	}
	if host, p := a.uiPort(); p > 0 && r.Protocol.HasTCP() && ipOverlap(r.ListenIP, host) &&
		p >= r.ListenPortStart && p <= r.ListenPortEnd {
		fe["listen_port_start"] = fmt.Sprintf("port %d is used by the proxier web UI", p)
		return fe
	}
	for _, proto := range []model.Protocol{model.ProtoTCP, model.ProtoUDP} {
		if (proto == model.ProtoTCP && !r.Protocol.HasTCP()) || (proto == model.ProtoUDP && !r.Protocol.HasUDP()) {
			continue
		}
		for port := r.ListenPortStart; port <= r.ListenPortEnd; port++ {
			if a.Proxy.Holds(proto, port) {
				continue // ours; overlap with enabled rules was checked above
			}
			if err := proxy.CheckPortFree(proto, r.ListenIP, port); err != nil {
				fe["listen_port_start"] = fmt.Sprintf("%s port %d is already in use on this server (%s)", proto, port, shortErr(err))
				return fe
			}
		}
	}
	return nil
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return s
}

func portRange(r *model.Rule) string {
	if r.ListenPortStart == r.ListenPortEnd {
		return fmt.Sprintf("%s %d", r.Protocol, r.ListenPortStart)
	}
	return fmt.Sprintf("%s %d-%d", r.Protocol, r.ListenPortStart, r.ListenPortEnd)
}

// CheckRule is a dry-run validation + conflict check.
func (a *App) CheckRule(ctx context.Context, r *model.Rule) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	existing, err := a.Store.ListRules(ctx)
	if err != nil {
		return err
	}
	return a.checkRule(r, existing)
}

func (a *App) CreateRule(ctx context.Context, r *model.Rule) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	existing, err := a.Store.ListRules(ctx)
	if err != nil {
		return err
	}
	r.ID = 0
	if err := a.checkRule(r, existing); err != nil {
		return err
	}
	if err := a.Store.CreateRule(ctx, r); err != nil {
		return err
	}
	a.Emit("info", "rule_created", fmt.Sprintf("Rule %q created: %s → %s", r.Name, portRange(r), targetStr(r)), &r.ID, "")
	return a.reloadLocked(ctx)
}

func (a *App) UpdateRule(ctx context.Context, r *model.Rule) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	old, err := a.Store.GetRule(ctx, r.ID)
	if err != nil {
		return err
	}
	existing, err := a.Store.ListRules(ctx)
	if err != nil {
		return err
	}
	r.PresetGroup = old.PresetGroup
	if err := a.checkRule(r, existing); err != nil {
		return err
	}
	if err := a.Store.UpdateRule(ctx, r); err != nil {
		return err
	}
	if old.TargetHost != r.TargetHost {
		a.Proxy.Resolver.Forget(old.TargetHost)
	}
	a.Emit("info", "rule_updated", fmt.Sprintf("Rule %q updated: %s → %s", r.Name, portRange(r), targetStr(r)), &r.ID, "")
	return a.reloadLocked(ctx)
}

func (a *App) SetRuleEnabled(ctx context.Context, id int64, enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, err := a.Store.GetRule(ctx, id)
	if err != nil {
		return err
	}
	if enabled && !r.Enabled {
		existing, err := a.Store.ListRules(ctx)
		if err != nil {
			return err
		}
		r.Enabled = true
		if err := a.checkRule(r, existing); err != nil {
			return err
		}
	}
	if err := a.Store.SetRuleEnabled(ctx, id, enabled); err != nil {
		return err
	}
	verb := "disabled"
	if enabled {
		verb = "enabled"
	}
	a.Emit("info", "rule_"+verb, fmt.Sprintf("Rule %q %s", r.Name, verb), &id, "")
	return a.reloadLocked(ctx)
}

func (a *App) DeleteRule(ctx context.Context, id int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, err := a.Store.GetRule(ctx, id)
	if err != nil {
		return err
	}
	if err := a.Store.DeleteRule(ctx, id); err != nil {
		return err
	}
	_ = a.Store.DeleteRuleTraffic(ctx, id)
	a.Emit("info", "rule_deleted", fmt.Sprintf("Rule %q deleted", r.Name), nil, "")
	return a.reloadLocked(ctx)
}

func targetStr(r *model.Rule) string {
	host := r.TargetHost
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if r.ListenPortStart == r.ListenPortEnd {
		return fmt.Sprintf("%s:%d", host, r.TargetPortStart)
	}
	return fmt.Sprintf("%s:%d-%d", host, r.TargetPortStart, r.TargetPortEnd())
}

// ---------- bans ----------

func (a *App) AddBan(ctx context.Context, b *model.Ban) error {
	p, err := model.ParseCIDR(b.CIDR)
	if err != nil {
		return model.FieldErrors{"cidr": "must be an IP address or CIDR (e.g. 1.2.3.4 or 1.2.3.0/24)"}
	}
	if p.Bits() == 0 {
		return model.FieldErrors{"cidr": "refusing to ban every address"}
	}
	b.CIDR = p.String()
	if p.IsSingleIP() {
		b.CIDR = p.Addr().String()
	}
	b.Reason = strings.TrimSpace(b.Reason)
	if len(b.Reason) > 500 {
		return model.FieldErrors{"reason": "at most 500 characters"}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	scope := "all rules"
	if b.RuleID != nil {
		r, err := a.Store.GetRule(ctx, *b.RuleID)
		if err != nil {
			return model.FieldErrors{"rule_id": "unknown rule"}
		}
		scope = fmt.Sprintf("rule %q", r.Name)
	}
	if err := a.Store.CreateBan(ctx, b); err != nil {
		return err
	}
	a.Emit("warn", "ban_added", fmt.Sprintf("Banned %s on %s", b.CIDR, scope), b.RuleID, b.CIDR)
	return a.reloadLocked(ctx)
}

func (a *App) RemoveBan(ctx context.Context, id int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.Store.DeleteBan(ctx, id); err != nil {
		return err
	}
	a.Emit("info", "ban_removed", fmt.Sprintf("Ban #%d removed", id), nil, "")
	return a.reloadLocked(ctx)
}

// BanConnection bans the client IP of a live connection (on its rule, or
// globally) and kicks it.
func (a *App) BanConnection(ctx context.Context, connID string, global bool, reason string) (*model.Ban, error) {
	e := a.Proxy.Registry.Get(connID)
	if e == nil {
		return nil, store.ErrNotFound
	}
	b := &model.Ban{CIDR: e.Client.Addr().Unmap().String(), Reason: reason}
	if !global {
		id := e.RuleID
		b.RuleID = &id
	}
	return b, a.AddBan(ctx, b)
}

// ---------- settings ----------

type Settings struct {
	PublicAddress       string `json:"public_address"`
	RetentionMinuteDays int    `json:"retention_minute_days"`
	RetentionHourDays   int    `json:"retention_hour_days"`
	RetentionEventsDays int    `json:"retention_events_days"`
}

func defaultSettings() Settings {
	return Settings{RetentionMinuteDays: 7, RetentionHourDays: 365, RetentionEventsDays: 30}
}

func (a *App) GetSettings(ctx context.Context) (Settings, error) {
	s := defaultSettings()
	kv, err := a.Store.GetSettings(ctx)
	if err != nil {
		return s, err
	}
	s.PublicAddress = kv["public_address"]
	atoi := func(k string, def int) int {
		if v, err := strconv.Atoi(kv[k]); err == nil && v > 0 {
			return v
		}
		return def
	}
	s.RetentionMinuteDays = atoi("retention_minute_days", s.RetentionMinuteDays)
	s.RetentionHourDays = atoi("retention_hour_days", s.RetentionHourDays)
	s.RetentionEventsDays = atoi("retention_events_days", s.RetentionEventsDays)
	return s, nil
}

func (a *App) SaveSettings(ctx context.Context, s Settings) error {
	fe := model.FieldErrors{}
	s.PublicAddress = strings.TrimSpace(s.PublicAddress)
	if len(s.PublicAddress) > 253 || strings.ContainsAny(s.PublicAddress, " /") {
		fe["public_address"] = "must be a hostname or IP address"
	}
	if s.RetentionMinuteDays < 1 || s.RetentionMinuteDays > 90 {
		fe["retention_minute_days"] = "must be 1-90"
	}
	if s.RetentionHourDays < 1 || s.RetentionHourDays > 3650 {
		fe["retention_hour_days"] = "must be 1-3650"
	}
	if s.RetentionEventsDays < 1 || s.RetentionEventsDays > 3650 {
		fe["retention_events_days"] = "must be 1-3650"
	}
	if len(fe) > 0 {
		return fe
	}
	err := a.Store.SetSettings(ctx, map[string]string{
		"public_address":        s.PublicAddress,
		"retention_minute_days": strconv.Itoa(s.RetentionMinuteDays),
		"retention_hour_days":   strconv.Itoa(s.RetentionHourDays),
		"retention_events_days": strconv.Itoa(s.RetentionEventsDays),
	})
	if err == nil {
		a.Emit("info", "settings_changed", "Settings updated", nil, "")
	}
	return err
}

func (a *App) retention() stats.Retention {
	s, _ := a.GetSettings(context.Background())
	day := 24 * time.Hour
	return stats.Retention{
		Minute: time.Duration(s.RetentionMinuteDays) * day,
		Hour:   time.Duration(s.RetentionHourDays) * day,
		Events: time.Duration(s.RetentionEventsDays) * day,
	}
}

// InterfaceIPs lists non-loopback unicast addresses of this host.
func InterfaceIPs() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		pfx, err := netip.ParsePrefix(a.String())
		if err != nil {
			continue
		}
		ip := pfx.Addr()
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}

// GuessPublicAddress returns the first global IPv4 (or IPv6) interface address.
func GuessPublicAddress() string {
	var v6 string
	for _, s := range InterfaceIPs() {
		ip := netip.MustParseAddr(s)
		if ip.IsPrivate() {
			continue
		}
		if ip.Is4() {
			return s
		}
		if v6 == "" && ip.IsGlobalUnicast() {
			v6 = s
		}
	}
	return v6
}
