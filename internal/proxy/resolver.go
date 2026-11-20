package proxy

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"
)

// Resolver caches hostname lookups. Stale entries are served immediately while
// a background refresh runs, so a slow or failing DNS server never stalls
// traffic for a target that resolved before.
type Resolver struct {
	TTL      time.Duration
	Timeout  time.Duration
	lookupFn func(ctx context.Context, host string) ([]netip.Addr, error)

	mu    sync.Mutex
	cache map[string]*resolveEntry
}

type resolveEntry struct {
	addr       netip.Addr
	fetched    time.Time
	refreshing bool
}

func NewResolver() *Resolver {
	return &Resolver{
		TTL:     60 * time.Second,
		Timeout: 5 * time.Second,
		lookupFn: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		cache: map[string]*resolveEntry{},
	}
}

// Resolve returns an address for host (IP literals are returned as-is).
func (r *Resolver) Resolve(ctx context.Context, host string) (netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return a.Unmap(), nil
	}
	r.mu.Lock()
	e, ok := r.cache[host]
	if ok {
		if time.Since(e.fetched) > r.TTL && !e.refreshing {
			e.refreshing = true
			go r.refresh(host)
		}
		a := e.addr
		r.mu.Unlock()
		return a, nil
	}
	r.mu.Unlock()
	return r.lookup(ctx, host)
}

func (r *Resolver) refresh(host string) {
	ctx, cancel := context.WithTimeout(context.Background(), r.Timeout)
	defer cancel()
	if _, err := r.lookup(ctx, host); err != nil {
		r.mu.Lock()
		if e, ok := r.cache[host]; ok {
			e.refreshing = false
		}
		r.mu.Unlock()
	}
}

func (r *Resolver) lookup(ctx context.Context, host string) (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	addrs, err := r.lookupFn(ctx, host)
	if err != nil {
		return netip.Addr{}, err
	}
	if len(addrs) == 0 {
		return netip.Addr{}, fmt.Errorf("no addresses for %s", host)
	}
	// Prefer IPv4: cheap VPS IPv6 routing is often less reliable.
	best := addrs[0].Unmap()
	for _, a := range addrs {
		if a.Unmap().Is4() {
			best = a.Unmap()
			break
		}
	}
	r.mu.Lock()
	r.cache[host] = &resolveEntry{addr: best, fetched: time.Now()}
	r.mu.Unlock()
	return best, nil
}

// Forget drops a cached host (used when a rule changes).
func (r *Resolver) Forget(host string) {
	r.mu.Lock()
	delete(r.cache, host)
	r.mu.Unlock()
}
