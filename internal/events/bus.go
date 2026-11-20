// Package events persists engine and audit events and fans them out to live subscribers.
package events

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"proxier/internal/model"
	"proxier/internal/store"
)

type Bus struct {
	store   *store.Store
	log     *slog.Logger
	ch      chan model.Event
	dropped atomic.Uint64

	mu   sync.Mutex
	subs map[int]chan model.Event
	next int
	done chan struct{}
}

func NewBus(s *store.Store, log *slog.Logger) *Bus {
	return &Bus{store: s, log: log, ch: make(chan model.Event, 8192), subs: map[int]chan model.Event{}, done: make(chan struct{})}
}

// Publish queues an event. It never blocks; events are dropped if the queue is full.
func (b *Bus) Publish(e model.Event) {
	if e.TS == 0 {
		e.TS = time.Now().UnixMilli()
	}
	if e.Level == "" {
		e.Level = "info"
	}
	select {
	case b.ch <- e:
	default:
		b.dropped.Add(1)
	}
}

// Subscribe returns a channel of persisted events and a cancel func.
func (b *Bus) Subscribe() (<-chan model.Event, func()) {
	c := make(chan model.Event, 256)
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = c
	b.mu.Unlock()
	return c, func() {
		b.mu.Lock()
		if _, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
		b.mu.Unlock()
	}
}

// Run persists events until ctx is cancelled, then drains the queue.
func (b *Bus) Run(ctx context.Context) {
	defer close(b.done)
	for {
		select {
		case e := <-b.ch:
			b.handle(e)
		case <-ctx.Done():
			for {
				select {
				case e := <-b.ch:
					b.handle(e)
				default:
					return
				}
			}
		}
	}
}

// Wait blocks until Run has drained and returned.
func (b *Bus) Wait() { <-b.done }

func (b *Bus) handle(e model.Event) {
	if err := b.store.InsertEvent(context.Background(), &e); err != nil {
		b.log.Warn("persist event", "err", err)
	}
	if e.Level == "warn" || e.Level == "error" {
		b.log.Warn(e.Message, "type", e.Type)
	} else {
		b.log.Debug(e.Message, "type", e.Type)
	}
	b.mu.Lock()
	for _, c := range b.subs {
		select {
		case c <- e:
		default: // slow subscriber: drop
		}
	}
	b.mu.Unlock()
}
