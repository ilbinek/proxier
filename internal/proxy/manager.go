// Package proxy is the userspace TCP/UDP forwarding engine.
package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"proxier/internal/model"
)

// Counters are per-rule cumulative traffic counters (since process start).
type Counters struct {
	BytesIn     atomic.Uint64 // client -> target
	BytesOut    atomic.Uint64 // target -> client
	ConnsOpened atomic.Uint64
	Active      atomic.Int64
}

type CounterSnapshot struct {
	BytesIn     uint64
	BytesOut    uint64
	ConnsOpened uint64
	Active      int64
}

type RuleState string

const (
	StateRunning  RuleState = "running"
	StateError    RuleState = "error"
	StateDisabled RuleState = "disabled"
)

type RuleStatus struct {
	State     RuleState `json:"state"`
	Error     string    `json:"error,omitempty"`
	Listeners int       `json:"listeners"` // listeners successfully bound
	Expected  int       `json:"expected"`  // listeners wanted
}

type lkey struct {
	proto model.Protocol // tcp or udp
	ip    string
	port  int
}

func (k lkey) String() string {
	ip := k.ip
	if ip == "" {
		ip = "*"
	} else if strings.Contains(ip, ":") {
		ip = "[" + ip + "]"
	}
	return fmt.Sprintf("%s/%s:%d", k.proto, ip, k.port)
}

type listener interface {
	ruleID() int64
	setRule(*model.Rule)
	close()
}

// Manager reconciles the desired rule set with running listeners.
type Manager struct {
	Registry *Registry
	Bans     *BanList
	Resolver *Resolver

	DialTimeout    time.Duration
	MaxUDPSessions int // per listener

	log  *slog.Logger
	emit func(model.Event)

	mu        sync.Mutex
	rules     []*model.Rule
	listeners map[lkey]listener
	failed    map[lkey]string // last bind error per key
	status    map[int64]RuleStatus

	ctrMu    sync.RWMutex
	counters map[int64]*Counters

	limMu   sync.Mutex
	limiter map[string]time.Time
}

// NewManager creates a manager. emit receives engine events (may be nil).
func NewManager(log *slog.Logger, emit func(model.Event)) *Manager {
	if emit == nil {
		emit = func(model.Event) {}
	}
	return &Manager{
		Registry:       NewRegistry(),
		Bans:           NewBanList(),
		Resolver:       NewResolver(),
		DialTimeout:    10 * time.Second,
		MaxUDPSessions: 4096,
		log:            log,
		emit:           emit,
		listeners:      map[lkey]listener{},
		failed:         map[lkey]string{},
		status:         map[int64]RuleStatus{},
		counters:       map[int64]*Counters{},
		limiter:        map[string]time.Time{},
	}
}

// Run retries failed binds periodically until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			retry := len(m.failed) > 0
			rules := m.rules
			m.mu.Unlock()
			if retry {
				m.Apply(rules)
			}
		}
	}
}

// Apply makes the running listeners match rules. It is safe to call repeatedly.
func (m *Manager) Apply(rules []*model.Rule) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prevByID := map[int64]*model.Rule{}
	for _, r := range m.rules {
		prevByID[r.ID] = r
	}
	m.rules = rules

	desired := map[lkey]*model.Rule{}
	enabled := map[int64]bool{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		enabled[r.ID] = true
		for _, proto := range []model.Protocol{model.ProtoTCP, model.ProtoUDP} {
			if (proto == model.ProtoTCP && !r.Protocol.HasTCP()) || (proto == model.ProtoUDP && !r.Protocol.HasUDP()) {
				continue
			}
			for p := r.ListenPortStart; p <= r.ListenPortEnd; p++ {
				desired[lkey{proto, r.ListenIP, p}] = r
			}
		}
	}

	// Stop listeners that are no longer wanted or now belong to another rule.
	for k, l := range m.listeners {
		if r, ok := desired[k]; !ok || r.ID != l.ruleID() {
			l.close()
			delete(m.listeners, k)
		}
	}
	for k := range m.failed {
		if _, ok := desired[k]; !ok {
			delete(m.failed, k)
		}
	}

	// Close live connections of rules that were disabled/deleted or retargeted.
	m.Registry.CloseWhere(func(e *Entry) bool {
		if !enabled[e.RuleID] {
			return true
		}
		prev := prevByID[e.RuleID]
		cur := findRule(rules, e.RuleID)
		return prev != nil && cur != nil && (prev.TargetHost != cur.TargetHost || prev.TargetPortStart != cur.TargetPortStart)
	})

	// Start missing listeners, update existing ones.
	keys := make([]lkey, 0, len(desired))
	for k := range desired {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].port < keys[j].port })
	for _, k := range keys {
		r := desired[k]
		if l, ok := m.listeners[k]; ok {
			l.setRule(r)
			continue
		}
		var l listener
		var err error
		if k.proto == model.ProtoTCP {
			l, err = m.startTCP(k, r)
		} else {
			l, err = m.startUDP(k, r)
		}
		if err != nil {
			m.failed[k] = err.Error()
			continue
		}
		delete(m.failed, k)
		m.listeners[k] = l
	}

	// Compute status and emit changes.
	newStatus := map[int64]RuleStatus{}
	for _, r := range rules {
		st := RuleStatus{State: StateDisabled}
		if r.Enabled {
			st.State = StateRunning
			var errs []string
			for k, rr := range desired {
				if rr.ID != r.ID {
					continue
				}
				st.Expected++
				if msg, bad := m.failed[k]; bad {
					errs = append(errs, k.String()+": "+msg)
				} else {
					st.Listeners++
				}
			}
			if len(errs) > 0 {
				sort.Strings(errs)
				st.State = StateError
				st.Error = errs[0]
				if len(errs) > 1 {
					st.Error += fmt.Sprintf(" (and %d more)", len(errs)-1)
				}
			}
		}
		old, had := m.status[r.ID]
		if !had || old.State != st.State || old.Error != st.Error {
			m.emitStatus(r, st)
		}
		newStatus[r.ID] = st
	}
	m.status = newStatus

	// Make sure counters exist for current rules; drop counters of deleted ones.
	m.ctrMu.Lock()
	keep := map[int64]bool{}
	for _, r := range rules {
		keep[r.ID] = true
		if _, ok := m.counters[r.ID]; !ok {
			m.counters[r.ID] = &Counters{}
		}
	}
	for id := range m.counters {
		if !keep[id] {
			delete(m.counters, id)
		}
	}
	m.ctrMu.Unlock()
}

func findRule(rules []*model.Rule, id int64) *model.Rule {
	for _, r := range rules {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (m *Manager) emitStatus(r *model.Rule, st RuleStatus) {
	id := r.ID
	ev := model.Event{RuleID: &id, Meta: map[string]any{"state": string(st.State)}}
	switch st.State {
	case StateRunning:
		ev.Level, ev.Type, ev.Message = "info", "rule_started", fmt.Sprintf("Rule %q is forwarding (%d listener(s))", r.Name, st.Listeners)
	case StateError:
		ev.Level, ev.Type, ev.Message = "error", "rule_error", fmt.Sprintf("Rule %q failed to bind: %s", r.Name, st.Error)
	case StateDisabled:
		ev.Level, ev.Type, ev.Message = "info", "rule_stopped", fmt.Sprintf("Rule %q stopped", r.Name)
	}
	m.log.Info("rule status", "rule", r.Name, "state", st.State, "error", st.Error)
	m.emit(ev)
}

// Status returns the status of every rule known to the manager.
func (m *Manager) Status() map[int64]RuleStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.status)
}

// Holds reports whether the manager currently has a listener on proto/port (any IP).
func (m *Manager) Holds(proto model.Protocol, port int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.listeners {
		if k.proto == proto && k.port == port {
			return true
		}
	}
	return false
}

func (m *Manager) countersFor(ruleID int64) *Counters {
	m.ctrMu.RLock()
	c := m.counters[ruleID]
	m.ctrMu.RUnlock()
	if c != nil {
		return c
	}
	m.ctrMu.Lock()
	defer m.ctrMu.Unlock()
	if c = m.counters[ruleID]; c == nil {
		c = &Counters{}
		m.counters[ruleID] = c
	}
	return c
}

// Counters returns a snapshot of all per-rule counters.
func (m *Manager) Counters() map[int64]CounterSnapshot {
	m.ctrMu.RLock()
	defer m.ctrMu.RUnlock()
	out := make(map[int64]CounterSnapshot, len(m.counters))
	for id, c := range m.counters {
		out[id] = CounterSnapshot{
			BytesIn:     c.BytesIn.Load(),
			BytesOut:    c.BytesOut.Load(),
			ConnsOpened: c.ConnsOpened.Load(),
			Active:      c.Active.Load(),
		}
	}
	return out
}

// Kick closes one live connection/session.
func (m *Manager) Kick(id string) bool {
	e := m.Registry.Get(id)
	if e == nil {
		return false
	}
	e.Close()
	return true
}

// KickBanned closes every live entry that is now banned.
func (m *Manager) KickBanned() int {
	return m.Registry.CloseWhere(func(e *Entry) bool { return m.Bans.Banned(e.RuleID, e.Client.Addr()) })
}

// Shutdown closes all listeners and live connections.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	for k, l := range m.listeners {
		l.close()
		delete(m.listeners, k)
	}
	m.mu.Unlock()
	m.Registry.CloseWhere(func(*Entry) bool { return true })
}

// allow is a tiny per-key rate limiter for noisy events.
func (m *Manager) allow(key string, every time.Duration) bool {
	m.limMu.Lock()
	defer m.limMu.Unlock()
	now := time.Now()
	if t, ok := m.limiter[key]; ok && now.Sub(t) < every {
		return false
	}
	if len(m.limiter) > 10000 {
		m.limiter = map[string]time.Time{}
	}
	m.limiter[key] = now
	return true
}

func (m *Manager) emitConn(typ string, e *Entry) {
	id := e.RuleID
	ev := model.Event{
		Level:    "info",
		Type:     typ,
		RuleID:   &id,
		ClientIP: e.Client.Addr().Unmap().String(),
		Meta: map[string]any{
			"conn_id": e.ID, "proto": string(e.Proto), "listen_port": e.ListenPort, "target": e.Target,
		},
	}
	if typ == "conn_open" {
		ev.Message = fmt.Sprintf("%s %s opened → %s", strings.ToUpper(string(e.Proto)), e.Client, e.Target)
	} else {
		in, out := e.BytesIn.Load(), e.BytesOut.Load()
		dur := time.Since(e.Started).Round(time.Second)
		ev.Meta["bytes_in"], ev.Meta["bytes_out"], ev.Meta["duration_ms"] = in, out, time.Since(e.Started).Milliseconds()
		ev.Message = fmt.Sprintf("%s %s closed after %s (↑%s ↓%s)", strings.ToUpper(string(e.Proto)), e.Client, dur, humanBytes(in), humanBytes(out))
	}
	m.emit(ev)
}

func (m *Manager) emitReject(r *model.Rule, proto model.Protocol, client netip.AddrPort) {
	ip := client.Addr().Unmap()
	if !m.allow("ban:"+ip.String(), time.Minute) {
		return
	}
	id := r.ID
	m.emit(model.Event{Level: "warn", Type: "conn_rejected", RuleID: &id, ClientIP: ip.String(),
		Message: fmt.Sprintf("Rejected banned client %s (%s)", ip, proto)})
}

func (m *Manager) emitDialError(r *model.Rule, proto model.Protocol, client netip.AddrPort, target string, err error) {
	if !m.allow(fmt.Sprintf("dial:%d:%s", r.ID, proto), 30*time.Second) {
		return
	}
	id := r.ID
	m.emit(model.Event{Level: "warn", Type: "dial_error", RuleID: &id, ClientIP: client.Addr().Unmap().String(),
		Message: fmt.Sprintf("Could not reach target %s for %s: %v", target, proto, err)})
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
