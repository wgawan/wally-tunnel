package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/wgawan/wally-tunnel/internal/protocol"
)

const (
	requestTimeout = 30 * time.Second
	pingInterval   = 30 * time.Second
	pongTimeout    = 10 * time.Second

	// authRateLimit: max tunnel connection attempts per IP per window.
	authRateMax    = 5
	authRateWindow = time.Minute
)

// connLimiter tracks connection attempts per IP to mitigate brute-force auth.
type connLimiter struct {
	mu      sync.Mutex
	entries map[string]*limitEntry
}

type limitEntry struct {
	count   int
	resetAt time.Time
}

func newConnLimiter() *connLimiter {
	return &connLimiter{entries: make(map[string]*limitEntry)}
}

// allow returns true if the IP has not exceeded the rate limit.
func (l *connLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.entries[ip]
	if !ok || now.After(e.resetAt) {
		l.entries[ip] = &limitEntry{count: 1, resetAt: now.Add(authRateWindow)}
		return true
	}
	e.count++
	return e.count <= authRateMax
}

type Server struct {
	token    string
	domain   string
	registry *Registry
	limiter  *connLimiter
}

func New(token, domain string) *Server {
	return &Server{
		token:    token,
		domain:   domain,
		registry: NewRegistry(),
		limiter:  newConnLimiter(),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/_tunnel/ws" {
		s.handleTunnelClient(w, r)
		return
	}

	// Caddy on-demand TLS check: allow cert provisioning for any subdomain of our domain.
	// Restricted to loopback to prevent external abuse of certificate issuance.
	if r.URL.Path == "/_tunnel/check" {
		if !isLoopback(r.RemoteAddr) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		domain := r.URL.Query().Get("domain")
		// Always approve the tunnel server's own hostname (tunnel.<domain>).
		if domain == "tunnel."+s.domain {
			w.WriteHeader(http.StatusOK)
			return
		}
		sub := s.extractSubdomain(domain)
		if sub != "" {
			route, _ := s.registry.Resolve(sub)
			if route != nil {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		http.Error(w, "not an active subdomain of "+s.domain, http.StatusNotFound)
		return
	}

	// Extract subdomain from Host header
	subdomain := s.extractSubdomain(r.Host)
	if subdomain == "" {
		http.Error(w, "No tunnel configured for this host", http.StatusBadGateway)
		return
	}

	s.handleProxyRequest(w, r, subdomain)
}

func (s *Server) extractSubdomain(host string) string {
	// Strip port if present
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	suffix := "." + s.domain
	if !strings.HasSuffix(host, suffix) {
		return ""
	}
	sub := strings.TrimSuffix(host, suffix)
	if sub == "" || strings.Contains(sub, ".") {
		return ""
	}
	return sub
}

func (s *Server) handleTunnelClient(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	if !s.limiter.allow(ip) {
		http.Error(w, "too many connection attempts", http.StatusTooManyRequests)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // We handle auth ourselves
	})
	if err != nil {
		log.Printf("websocket accept error: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	conn.SetReadLimit(16 << 20) // 16MB

	ctx := r.Context()

	// Step 1: Authenticate
	if err := s.authenticate(ctx, conn); err != nil {
		log.Printf("auth failed: %v", err)
		conn.Close(websocket.StatusPolicyViolation, "auth failed")
		return
	}

	// Step 2: Register subdomains
	client, err := s.registerClient(ctx, conn)
	if err != nil {
		log.Printf("registration failed: %v", err)
		conn.Close(websocket.StatusPolicyViolation, "registration failed")
		return
	}
	defer s.registry.Unregister(client)

	log.Printf("client connected, subdomains: %v", keys(client.subdomains))

	// Step 3: Start keepalive and read loop
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go s.keepalive(ctx, cancel, client)
	s.readLoop(ctx, client)

	log.Printf("client disconnected, subdomains: %v", keys(client.subdomains))
}

func (s *Server) authenticate(ctx context.Context, conn *websocket.Conn) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, raw, err := conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("read auth: %w", err)
	}

	env, err := protocol.Unwrap(raw)
	if err != nil || env.Type != protocol.TypeAuth {
		return fmt.Errorf("expected auth message")
	}

	var auth protocol.AuthMsg
	if err := json.Unmarshal(env.Data, &auth); err != nil {
		return fmt.Errorf("unmarshal auth: %w", err)
	}

	ok := subtle.ConstantTimeCompare([]byte(auth.Token), []byte(s.token)) == 1
	resp, err := protocol.Wrap(protocol.TypeAuthResp, protocol.AuthRespMsg{
		OK:    ok,
		Error: ternary(!ok, "invalid token", ""),
	})
	if err != nil {
		return fmt.Errorf("marshal auth response: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
		return fmt.Errorf("write auth response: %w", err)
	}

	if !ok {
		return fmt.Errorf("invalid token")
	}
	return nil
}

func (s *Server) registerClient(ctx context.Context, conn *websocket.Conn) (*TunnelClient, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, raw, err := conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read register: %w", err)
	}

	env, err := protocol.Unwrap(raw)
	if err != nil || env.Type != protocol.TypeRegister {
		return nil, fmt.Errorf("expected register message")
	}

	var reg protocol.RegisterMsg
	if err := json.Unmarshal(env.Data, &reg); err != nil {
		return nil, fmt.Errorf("unmarshal register: %w", err)
	}

	client := newTunnelClient(conn, reg.Subdomains)
	active, taken, invalid := s.registry.Register(client, reg.Subdomains, reg.Options)

	var errParts []string
	if len(taken) > 0 {
		errParts = append(errParts, fmt.Sprintf("already taken: %v", taken))
	}
	if len(invalid) > 0 {
		errParts = append(errParts, fmt.Sprintf("invalid or reserved: %v", invalid))
	}
	errMsg := strings.Join(errParts, "; ")

	resp, err := protocol.Wrap(protocol.TypeRegisterAck, protocol.RegisterAckMsg{
		OK:     len(taken) == 0,
		Active: active,
		Error:  errMsg,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal register ack: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
		return nil, fmt.Errorf("write register ack: %w", err)
	}

	if len(active) == 0 {
		return nil, fmt.Errorf("no subdomains registered")
	}
	return client, nil
}

func (s *Server) readLoop(ctx context.Context, client *TunnelClient) {
	for {
		_, raw, err := client.conn.Read(ctx)
		if err != nil {
			return
		}

		env, err := protocol.Unwrap(raw)
		if err != nil {
			log.Printf("bad message from client: %v", err)
			continue
		}

		switch env.Type {
		case protocol.TypeHTTPResp:
			var resp protocol.HTTPRespMsg
			if err := json.Unmarshal(env.Data, &resp); err != nil {
				log.Printf("bad http_resp: %v", err)
				continue
			}
			client.mu.Lock()
			ch, ok := client.pending[resp.ID]
			if ok {
				delete(client.pending, resp.ID)
			}
			client.mu.Unlock()
			if ok {
				ch <- &resp
			}

		case protocol.TypeHTTPRespHead:
			var head protocol.HTTPRespHeadMsg
			if err := json.Unmarshal(env.Data, &head); err != nil {
				log.Printf("bad http_resp_head: %v", err)
				continue
			}
			client.mu.Lock()
			sw, ok := client.streams[head.ID]
			client.mu.Unlock()
			if ok {
				for k, vals := range head.Headers {
					for _, v := range vals {
						sw.w.Header().Add(k, v)
					}
				}
				sw.w.WriteHeader(head.StatusCode)
				if sw.flusher != nil {
					sw.flusher.Flush()
				}
				select {
				case <-sw.started:
				default:
					close(sw.started)
				}
			}

		case protocol.TypeHTTPRespBody:
			var body protocol.HTTPRespBodyMsg
			if err := json.Unmarshal(env.Data, &body); err != nil {
				log.Printf("bad http_resp_body: %v", err)
				continue
			}
			client.mu.Lock()
			sw, ok := client.streams[body.ID]
			client.mu.Unlock()
			if ok {
				if _, err := sw.w.Write(body.Data); err != nil {
					log.Printf("stream write failed for %s: %v", body.ID, err)
				}
				if sw.flusher != nil {
					sw.flusher.Flush()
				}
			}

		case protocol.TypeHTTPRespEnd:
			var end protocol.HTTPRespEndMsg
			if err := json.Unmarshal(env.Data, &end); err != nil {
				continue
			}
			client.mu.Lock()
			sw, ok := client.streams[end.ID]
			if ok {
				delete(client.streams, end.ID)
			}
			client.mu.Unlock()
			if ok {
				close(sw.done)
			}

		case protocol.TypeWSOpenResp:
			var resp protocol.WSOpenRespMsg
			if err := json.Unmarshal(env.Data, &resp); err != nil {
				log.Printf("bad ws_open_resp: %v", err)
				continue
			}
			client.mu.Lock()
			ch, ok := client.wsOpen[resp.ID]
			client.mu.Unlock()
			if ok {
				ch <- &resp
			}

		case protocol.TypeWSFrame:
			var frame protocol.WSFrameMsg
			if err := json.Unmarshal(env.Data, &frame); err != nil {
				log.Printf("bad ws_frame: %v", err)
				continue
			}
			// Forward frame to the internet-facing WebSocket
			client.mu.Lock()
			wsConn, ok := client.wsConns[frame.ID]
			client.mu.Unlock()
			if ok {
				msgType := websocket.MessageBinary
				if frame.IsText {
					msgType = websocket.MessageText
				}
				if err := wsConn.Write(ctx, msgType, frame.Data); err != nil {
					log.Printf("ws proxy: write to external ws %s failed: %v", frame.ID, err)
					client.mu.Lock()
					delete(client.wsConns, frame.ID)
					client.mu.Unlock()
					wsConn.Close(websocket.StatusNormalClosure, "")
					// Notify tunnel client that the WS closed
					if closeMsg, err := protocol.Wrap(protocol.TypeWSClose, protocol.WSCloseMsg{ID: frame.ID}); err == nil {
						client.mu.Lock()
						if wErr := client.conn.Write(ctx, websocket.MessageText, closeMsg); wErr != nil {
							log.Printf("ws proxy: failed to notify client of ws close %s: %v", frame.ID, wErr)
						}
						client.mu.Unlock()
					}
				}
			}

		case protocol.TypeWSClose:
			var closeMsg protocol.WSCloseMsg
			if err := json.Unmarshal(env.Data, &closeMsg); err != nil {
				continue
			}
			client.mu.Lock()
			wsConn, ok := client.wsConns[closeMsg.ID]
			if ok {
				delete(client.wsConns, closeMsg.ID)
			}
			client.mu.Unlock()
			if ok {
				wsConn.Close(websocket.StatusNormalClosure, "")
			}

		case protocol.TypePong:
			// keepalive response, nothing to do
		default:
			log.Printf("unexpected message type from client: %s", env.Type)
		}
	}
}

func (s *Server) keepalive(ctx context.Context, cancel context.CancelFunc, client *TunnelClient) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg, err := protocol.Wrap(protocol.TypePing, nil)
			if err != nil {
				log.Printf("keepalive marshal failed: %v", err)
				cancel()
				return
			}
			writeCtx, writeCancel := context.WithTimeout(ctx, pongTimeout)
			client.mu.Lock()
			err = client.conn.Write(writeCtx, websocket.MessageText, msg)
			client.mu.Unlock()
			writeCancel()
			if err != nil {
				log.Printf("keepalive write failed: %v", err)
				cancel()
				return
			}
		}
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (s *Server) handleProxyRequest(w http.ResponseWriter, r *http.Request, subdomain string) {
	route, expired := s.registry.Resolve(subdomain)
	if route == nil {
		if expired {
			http.Error(w, "Tunnel expired", http.StatusGone)
			return
		}
		http.Error(w, "No tunnel connected for this host", http.StatusBadGateway)
		return
	}
	if !s.authorizeTunnelRequest(w, r, route) {
		return
	}
	client := route.client

	if isWebSocketUpgrade(r) {
		s.handleWSProxy(w, r, client)
		return
	}

	msg, err := requestToMsg(r)
	if err != nil {
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}

	// Create response channel for non-streaming responses
	ch := make(chan *protocol.HTTPRespMsg, 1)
	client.mu.Lock()
	client.pending[msg.ID] = ch
	client.mu.Unlock()

	// Also register a stream writer in case this is a streaming response
	flusher, _ := w.(http.Flusher)
	sw := &streamWriter{w: w, flusher: flusher, started: make(chan struct{}), done: make(chan struct{})}
	client.mu.Lock()
	client.streams[msg.ID] = sw
	client.mu.Unlock()

	// Clean up on exit
	defer func() {
		client.mu.Lock()
		delete(client.pending, msg.ID)
		delete(client.streams, msg.ID)
		client.mu.Unlock()
	}()

	// Send request to client
	data, err := protocol.Wrap(protocol.TypeHTTPReq, msg)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	writeCtx, writeCancel := context.WithTimeout(r.Context(), 5*time.Second)
	client.mu.Lock()
	err = client.conn.Write(writeCtx, websocket.MessageText, data)
	client.mu.Unlock()
	writeCancel()
	if err != nil {
		http.Error(w, "Tunnel connection error", http.StatusBadGateway)
		return
	}

	// Wait for either a complete response or streaming headers (with timeout).
	select {
	case resp := <-ch:
		// Normal (non-streaming) response
		writeResponse(w, resp)
		return
	case <-sw.started:
		// Streaming response started — headers already written.
		// Now wait until the stream ends or client disconnects.
		select {
		case <-sw.done:
		case <-r.Context().Done():
		}
		return
	case <-time.After(requestTimeout):
		http.Error(w, "Tunnel request timed out", http.StatusGatewayTimeout)
		return
	case <-r.Context().Done():
		return
	}
}

func (s *Server) authorizeTunnelRequest(w http.ResponseWriter, r *http.Request, route *routeEntry) bool {
	if route.protection.basicAuth == nil {
		return true
	}

	username, password, ok := r.BasicAuth()
	expected := route.protection.basicAuth
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(expected.Username)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(password), []byte(expected.Password)) == 1
	if ok && userOK && passOK {
		return true
	}

	w.Header().Set("WWW-Authenticate", `Basic realm="wally-tunnel", charset="UTF-8"`)
	http.Error(w, "Authentication required", http.StatusUnauthorized)
	return false
}

func (s *Server) handleWSProxy(w http.ResponseWriter, r *http.Request, client *TunnelClient) {
	// Accept the WebSocket from the internet-facing client.
	// Echo back any subprotocols the browser requested (e.g. vite-hmr)
	// so the browser handshake succeeds.
	acceptOpts := &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	}
	if protos := r.Header.Values("Sec-WebSocket-Protocol"); len(protos) > 0 {
		acceptOpts.Subprotocols = protos
	}
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		log.Printf("ws proxy: accept error: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	conn.SetReadLimit(16 << 20)

	id := fmt.Sprintf("ws-%s", uuid.New().String())

	path := r.URL.Path
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}

	// Register this WS connection
	client.mu.Lock()
	client.wsConns[id] = conn
	client.mu.Unlock()
	defer func() {
		client.mu.Lock()
		delete(client.wsConns, id)
		client.mu.Unlock()
		// Notify tunnel client that the WS closed
		if msg, err := protocol.Wrap(protocol.TypeWSClose, protocol.WSCloseMsg{ID: id}); err == nil {
			client.mu.Lock()
			if wErr := client.conn.Write(context.Background(), websocket.MessageText, msg); wErr != nil {
				log.Printf("ws proxy: failed to send ws_close for %s: %v", id, wErr)
			}
			client.mu.Unlock()
		}
	}()

	// Tell tunnel client to open a WebSocket to the local service
	openMsg, err := protocol.Wrap(protocol.TypeWSOpen, protocol.WSOpenMsg{
		ID:      id,
		Path:    path,
		Host:    r.Host,
		Headers: r.Header,
	})
	if err != nil {
		log.Printf("ws proxy: failed to marshal ws_open: %v", err)
		return
	}

	// Create a channel to wait for the open response
	openCh := make(chan *protocol.WSOpenRespMsg, 1)
	client.mu.Lock()
	client.wsOpen[id] = openCh
	err = client.conn.Write(r.Context(), websocket.MessageText, openMsg)
	client.mu.Unlock()
	if err != nil {
		log.Printf("ws proxy: failed to send ws_open: %v", err)
		return
	}

	// Wait for open response
	var openResp *protocol.WSOpenRespMsg
	select {
	case openResp = <-openCh:
	case <-time.After(10 * time.Second):
		log.Printf("ws proxy: timeout waiting for ws_open_resp")
		client.mu.Lock()
		delete(client.wsOpen, id)
		client.mu.Unlock()
		return
	}

	client.mu.Lock()
	delete(client.wsOpen, id)
	client.mu.Unlock()

	if !openResp.OK {
		log.Printf("ws proxy: client failed to open local ws: %s", openResp.Error)
		conn.Close(websocket.StatusInternalError, "failed to connect to local service")
		return
	}

	ctx := r.Context()

	// Read frames from the internet-facing WebSocket and forward to tunnel client
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		isText := msgType == websocket.MessageText
		frame, err := protocol.Wrap(protocol.TypeWSFrame, protocol.WSFrameMsg{
			ID:     id,
			IsText: isText,
			Data:   data,
		})
		if err != nil {
			log.Printf("ws proxy: failed to marshal ws_frame: %v", err)
			return
		}

		client.mu.Lock()
		err = client.conn.Write(ctx, websocket.MessageText, frame)
		client.mu.Unlock()
		if err != nil {
			return
		}
	}
}

func isLoopback(remoteAddr string) bool {
	host := remoteAddr
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	// Strip brackets from IPv6 (e.g., "[::1]")
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return host == "127.0.0.1" || host == "::1"
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func keys(m map[string]int) []string {
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	return k
}
