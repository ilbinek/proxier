package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"proxier/internal/app"
	"proxier/internal/stats"
)

// Hub fans out stats samples and events to connected WebSocket clients.
type Hub struct {
	app     *app.App
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

type wsMsg struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

func NewHub(a *app.App) *Hub {
	h := &Hub{app: a, clients: map[chan []byte]struct{}{}}
	a.Stats.OnSample(func(s stats.Sample) {
		if h.count() > 0 {
			h.broadcast(wsMsg{Type: "stats", Data: s})
		}
	})
	go func() {
		ch, _ := a.Events.Subscribe()
		for e := range ch {
			h.broadcast(wsMsg{Type: "event", Data: e})
		}
	}()
	return h
}

func (h *Hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) broadcast(m wsMsg) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- b:
		default: // slow client, drop frame
		}
	}
}

// ServeWS upgrades the request. coder/websocket rejects cross-origin requests
// unless the Origin host matches the request Host.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := conn.CloseRead(r.Context())

	send := make(chan []byte, 64)
	h.mu.Lock()
	h.clients[send] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, send)
		h.mu.Unlock()
	}()

	hello, _ := json.Marshal(wsMsg{Type: "hello", Data: map[string]any{"version": h.app.Version}})
	if write(ctx, conn, hello) != nil {
		return
	}
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-send:
			if write(ctx, conn, b) != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func write(ctx context.Context, c *websocket.Conn, b []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.Write(ctx, websocket.MessageText, b)
}
