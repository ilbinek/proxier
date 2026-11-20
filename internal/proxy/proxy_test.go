package proxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"proxier/internal/model"
)

func testManager(t *testing.T) (*Manager, *eventLog) {
	t.Helper()
	ev := &eventLog{}
	m := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)), ev.add)
	m.DialTimeout = 2 * time.Second
	t.Cleanup(m.Shutdown)
	return m, ev
}

type eventLog struct {
	mu sync.Mutex
	ev []model.Event
}

func (l *eventLog) add(e model.Event) { l.mu.Lock(); l.ev = append(l.ev, e); l.mu.Unlock() }
func (l *eventLog) count(typ string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.ev {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// freePort finds a port free for both TCP and UDP on 127.0.0.1.
func freePort(t *testing.T) int {
	t.Helper()
	for range 50 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		if pc, err := net.ListenPacket("udp", "127.0.0.1:"+strconv.Itoa(p)); err == nil {
			pc.Close()
			return p
		}
	}
	t.Fatal("no free port")
	return 0
}

// freeRange finds n consecutive ports free for TCP.
func freeRange(t *testing.T, n int) int {
	t.Helper()
outer:
	for range 50 {
		start := freePort(t)
		for p := start; p < start+n; p++ {
			if CheckPortFree(model.ProtoTCP, "127.0.0.1", p) != nil {
				continue outer
			}
		}
		return start
	}
	t.Fatal("no free range")
	return 0
}

func tcpEcho(t *testing.T) (port int, prefix []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	prefix = []byte("p" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port) + ":")
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.Write(prefix)
				io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, prefix
}

func udpEcho(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, a, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(append([]byte("echo:"), buf[:n]...), a)
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func rule(id int64, proto model.Protocol, listen, target int) *model.Rule {
	r := &model.Rule{ID: id, Name: "r" + strconv.FormatInt(id, 10), Enabled: true, Protocol: proto,
		ListenIP: "127.0.0.1", ListenPortStart: listen, TargetHost: "127.0.0.1", TargetPortStart: target}
	r.Normalize()
	return r
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for: " + msg)
}

func dialTCP(t *testing.T, port int) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(3 * time.Second))
	return c
}

func TestTCPForwardAndCounters(t *testing.T) {
	m, ev := testManager(t)
	target, prefix := tcpEcho(t)
	listen := freePort(t)
	m.Apply([]*model.Rule{rule(1, model.ProtoTCP, listen, target)})
	if st := m.Status()[1]; st.State != StateRunning {
		t.Fatalf("status = %+v", st)
	}

	c := dialTCP(t, listen)
	msg := []byte("hello through the proxy")
	c.Write(msg)
	got := make([]byte, len(prefix)+len(msg))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, append(prefix, msg...)) {
		t.Fatalf("got %q", got)
	}
	eventually(t, func() bool { return m.Registry.Count() == 1 }, "registry entry")
	c.Close()
	eventually(t, func() bool { return m.Registry.Count() == 0 }, "registry cleared")

	cs := m.Counters()[1]
	if cs.BytesIn != uint64(len(msg)) || cs.BytesOut != uint64(len(prefix)+len(msg)) || cs.ConnsOpened != 1 || cs.Active != 0 {
		t.Fatalf("counters = %+v", cs)
	}
	if ev.count("conn_open") != 1 {
		t.Fatal("missing conn_open event")
	}
	eventually(t, func() bool { return ev.count("conn_close") == 1 }, "conn_close event")
}

func TestTCPHalfClose(t *testing.T) {
	m, _ := testManager(t)
	// Target reads everything until EOF, then replies with the byte count.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		n, _ := io.Copy(io.Discard, c)
		c.Write([]byte(strconv.FormatInt(n, 10)))
	}()
	listen := freePort(t)
	m.Apply([]*model.Rule{rule(1, model.ProtoTCP, listen, ln.Addr().(*net.TCPAddr).Port)})

	c := dialTCP(t, listen)
	c.Write(bytes.Repeat([]byte("x"), 100000))
	c.(*net.TCPConn).CloseWrite()
	resp, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp) != "100000" {
		t.Fatalf("got %q", resp)
	}
}

func TestUDPForwardSessionsAndReap(t *testing.T) {
	m, ev := testManager(t)
	target := udpEcho(t)
	listen := freePort(t)
	r := rule(1, model.ProtoUDP, listen, target)
	r.UDPIdleTimeoutS = 1
	m.Apply([]*model.Rule{r})

	send := func(c net.Conn, msg string) {
		t.Helper()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := c.Write([]byte(msg)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 100)
		n, err := c.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf[:n]) != "echo:"+msg {
			t.Fatalf("got %q", buf[:n])
		}
	}
	c1, _ := net.Dial("udp", "127.0.0.1:"+strconv.Itoa(listen))
	c2, _ := net.Dial("udp", "127.0.0.1:"+strconv.Itoa(listen))
	defer c1.Close()
	defer c2.Close()
	send(c1, "a")
	send(c1, "b")
	send(c2, "c")
	if n := m.Registry.Count(); n != 2 {
		t.Fatalf("expected 2 sessions, got %d", n)
	}
	cs := m.Counters()[1]
	if cs.ConnsOpened != 2 || cs.BytesIn != 3 {
		t.Fatalf("counters = %+v", cs)
	}
	// Idle timeout is 1s; the reaper runs every second.
	eventually(t, func() bool { return m.Registry.Count() == 0 }, "udp sessions reaped")
	eventually(t, func() bool { return ev.count("conn_close") == 2 }, "close events")
	// A new packet creates a fresh session.
	send(c1, "again")
	if n := m.Registry.Count(); n != 1 {
		t.Fatalf("expected 1 session, got %d", n)
	}
}

func TestBothProtocolsAndRange(t *testing.T) {
	m, _ := testManager(t)
	t1, p1 := tcpEcho(t)
	t2, p2 := tcpEcho(t)
	if t2 != t1+1 {
		t.Skip("echo servers not on consecutive ports")
	}
	listen := freeRange(t, 2)
	r := rule(1, model.ProtoTCP, listen, t1)
	r.ListenPortEnd = listen + 1
	m.Apply([]*model.Rule{r})
	for i, prefix := range [][]byte{p1, p2} {
		c := dialTCP(t, listen+i)
		buf := make([]byte, len(prefix))
		io.ReadFull(c, buf)
		if !bytes.Equal(buf, prefix) {
			t.Fatalf("port %d: got %q want %q", listen+i, buf, prefix)
		}
	}
}

func TestRangeOffsetMapping(t *testing.T) {
	r := &model.Rule{ListenPortStart: 27000, ListenPortEnd: 27050, TargetPortStart: 30000}
	if r.TargetPortFor(27010) != 30010 || r.TargetPortEnd() != 30050 {
		t.Fatal("bad mapping")
	}
}

func TestBanRejectsAndKicks(t *testing.T) {
	m, ev := testManager(t)
	target, prefix := tcpEcho(t)
	listen := freePort(t)
	m.Apply([]*model.Rule{rule(1, model.ProtoTCP, listen, target)})

	c := dialTCP(t, listen)
	buf := make([]byte, len(prefix))
	io.ReadFull(c, buf)
	eventually(t, func() bool { return m.Registry.Count() == 1 }, "conn registered")

	rid := int64(1)
	m.Bans.Replace([]*model.Ban{{CIDR: "127.0.0.0/8", RuleID: &rid}})
	if n := m.KickBanned(); n != 1 {
		t.Fatalf("kicked %d", n)
	}
	if _, err := c.Read(buf); err == nil {
		t.Fatal("expected kicked connection to be closed")
	}
	// New connections are rejected.
	c2 := dialTCP(t, listen)
	if _, err := c2.Read(buf); err == nil {
		t.Fatal("banned connection should be closed")
	}
	eventually(t, func() bool { return ev.count("conn_rejected") == 1 }, "conn_rejected event")
	// A ban on another rule does not apply.
	other := int64(2)
	m.Bans.Replace([]*model.Ban{{CIDR: "127.0.0.1", RuleID: &other}})
	c3 := dialTCP(t, listen)
	if _, err := io.ReadFull(c3, buf); err != nil {
		t.Fatal("unbanned connection failed:", err)
	}
	// Expired bans don't apply.
	past := time.Now().Add(-time.Minute).Unix()
	m.Bans.Replace([]*model.Ban{{CIDR: "127.0.0.1", ExpiresAt: &past}})
	if m.Bans.Banned(1, netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("expired ban applied")
	}
}

func TestKick(t *testing.T) {
	m, _ := testManager(t)
	target, prefix := tcpEcho(t)
	listen := freePort(t)
	m.Apply([]*model.Rule{rule(1, model.ProtoTCP, listen, target)})
	c := dialTCP(t, listen)
	buf := make([]byte, len(prefix))
	io.ReadFull(c, buf)
	eventually(t, func() bool { return m.Registry.Count() == 1 }, "conn registered")
	id := m.Registry.List(0)[0].ID
	if !m.Kick(id) {
		t.Fatal("kick failed")
	}
	if _, err := c.Read(buf); err == nil {
		t.Fatal("expected closed")
	}
}

func TestApplyDisableRetargetAndBindError(t *testing.T) {
	m, ev := testManager(t)
	t1, p1 := tcpEcho(t)
	t2, p2 := tcpEcho(t)
	listen := freePort(t)
	r := rule(1, model.ProtoTCP, listen, t1)
	m.Apply([]*model.Rule{r})

	c := dialTCP(t, listen)
	buf := make([]byte, len(p1))
	io.ReadFull(c, buf)
	eventually(t, func() bool { return m.Registry.Count() == 1 }, "conn registered")

	// Retarget: existing connection is closed, new ones go to the new target.
	r2 := *r
	r2.TargetPortStart = t2
	m.Apply([]*model.Rule{&r2})
	if _, err := c.Read(buf); err == nil {
		t.Fatal("retarget should close old connections")
	}
	c2 := dialTCP(t, listen)
	buf = make([]byte, len(p2))
	io.ReadFull(c2, buf)
	if !bytes.Equal(buf, p2) {
		t.Fatalf("got %q want %q", buf, p2)
	}

	// Disable: listener released.
	r3 := r2
	r3.Enabled = false
	m.Apply([]*model.Rule{&r3})
	if st := m.Status()[1]; st.State != StateDisabled {
		t.Fatalf("status %+v", st)
	}
	eventually(t, func() bool { return CheckPortFree(model.ProtoTCP, "127.0.0.1", listen) == nil }, "port released")

	// Bind error: port occupied by someone else.
	blocker, _ := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(listen))
	defer blocker.Close()
	r3.Enabled = true
	m.Apply([]*model.Rule{&r3})
	if st := m.Status()[1]; st.State != StateError || st.Error == "" {
		t.Fatalf("expected error status, got %+v", st)
	}
	if ev.count("rule_error") != 1 {
		t.Fatal("missing rule_error event")
	}
}

func TestResolverCachesAndRefreshes(t *testing.T) {
	r := NewResolver()
	r.TTL = 50 * time.Millisecond
	var calls atomic.Int32
	addr := atomic.Value{}
	addr.Store(netip.MustParseAddr("10.0.0.1"))
	r.lookupFn = func(ctx context.Context, host string) ([]netip.Addr, error) {
		calls.Add(1)
		return []netip.Addr{netip.MustParseAddr("2001:db8::1"), addr.Load().(netip.Addr)}, nil
	}
	a, err := r.Resolve(context.Background(), "example.test")
	if err != nil || a.String() != "10.0.0.1" {
		t.Fatalf("got %v %v (want IPv4 preferred)", a, err)
	}
	r.Resolve(context.Background(), "example.test")
	if calls.Load() != 1 {
		t.Fatal("expected cache hit")
	}
	addr.Store(netip.MustParseAddr("10.0.0.2"))
	time.Sleep(60 * time.Millisecond)
	r.Resolve(context.Background(), "example.test") // stale: triggers refresh, returns old
	eventually(t, func() bool {
		a, _ := r.Resolve(context.Background(), "example.test")
		return a.String() == "10.0.0.2"
	}, "refreshed address")
	if a, _ := r.Resolve(context.Background(), "1.2.3.4"); a.String() != "1.2.3.4" {
		t.Fatal("IP literal not passed through")
	}
}

func TestRuleValidation(t *testing.T) {
	cases := []struct {
		r     model.Rule
		field string
	}{
		{model.Rule{Name: "", Protocol: "tcp", ListenPortStart: 1, TargetHost: "a"}, "name"},
		{model.Rule{Name: "x", Protocol: "icmp", ListenPortStart: 1, TargetHost: "a"}, "protocol"},
		{model.Rule{Name: "x", Protocol: "tcp", ListenPortStart: 70000, TargetHost: "a"}, "listen_port_start"},
		{model.Rule{Name: "x", Protocol: "tcp", ListenPortStart: 10, ListenPortEnd: 5, TargetHost: "a"}, "listen_port_end"},
		{model.Rule{Name: "x", Protocol: "tcp", ListenPortStart: 1, ListenPortEnd: 2000, TargetHost: "a"}, "listen_port_end"},
		{model.Rule{Name: "x", Protocol: "tcp", ListenPortStart: 100, ListenPortEnd: 200, TargetPortStart: 65500, TargetHost: "a"}, "target_port_start"},
		{model.Rule{Name: "x", Protocol: "tcp", ListenPortStart: 1, TargetHost: "bad host!"}, "target_host"},
		{model.Rule{Name: "x", Protocol: "tcp", ListenPortStart: 1, TargetHost: "a", ListenIP: "nope"}, "listen_ip"},
	}
	for i, c := range cases {
		c.r.Normalize()
		err := c.r.Validate()
		fe, ok := err.(model.FieldErrors)
		if !ok || fe[c.field] == "" {
			t.Errorf("case %d: expected error on %s, got %v", i, c.field, err)
		}
	}
	ok := model.Rule{Name: "ts", Protocol: "both", ListenPortStart: 9987, TargetHost: "ts.example.ru"}
	ok.Normalize()
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	v6 := model.Rule{Name: "v6", Protocol: "udp", ListenPortStart: 1, TargetHost: "[2001:db8::1]"}
	v6.Normalize()
	if err := v6.Validate(); err != nil || v6.TargetHost != "2001:db8::1" {
		t.Fatal(err, v6.TargetHost)
	}
}
