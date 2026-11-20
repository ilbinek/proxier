package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"proxier/internal/model"
)

var bufPool = sync.Pool{New: func() any { b := make([]byte, 32*1024); return &b }}

type tcpListener struct {
	m      *Manager
	key    lkey
	rule   atomic.Pointer[model.Rule]
	ln     net.Listener
	closed atomic.Bool
}

func (m *Manager) startTCP(k lkey, r *model.Rule) (*tcpListener, error) {
	lc := net.ListenConfig{KeepAlive: 30 * time.Second}
	ln, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(k.ip, strconv.Itoa(k.port)))
	if err != nil {
		return nil, err
	}
	l := &tcpListener{m: m, key: k, ln: ln}
	l.rule.Store(r)
	go l.serve()
	return l, nil
}

func (l *tcpListener) ruleID() int64         { return l.rule.Load().ID }
func (l *tcpListener) setRule(r *model.Rule) { l.rule.Store(r) }

func (l *tcpListener) close() {
	l.closed.Store(true)
	l.ln.Close()
}

func (l *tcpListener) serve() {
	var backoff time.Duration
	for {
		c, err := l.ln.Accept()
		if err != nil {
			if l.closed.Load() || errors.Is(err, net.ErrClosed) {
				return
			}
			// Temporary error (e.g. too many open files): back off a little.
			if backoff == 0 {
				backoff = 5 * time.Millisecond
			} else if backoff < time.Second {
				backoff *= 2
			}
			time.Sleep(backoff)
			continue
		}
		backoff = 0
		go l.handle(c)
	}
}

func (l *tcpListener) handle(c net.Conn) {
	r := l.rule.Load()
	m := l.m
	client := addrPortOf(c.RemoteAddr())
	if m.Bans.Banned(r.ID, client.Addr()) {
		c.Close()
		m.emitReject(r, model.ProtoTCP, client)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), m.DialTimeout)
	targetStr := net.JoinHostPort(r.TargetHost, strconv.Itoa(r.TargetPortFor(l.key.port)))
	addr, err := m.Resolver.Resolve(ctx, r.TargetHost)
	if err != nil {
		cancel()
		c.Close()
		m.emitDialError(r, model.ProtoTCP, client, targetStr, err)
		return
	}
	target := netip.AddrPortFrom(addr, uint16(r.TargetPortFor(l.key.port)))
	var d net.Dialer
	up, err := d.DialContext(ctx, "tcp", target.String())
	cancel()
	if err != nil {
		c.Close()
		m.emitDialError(r, model.ProtoTCP, client, target.String(), err)
		return
	}
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	if tc, ok := up.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}

	ctr := m.countersFor(r.ID)
	e := &Entry{RuleID: r.ID, Proto: model.ProtoTCP, Client: client, ListenPort: l.key.port, Target: target.String()}
	e.closeFn = func() { c.Close(); up.Close() }
	m.Registry.add(e)
	ctr.ConnsOpened.Add(1)
	ctr.Active.Add(1)
	m.emitConn("conn_open", e)

	pipeTCP(c, up, e, ctr)

	e.Close()
	m.Registry.remove(e.ID)
	ctr.Active.Add(-1)
	m.emitConn("conn_close", e)
}

// pipeTCP copies both directions until both are done. A clean EOF on one side
// half-closes the other; an error tears down the whole connection.
func pipeTCP(client, up net.Conn, e *Entry, ctr *Counters) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		err := copyCount(up, client, e, &e.BytesIn, &ctr.BytesIn)
		finish(up, err, e)
	}()
	go func() {
		defer wg.Done()
		err := copyCount(client, up, e, &e.BytesOut, &ctr.BytesOut)
		finish(client, err, e)
	}()
	wg.Wait()
}

func finish(dst net.Conn, err error, e *Entry) {
	if err != nil {
		e.Close()
		return
	}
	if cw, ok := dst.(interface{ CloseWrite() error }); ok {
		if cw.CloseWrite() == nil {
			return
		}
	}
	e.Close()
}

// copyCount copies src to dst, adding transferred bytes to the counters.
// It returns nil on clean EOF.
func copyCount(dst io.Writer, src io.Reader, e *Entry, a, b *atomic.Uint64) error {
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	buf := *bp
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			w, werr := dst.Write(buf[:n])
			if w > 0 {
				a.Add(uint64(w))
				b.Add(uint64(w))
				e.touch()
			}
			if werr != nil {
				return werr
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return rerr
		}
	}
}

func addrPortOf(a net.Addr) netip.AddrPort {
	switch v := a.(type) {
	case *net.TCPAddr:
		return v.AddrPort()
	case *net.UDPAddr:
		return v.AddrPort()
	}
	ap, _ := netip.ParseAddrPort(a.String())
	return ap
}
