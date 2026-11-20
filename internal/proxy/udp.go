package proxy

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"proxier/internal/model"
)

const udpBufSize = 64 * 1024

type udpListener struct {
	m      *Manager
	key    lkey
	rule   atomic.Pointer[model.Rule]
	conn   *net.UDPConn
	closed atomic.Bool
	stop   chan struct{}

	mu       sync.RWMutex
	sessions map[netip.AddrPort]*udpSession
}

type udpSession struct {
	entry *Entry
	up    *net.UDPConn
	ctr   *Counters
}

func (m *Manager) startUDP(k lkey, r *model.Rule) (*udpListener, error) {
	pc, err := net.ListenPacket("udp", net.JoinHostPort(k.ip, strconv.Itoa(k.port)))
	if err != nil {
		return nil, err
	}
	conn := pc.(*net.UDPConn)
	_ = conn.SetReadBuffer(4 << 20)
	_ = conn.SetWriteBuffer(4 << 20)
	l := &udpListener{m: m, key: k, conn: conn, stop: make(chan struct{}), sessions: map[netip.AddrPort]*udpSession{}}
	l.rule.Store(r)
	go l.serve()
	go l.reap()
	return l, nil
}

func (l *udpListener) ruleID() int64         { return l.rule.Load().ID }
func (l *udpListener) setRule(r *model.Rule) { l.rule.Store(r) }

func (l *udpListener) close() {
	if l.closed.Swap(true) {
		return
	}
	close(l.stop)
	l.conn.Close()
	l.mu.RLock()
	sessions := make([]*udpSession, 0, len(l.sessions))
	for _, s := range l.sessions {
		sessions = append(sessions, s)
	}
	l.mu.RUnlock()
	for _, s := range sessions {
		s.entry.Close()
	}
}

func (l *udpListener) serve() {
	buf := make([]byte, udpBufSize)
	for {
		n, src, err := l.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if l.closed.Load() || errors.Is(err, net.ErrClosed) {
				return
			}
			continue // e.g. ICMP-induced errors on some platforms
		}
		l.mu.RLock()
		s := l.sessions[src]
		l.mu.RUnlock()
		if s == nil {
			if s = l.newSession(src); s == nil {
				continue
			}
		}
		if w, err := s.up.Write(buf[:n]); err == nil {
			s.entry.BytesIn.Add(uint64(w))
			s.ctr.BytesIn.Add(uint64(w))
			s.entry.touch()
		}
	}
}

func (l *udpListener) newSession(src netip.AddrPort) *udpSession {
	r := l.rule.Load()
	m := l.m
	if m.Bans.Banned(r.ID, src.Addr()) {
		m.emitReject(r, model.ProtoUDP, src)
		return nil
	}
	l.mu.RLock()
	full := len(l.sessions) >= m.MaxUDPSessions
	l.mu.RUnlock()
	if full {
		return nil
	}

	targetPort := r.TargetPortFor(l.key.port)
	ctx, cancel := context.WithTimeout(context.Background(), m.DialTimeout)
	addr, err := m.Resolver.Resolve(ctx, r.TargetHost)
	cancel()
	if err != nil {
		m.emitDialError(r, model.ProtoUDP, src, net.JoinHostPort(r.TargetHost, strconv.Itoa(targetPort)), err)
		return nil
	}
	target := netip.AddrPortFrom(addr, uint16(targetPort))
	up, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(target))
	if err != nil {
		m.emitDialError(r, model.ProtoUDP, src, target.String(), err)
		return nil
	}
	_ = up.SetReadBuffer(1 << 20)

	ctr := m.countersFor(r.ID)
	e := &Entry{RuleID: r.ID, Proto: model.ProtoUDP, Client: src, ListenPort: l.key.port, Target: target.String()}
	s := &udpSession{entry: e, up: up, ctr: ctr}
	e.closeFn = func() {
		up.Close()
		l.mu.Lock()
		if l.sessions[src] == s {
			delete(l.sessions, src)
		}
		l.mu.Unlock()
	}
	l.mu.Lock()
	l.sessions[src] = s
	l.mu.Unlock()
	m.Registry.add(e)
	ctr.ConnsOpened.Add(1)
	ctr.Active.Add(1)
	m.emitConn("conn_open", e)

	go l.replyLoop(s, src)
	return s
}

// replyLoop relays packets from the target back to the client until the
// upstream socket is closed (idle reap, kick, rule change or shutdown).
func (l *udpListener) replyLoop(s *udpSession, src netip.AddrPort) {
	buf := make([]byte, udpBufSize)
	for {
		n, err := s.up.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}
			// ECONNREFUSED from an ICMP port-unreachable: target is down for
			// now. Keep the session; the idle timeout cleans it up.
			continue
		}
		if w, err := l.conn.WriteToUDPAddrPort(buf[:n], src); err == nil {
			s.entry.BytesOut.Add(uint64(w))
			s.ctr.BytesOut.Add(uint64(w))
			s.entry.touch()
		} else if l.closed.Load() {
			break
		}
	}
	s.entry.Close()
	l.m.Registry.remove(s.entry.ID)
	s.ctr.Active.Add(-1)
	l.m.emitConn("conn_close", s.entry)
}

func (l *udpListener) reap() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
		}
		timeout := time.Duration(l.rule.Load().UDPIdleTimeoutS) * time.Second
		cutoff := time.Now().Add(-timeout).UnixNano()
		var idle []*udpSession
		l.mu.RLock()
		for _, s := range l.sessions {
			if s.entry.LastActive.Load() < cutoff {
				idle = append(idle, s)
			}
		}
		l.mu.RUnlock()
		for _, s := range idle {
			s.entry.Close()
		}
	}
}
