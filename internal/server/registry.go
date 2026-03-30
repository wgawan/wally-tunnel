package server

import (
	"net/http"
	"sync"

	"github.com/wgawan/wally-tunnel/internal/protocol"
	"nhooyr.io/websocket"
)

var _ = protocol.TypeAuth // ensure import

// streamWriter handles streaming responses (SSE, chunked) to the original HTTP client.
type streamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	started chan struct{} // closed when headers have been sent
	done    chan struct{} // closed when the stream ends
}

type TunnelClient struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	pending map[string]chan *protocol.HTTPRespMsg
	// streams tracks active streaming responses (id -> streamWriter)
	streams map[string]*streamWriter
	// wsConns tracks active proxied WebSocket connections (id -> server-side WS conn)
	wsConns map[string]*websocket.Conn
	// wsOpen tracks pending WebSocket open responses
	wsOpen map[string]chan *protocol.WSOpenRespMsg
	// subdomains this client owns (subdomain -> local port on client side)
	subdomains map[string]int
}

func newTunnelClient(conn *websocket.Conn, subdomains map[string]int) *TunnelClient {
	return &TunnelClient{
		conn:       conn,
		pending:    make(map[string]chan *protocol.HTTPRespMsg),
		streams:    make(map[string]*streamWriter),
		wsConns:    make(map[string]*websocket.Conn),
		wsOpen:     make(map[string]chan *protocol.WSOpenRespMsg),
		subdomains: subdomains,
	}
}

type Registry struct {
	mu      sync.RWMutex
	tunnels map[string]*TunnelClient // subdomain -> client
}

func NewRegistry() *Registry {
	return &Registry{
		tunnels: make(map[string]*TunnelClient),
	}
}

// Register claims subdomains for a client. Returns the list of successfully registered
// subdomains and any that were already taken.
func (r *Registry) Register(client *TunnelClient, subdomains map[string]int) (active []string, taken []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for sub := range subdomains {
		if existing, ok := r.tunnels[sub]; ok && existing != client {
			taken = append(taken, sub)
			continue
		}
		r.tunnels[sub] = client
		active = append(active, sub)
	}
	return active, taken
}

// Unregister removes all subdomains owned by a client.
func (r *Registry) Unregister(client *TunnelClient) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for sub, c := range r.tunnels {
		if c == client {
			delete(r.tunnels, sub)
		}
	}
}

// Lookup finds the tunnel client for a subdomain.
func (r *Registry) Lookup(subdomain string) *TunnelClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tunnels[subdomain]
}

// ActiveSubdomains returns a list of all currently registered subdomains.
func (r *Registry) ActiveSubdomains() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	subs := make([]string, 0, len(r.tunnels))
	for sub := range r.tunnels {
		subs = append(subs, sub)
	}
	return subs
}
