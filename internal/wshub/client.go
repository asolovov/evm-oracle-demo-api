package wshub

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

const (
	pingInterval = 30 * time.Second
	pongTimeout  = 60 * time.Second
	writeTimeout = 10 * time.Second
)

// upgrader is shared across all /ws/stream upgrades. Origin checks happen
// upstream in the CORS middleware — the hub itself trusts whatever the
// CORS policy let through.
var upgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	ReadBufferSize:   4 * 1024,
	WriteBufferSize:  4 * 1024,
	CheckOrigin:      func(_ *http.Request) bool { return true },
}

// Client is one connected WebSocket subscriber.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

	closeOnce sync.Once
	closed    chan struct{}
}

// Serve upgrades the HTTP request to a WebSocket connection, registers the
// resulting client with the hub, and runs the read + write pumps until the
// client disconnects or the hub shuts down.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// upgrader.Upgrade already wrote the HTTP error response.
		logger.Log().WithError(err).Debug("wshub: ws upgrade failed")
		return
	}

	client := &Client{
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, h.clientBufferSize),
		closed: make(chan struct{}),
	}
	h.register(client)

	// Read deadline + pong handler — the hub closes idle clients after
	// pongTimeout without a pong response.
	_ = conn.SetReadDeadline(time.Now().Add(pongTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongTimeout))
	})

	go client.writePump()
	client.readPump()
}

// readPump blocks on conn.ReadMessage until the client disconnects or fails
// the pong deadline.
func (c *Client) readPump() {
	defer c.close()
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

// writePump drains c.send onto the conn, emitting periodic pings to keep
// the connection alive and detect dead peers.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.close()
	}()

	for {
		select {
		case payload, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.closed:
			return
		}
	}
}

// close is idempotent — multiple paths (hub broadcast drop, read failure,
// hub shutdown) can race to close a client; only the first call matters.
func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.hub.unregister(c)
	})
}
