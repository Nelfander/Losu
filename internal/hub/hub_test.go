package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// --- Helpers ---

// startHub starts the hub event loop and returns a cleanup function.
func startHub(h *Hub) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Run()
	}()
	return func() {}
}

// newTestServer creates a test HTTP server that upgrades connections to WebSocket
// and registers them with the hub. Returns the server and its WebSocket URL.
func newTestServer(t *testing.T, h *Hub) (*httptest.Server, string) {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		h.ServeClient(conn)
	}))
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	return srv, wsURL
}

// dialWS connects a test WebSocket client to the given URL.
func dialWS(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	return conn
}

// --- Tests ---

// TestHub_BroadcastReachesClient verifies that a payload sent via Broadcast()
// is received by a connected client.
func TestHub_BroadcastReachesClient(t *testing.T) {
	h := NewHub()
	startHub(h)

	srv, wsURL := newTestServer(t, h)
	defer srv.Close()

	conn := dialWS(t, wsURL)
	defer conn.Close()

	// Give hub time to register the client
	time.Sleep(50 * time.Millisecond)

	payload := []byte(`{"total_lines":42}`)
	h.Broadcast(payload)

	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}
	if string(msg) != string(payload) {
		t.Errorf("got %q want %q", msg, payload)
	}
}

// TestHub_BroadcastReachesAllClients verifies fan-out — every connected
// client receives the same payload.
func TestHub_BroadcastReachesAllClients(t *testing.T) {
	h := NewHub()
	startHub(h)

	srv, wsURL := newTestServer(t, h)
	defer srv.Close()

	// Connect 3 clients
	clients := make([]*websocket.Conn, 3)
	for i := range clients {
		clients[i] = dialWS(t, wsURL)
		defer clients[i].Close()
	}

	// Give hub time to register all clients
	time.Sleep(100 * time.Millisecond)

	payload := []byte(`{"status":"ok"}`)
	h.Broadcast(payload)

	// Every client should receive the payload
	for i, conn := range clients {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("client %d failed to receive message: %v", i, err)
			continue
		}
		if string(msg) != string(payload) {
			t.Errorf("client %d: got %q want %q", i, msg, payload)
		}
	}
}

// TestHub_ClientDisconnect verifies that when a client disconnects,
// the hub removes it cleanly and subsequent broadcasts don't panic.
func TestHub_ClientDisconnect(t *testing.T) {
	h := NewHub()
	startHub(h)

	srv, wsURL := newTestServer(t, h)
	defer srv.Close()

	conn := dialWS(t, wsURL)
	time.Sleep(50 * time.Millisecond)

	// Disconnect the client
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	// Broadcasting after disconnect should not panic
	for i := 0; i < 5; i++ {
		h.Broadcast([]byte(`{"ping":true}`))
		time.Sleep(10 * time.Millisecond)
	}
}

// TestHub_NonBlockingBroadcast verifies that Broadcast() never blocks
// even when called rapidly — the non-blocking select must handle a full channel.
func TestHub_NonBlockingBroadcast(t *testing.T) {
	h := NewHub()
	startHub(h)

	// No clients connected — broadcast channel may back up
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			h.Broadcast([]byte(`{"tick":true}`))
		}
	}()

	select {
	case <-done:
		// All 100 broadcasts completed without blocking
	case <-time.After(500 * time.Millisecond):
		t.Error("Broadcast() blocked — non-blocking select not working")
	}
}

// TestHub_SlowClientDropped verifies that a client whose send buffer is full
// gets dropped by the hub rather than stalling broadcasts to other clients.
func TestHub_SlowClientDropped(t *testing.T) {
	h := NewHub()
	startHub(h)

	srv, wsURL := newTestServer(t, h)
	defer srv.Close()

	// Connect a client but never read from it — simulates a slow/frozen browser tab
	slowConn := dialWS(t, wsURL)
	defer slowConn.Close()

	// Connect a healthy client that reads normally
	goodConn := dialWS(t, wsURL)
	defer goodConn.Close()

	time.Sleep(100 * time.Millisecond)

	// Flood with enough messages to overflow the slow client's 8-slot buffer
	for i := 0; i < sendBufferSize+5; i++ {
		h.Broadcast([]byte(`{"flood":true}`))
		time.Sleep(10 * time.Millisecond)
	}

	// The good client should still receive messages — slow client didn't block it
	goodConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err := goodConn.ReadMessage()
	if err != nil {
		t.Error("good client was starved by slow client — hub fan-out is blocking")
	}
}

// TestHub_NewHub verifies that NewHub initialises all channels correctly.
func TestHub_NewHub(t *testing.T) {
	h := NewHub()

	if h.clients == nil {
		t.Error("clients map is nil")
	}
	if h.register == nil {
		t.Error("register channel is nil")
	}
	if h.unregister == nil {
		t.Error("unregister channel is nil")
	}
	if h.broadcast == nil {
		t.Error("broadcast channel is nil")
	}
}
