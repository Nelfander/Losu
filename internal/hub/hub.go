package hub

import (
	"sync"

	"github.com/gorilla/websocket"
)

const (
	// sendBufferSize is how many messages we'll queue per client before
	// we consider them too slow and drop the connection
	// At 500ms intervals this gives a client 4 seconds to catch up
	sendBufferSize = 8
)

// Client represents a single connected browser tab,
// it owns its WebSocket connection and a buffered channel of outgoing messages
type Client struct {
	conn *websocket.Conn
	send chan []byte // buffered outbox — hub writes, writePump reads
}

// Hub is the single owner of all active WebSocket clients.
// All access to the client map goes through the Hub's own Run() goroutine
// via channels — no mutex needed on the map itself.
type Hub struct {
	// clients is the set of currently connected clients.
	// Only the Run() goroutine reads/writes this map.
	clients    map[*Client]struct{}
	register   chan *Client // register is sent a new Client when a browser connects.
	unregister chan *Client // unregister is sent a Client when it disconnects or is dropped.

	// broadcast is sent a JSON payload every 500ms by the server.
	// Run() fans it out to all connected clients.
	broadcast chan []byte
	wg        sync.WaitGroup // wg tracks active writePump goroutines so Shutdown() can wait for them.
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 1), // size means that if a broadcast is already pending, skip
	}
}

// Run is the hub's event loop. It must be started in its own goroutine.
// It is the ONLY place that reads or writes the clients map — this is what
// makes the hub safe without a mutex on the map.
func (h *Hub) Run() {
	for {
		select {

		// A new browser tab connected — add it to the map.
		case client := <-h.register:
			h.clients[client] = struct{}{}

		// A client disconnected or was dropped — remove and clean up.
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send) // closing triggers writePump to exit
			}

		// A new payload is ready — fan it out to every connected client.
		case payload := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- payload:
					// Delivered to client's outbox successfully.
				default:
					// Client's buffer is full — they're too slow.
					// Drop them now so they don't stall everyone else.
					delete(h.clients, client)
					close(client.send)
				}
			}
		}
	}
}

// Broadcast sends a payload to all connected clients.
// Non-blocking: if the broadcast channel already has a pending message,
// we skip this one. At 500ms intervals this is never a real problem.
func (h *Hub) Broadcast(payload []byte) {
	select {
	case h.broadcast <- payload:
	default:
		// Previous broadcast not yet consumed by Run() — skip this one.
	}
}

// ServeClient registers a new client with the hub and starts its write pump.
// Called by the HTTP handler when a WebSocket upgrade succeeds.
func (h *Hub) ServeClient(conn *websocket.Conn) {
	client := &Client{
		conn: conn,
		send: make(chan []byte, sendBufferSize),
	}

	// Register with the hub's event loop.
	h.register <- client

	// Start the write pump for this client in its own goroutine.
	// It will run until the client disconnects or is dropped by the hub.
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		client.writePump(h)
	}()
}

// writePump drains the client's send channel and writes to the WebSocket.
// One goroutine per connected client — they never share state with each other.
func (c *Client) writePump(h *Hub) {
	defer func() {
		// Tell the hub to clean up this client, then close the connection.
		// We send to unregister only if send is still open (hub may have
		// already closed it if it dropped us for being slow).
		h.unregister <- c
		c.conn.Close()
	}()

	for payload := range c.send {
		// WriteMessage is safe to call from a single goroutine — this one.
		if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			// Connection is broken — exit the loop, defer will clean up.
			return
		}
	}
	// send was closed by the hub (slow client drop or shutdown) — exit cleanly.
}
