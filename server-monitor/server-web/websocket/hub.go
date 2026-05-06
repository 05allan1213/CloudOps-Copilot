package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 1024
)

var (
	ErrHubClosed       = errors.New("websocket hub is shutting down")
	ErrRegisterChannel = errors.New("websocket register channel is full")
	ErrMaxConnections  = errors.New("websocket max connections reached")
)

type Hub struct {
	clients        map[*Client]bool
	register       chan *Client
	unregister     chan *Client
	broadcast      chan []byte
	done           chan struct{}
	once           sync.Once
	observerMu     sync.RWMutex
	observer       func(int)
	upgrader       websocket.Upgrader
	maxConnections int
}

func NewHub(maxConnections int, allowedOrigins ...[]string) *Hub {
	origins := mapAllowedOrigins(nil)
	if len(allowedOrigins) > 0 {
		origins = mapAllowedOrigins(allowedOrigins[0])
	}

	if maxConnections <= 0 {
		maxConnections = 1000
	}

	return &Hub{
		clients:        make(map[*Client]bool),
		register:       make(chan *Client, 16),
		unregister:     make(chan *Client, 64),
		broadcast:      make(chan []byte, 64),
		done:           make(chan struct{}),
		maxConnections: maxConnections,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return isOriginAllowed(r, origins)
			},
		},
	}
}

func (h *Hub) Run(ctx context.Context) {
	defer func() {
		h.once.Do(func() { close(h.done) })
		for client := range h.clients {
			close(client.send)
			delete(h.clients, client)
		}
		h.observeConnections()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case client := <-h.register:
			if h.maxConnections > 0 && len(h.clients) >= h.maxConnections {
				close(client.send)
				zap.L().Warn("websocket hub max connections reached, rejecting new connection",
					zap.Int("current", len(h.clients)),
					zap.Int("max", h.maxConnections),
				)
				break
			}
			h.clients[client] = true
			h.observeConnections()
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				h.observeConnections()
			}
		case message := <-h.broadcast:
			h.broadcastMessage(message)
		}
	}
}

func (h *Hub) broadcastMessage(message []byte) {
	defer func() {
		if r := recover(); r != nil {
			zap.L().Warn("websocket hub recovered panic during broadcast", zap.Any("error", r))
		}
	}()

	for client := range h.clients {
		select {
		case client.send <- message:
		default:
			zap.L().Warn("websocket hub client send buffer full, disconnecting")
			delete(h.clients, client)
			close(client.send)
			h.observeConnections()
		}
	}
}

func (h *Hub) SetConnectionObserver(observer func(int)) {
	h.observerMu.Lock()
	defer h.observerMu.Unlock()
	h.observer = observer
}

func (h *Hub) observeConnections() {
	h.observerMu.RLock()
	observer := h.observer
	h.observerMu.RUnlock()

	if observer != nil {
		observer(len(h.clients))
	}
}

func (h *Hub) Broadcast(message []byte) {
	if h == nil {
		return
	}

	select {
	case <-h.done:
		return
	case h.broadcast <- message:
	default:
		zap.L().Warn("websocket hub broadcast channel full, message dropped")
	}
}

func (h *Hub) BroadcastBlocking(ctx context.Context, message []byte) error {
	if h == nil {
		return ErrHubClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case <-h.done:
		return ErrHubClosed
	case <-ctx.Done():
		return ctx.Err()
	case h.broadcast <- message:
		return nil
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) error {
	select {
	case <-h.done:
		return ErrHubClosed
	default:
	}

	if h.maxConnections > 0 && len(h.clients) >= h.maxConnections {
		return ErrMaxConnections
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	client := &Client{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 32),
	}

	select {
	case <-h.done:
		conn.Close()
		return ErrHubClosed
	case h.register <- client:
	default:
		conn.Close()
		return ErrRegisterChannel
	}

	go client.writePump()
	go client.readPump()

	return nil
}

func mapAllowedOrigins(origins []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	return allowed
}

func isOriginAllowed(r *http.Request, allowed map[string]struct{}) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if _, ok := allowed["*"]; ok {
		return true
	}
	if _, ok := allowed[origin]; ok {
		return true
	}

	originURL, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(originURL.Host, r.Host)
}

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

func (c *Client) readPump() {
	defer func() {
		select {
		case c.hub.unregister <- c:
		default:
			zap.L().Warn("websocket hub unregister channel full, client may leak")
		}
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
