package proxy

import (
	"net/netip"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"proxier/internal/model"
)

// Entry is a live TCP connection or UDP session.
type Entry struct {
	ID         string
	RuleID     int64
	Proto      model.Protocol // tcp or udp
	Client     netip.AddrPort
	ListenPort int
	Target     string
	Started    time.Time

	BytesIn    atomic.Uint64 // client -> target
	BytesOut   atomic.Uint64 // target -> client
	LastActive atomic.Int64  // unix nanos

	closeOnce sync.Once
	closeFn   func()
}

func (e *Entry) touch() { e.LastActive.Store(time.Now().UnixNano()) }

// Close terminates the connection/session (idempotent).
func (e *Entry) Close() {
	e.closeOnce.Do(func() {
		if e.closeFn != nil {
			e.closeFn()
		}
	})
}

// EntryInfo is a JSON-friendly snapshot of an Entry.
type EntryInfo struct {
	ID         string `json:"id"`
	RuleID     int64  `json:"rule_id"`
	Proto      string `json:"proto"`
	ClientIP   string `json:"client_ip"`
	ClientPort uint16 `json:"client_port"`
	ListenPort int    `json:"listen_port"`
	Target     string `json:"target"`
	Started    int64  `json:"started"` // unix millis
	LastActive int64  `json:"last_active"`
	BytesIn    uint64 `json:"bytes_in"`
	BytesOut   uint64 `json:"bytes_out"`
}

func (e *Entry) Info() EntryInfo {
	return EntryInfo{
		ID:         e.ID,
		RuleID:     e.RuleID,
		Proto:      string(e.Proto),
		ClientIP:   e.Client.Addr().Unmap().String(),
		ClientPort: e.Client.Port(),
		ListenPort: e.ListenPort,
		Target:     e.Target,
		Started:    e.Started.UnixMilli(),
		LastActive: e.LastActive.Load() / int64(time.Millisecond),
		BytesIn:    e.BytesIn.Load(),
		BytesOut:   e.BytesOut.Load(),
	}
}

// Registry tracks all live entries.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*Entry
	seq     atomic.Uint64
}

func NewRegistry() *Registry { return &Registry{entries: map[string]*Entry{}} }

func (r *Registry) add(e *Entry) {
	e.ID = string(e.Proto[0]) + strconv.FormatUint(r.seq.Add(1), 10)
	e.Started = time.Now()
	e.touch()
	r.mu.Lock()
	r.entries[e.ID] = e
	r.mu.Unlock()
}

func (r *Registry) remove(id string) {
	r.mu.Lock()
	delete(r.entries, id)
	r.mu.Unlock()
}

func (r *Registry) Get(id string) *Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries[id]
}

// List returns snapshots, optionally filtered by rule (ruleID 0 = all), newest first.
func (r *Registry) List(ruleID int64) []EntryInfo {
	r.mu.RLock()
	out := make([]EntryInfo, 0, len(r.entries))
	for _, e := range r.entries {
		if ruleID == 0 || e.RuleID == ruleID {
			out = append(out, e.Info())
		}
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Started > out[j].Started })
	return out
}

// CloseWhere closes every entry matching fn and returns how many were closed.
func (r *Registry) CloseWhere(fn func(*Entry) bool) int {
	r.mu.RLock()
	var victims []*Entry
	for _, e := range r.entries {
		if fn(e) {
			victims = append(victims, e)
		}
	}
	r.mu.RUnlock()
	for _, e := range victims {
		e.Close()
	}
	return len(victims)
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
