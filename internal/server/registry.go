package server

import (
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/wgawan/wally-tunnel/internal/protocol"
)

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

type tunnelProtection struct {
	basicAuth *protocol.BasicAuthConfig
	expiresAt time.Time
}

func protectionFromOptions(opts protocol.TunnelOptions, now time.Time) tunnelProtection {
	p := tunnelProtection{
		basicAuth: opts.BasicAuth,
	}
	if opts.ExpiresInSeconds > 0 {
		p.expiresAt = now.Add(time.Duration(opts.ExpiresInSeconds) * time.Second)
	}
	return p
}

func (p tunnelProtection) expired(now time.Time) bool {
	return !p.expiresAt.IsZero() && !now.Before(p.expiresAt)
}

type routeEntry struct {
	client     *TunnelClient
	protection tunnelProtection
}

// reservedSubdomains are names that cannot be claimed by tunnel clients.
var reservedSubdomains = map[string]bool{
	"www": true, "mail": true, "smtp": true, "imap": true, "pop": true,
	"api": true, "admin": true, "ns1": true, "ns2": true,
	"ftp": true, "ssh": true, "localhost": true,
	"_tunnel": true, "_dmarc": true,
}

// validSubdomain matches lowercase alphanumeric names with optional hyphens, 1-63 chars.
var validSubdomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

type Registry struct {
	mu      sync.RWMutex
	tunnels map[string]*routeEntry // subdomain -> route entry
}

func NewRegistry() *Registry {
	return &Registry{
		tunnels: make(map[string]*routeEntry),
	}
}

// Register claims subdomains for a client. Returns the list of successfully registered
// subdomains, any that were already taken, and any that were rejected as invalid.
func (r *Registry) Register(client *TunnelClient, subdomains map[string]int, options map[string]protocol.TunnelOptions) (active []string, taken []string, invalid []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for sub := range subdomains {
		if !validSubdomain.MatchString(sub) || reservedSubdomains[sub] {
			invalid = append(invalid, sub)
			continue
		}
		if existing, ok := r.tunnels[sub]; ok && existing.client != client {
			taken = append(taken, sub)
			continue
		}
		r.tunnels[sub] = &routeEntry{
			client:     client,
			protection: protectionFromOptions(options[sub], now),
		}
		active = append(active, sub)
	}
	return active, taken, invalid
}

// Unregister removes all subdomains owned by a client.
func (r *Registry) Unregister(client *TunnelClient) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for sub, c := range r.tunnels {
		if c.client == client {
			delete(r.tunnels, sub)
		}
	}
}

// Resolve returns the route entry for a subdomain and lazily removes expired routes.
func (r *Registry) Resolve(subdomain string) (*routeEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.tunnels[subdomain]
	if entry == nil {
		return nil, false
	}
	if entry.protection.expired(time.Now()) {
		delete(r.tunnels, subdomain)
		return nil, true
	}
	return entry, false
}

// Lookup finds the tunnel client for a subdomain.
func (r *Registry) Lookup(subdomain string) *TunnelClient {
	entry, _ := r.Resolve(subdomain)
	if entry == nil {
		return nil
	}
	return entry.client
}

// ActiveSubdomains returns a list of all currently registered subdomains.
func (r *Registry) ActiveSubdomains() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	subs := make([]string, 0, len(r.tunnels))
	for sub, entry := range r.tunnels {
		if entry.protection.expired(now) {
			continue
		}
		subs = append(subs, sub)
	}
	return subs
}
