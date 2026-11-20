// Package stats samples engine counters every second for live views and
// persists per-minute traffic buckets with hourly rollups.
package stats

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"proxier/internal/proxy"
	"proxier/internal/store"
)

type RuleRate struct {
	RuleID int64   `json:"rule_id"`
	InBps  float64 `json:"in_bps"`
	OutBps float64 `json:"out_bps"`
	Active int64   `json:"active"`
}

type Sample struct {
	TS     int64      `json:"ts"` // unix millis
	InBps  float64    `json:"in_bps"`
	OutBps float64    `json:"out_bps"`
	Active int64      `json:"active"`
	Rules  []RuleRate `json:"rules"`
}

// Retention controls how long history is kept.
type Retention struct {
	Minute time.Duration
	Hour   time.Duration
	Events time.Duration
}

type Collector struct {
	m         *proxy.Manager
	store     *store.Store
	log       *slog.Logger
	retention func() Retention

	mu       sync.Mutex
	prev     map[int64]proxy.CounterSnapshot
	prevAt   time.Time
	base     map[int64]proxy.CounterSnapshot // counters at last minute flush
	peak     map[int64]int64
	history  []Sample // ring of the last historyLen samples
	onSample []func(Sample)
}

const historyLen = 300

func NewCollector(m *proxy.Manager, s *store.Store, log *slog.Logger, retention func() Retention) *Collector {
	return &Collector{m: m, store: s, log: log, retention: retention,
		prev: map[int64]proxy.CounterSnapshot{}, base: map[int64]proxy.CounterSnapshot{}, peak: map[int64]int64{}}
}

// OnSample registers a callback invoked after every 1s sample.
func (c *Collector) OnSample(fn func(Sample)) {
	c.mu.Lock()
	c.onSample = append(c.onSample, fn)
	c.mu.Unlock()
}

// History returns recent 1s samples (oldest first).
func (c *Collector) History() []Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Sample(nil), c.history...)
}

// Latest returns the most recent sample.
func (c *Collector) Latest() Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.history) == 0 {
		return Sample{TS: time.Now().UnixMilli(), Rules: []RuleRate{}}
	}
	return c.history[len(c.history)-1]
}

// Unflushed returns bytes counted since the last minute flush, per rule.
func (c *Collector) Unflushed() map[int64][2]uint64 {
	cur := c.m.Counters()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[int64][2]uint64{}
	for id, s := range cur {
		b := c.base[id]
		out[id] = [2]uint64{sub(s.BytesIn, b.BytesIn), sub(s.BytesOut, b.BytesOut)}
	}
	return out
}

func sub(a, b uint64) uint64 {
	if a < b {
		return a // counter reset
	}
	return a - b
}

func (c *Collector) Run(ctx context.Context) {
	c.mu.Lock()
	c.prev = c.m.Counters()
	c.base = c.prev
	c.prevAt = time.Now()
	c.mu.Unlock()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	nextMinute := time.Now().Truncate(time.Minute).Add(time.Minute)
	nextRollup := time.Now().Add(time.Minute)
	for {
		select {
		case <-ctx.Done():
			c.flush(time.Now())
			return
		case now := <-tick.C:
			c.sample(now)
			if !now.Before(nextMinute) {
				c.flush(nextMinute.Add(-time.Minute))
				nextMinute = now.Truncate(time.Minute).Add(time.Minute)
			}
			if !now.Before(nextRollup) {
				c.maintain(ctx)
				nextRollup = now.Add(time.Hour)
			}
		}
	}
}

func (c *Collector) sample(now time.Time) {
	cur := c.m.Counters()
	c.mu.Lock()
	dt := now.Sub(c.prevAt).Seconds()
	if dt <= 0 {
		dt = 1
	}
	s := Sample{TS: now.UnixMilli(), Rules: make([]RuleRate, 0, len(cur))}
	for id, v := range cur {
		p := c.prev[id]
		rr := RuleRate{
			RuleID: id,
			InBps:  float64(sub(v.BytesIn, p.BytesIn)) / dt,
			OutBps: float64(sub(v.BytesOut, p.BytesOut)) / dt,
			Active: v.Active,
		}
		s.InBps += rr.InBps
		s.OutBps += rr.OutBps
		s.Active += rr.Active
		s.Rules = append(s.Rules, rr)
		if v.Active > c.peak[id] {
			c.peak[id] = v.Active
		}
	}
	c.prev, c.prevAt = cur, now
	c.history = append(c.history, s)
	if len(c.history) > historyLen {
		c.history = c.history[len(c.history)-historyLen:]
	}
	subs := append([]func(Sample){}, c.onSample...)
	c.mu.Unlock()
	for _, fn := range subs {
		fn(s)
	}
}

// flush writes the delta since the previous flush as the bucket starting at ts.
func (c *Collector) flush(bucket time.Time) {
	cur := c.m.Counters()
	c.mu.Lock()
	var rows []store.TrafficRow
	for id, v := range cur {
		b := c.base[id]
		row := store.TrafficRow{
			RuleID:      id,
			TS:          bucket.Truncate(time.Minute).Unix(),
			BytesIn:     sub(v.BytesIn, b.BytesIn),
			BytesOut:    sub(v.BytesOut, b.BytesOut),
			ConnsOpened: sub(v.ConnsOpened, b.ConnsOpened),
			PeakConns:   max(c.peak[id], v.Active),
		}
		if row.BytesIn == 0 && row.BytesOut == 0 && row.ConnsOpened == 0 && row.PeakConns == 0 {
			continue
		}
		rows = append(rows, row)
	}
	c.base = cur
	c.peak = map[int64]int64{}
	c.mu.Unlock()
	if err := c.store.InsertTrafficMinutes(context.Background(), rows); err != nil {
		c.log.Warn("flush traffic", "err", err)
	}
}

func (c *Collector) maintain(ctx context.Context) {
	r := c.retention()
	if err := c.store.RollupTraffic(ctx, r.Minute, r.Hour); err != nil {
		c.log.Warn("traffic rollup", "err", err)
	}
	if err := c.store.PurgeEventsBefore(ctx, time.Now().Add(-r.Events)); err != nil {
		c.log.Warn("purge events", "err", err)
	}
	if err := c.store.PurgeExpiredSessions(ctx); err != nil {
		c.log.Warn("purge sessions", "err", err)
	}
}
