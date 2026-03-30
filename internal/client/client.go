package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wgawan/wally-tunnel/internal/protocol"
	"nhooyr.io/websocket"
)

const (
	maxBackoff     = 30 * time.Second
	initialBackoff = 1 * time.Second
)

// Mapping holds the local port(s) for a subdomain.
// If WS is non-zero, WebSocket upgrades are routed there instead of HTTP.
type Mapping struct {
	HTTP int
	WS   int
}

// WSPort returns the port to use for WebSocket connections.
func (m Mapping) WSPort() int {
	if m.WS != 0 {
		return m.WS
	}
	return m.HTTP
}

type Client struct {
	ServerURL string             // e.g., wss://tunnel.yourdomain.dev
	Token     string
	Mappings  map[string]Mapping // subdomain -> local port(s)
	Domain    string             // e.g., yourdomain.dev (for display only)

	// connMu serializes writes to the tunnel WebSocket connection.
	// nhooyr.io/websocket Conn.Write is not safe for concurrent use.
	connMu sync.Mutex

	// active local WebSocket connections (ws ID -> local WS conn)
	wsMu    sync.Mutex
	wsConns map[string]*websocket.Conn
}

// writeMsg serializes writes to the tunnel WebSocket connection.
func (c *Client) writeMsg(ctx context.Context, conn *websocket.Conn, data []byte) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return conn.Write(ctx, websocket.MessageText, data)
}

func (c *Client) Run(ctx context.Context) error {
	backoff := initialBackoff

	for {
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Printf("disconnected: %v", err)
		log.Printf("reconnecting in %s...", backoff)

		select {
		case <-time.After(backoff):
			backoff = min(backoff*2, maxBackoff)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *Client) connect(ctx context.Context) error {
	wsURL := strings.TrimRight(c.ServerURL, "/") + "/_tunnel/ws"
	log.Printf("connecting to %s", wsURL)

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	conn.SetReadLimit(16 << 20) // 16MB

	c.wsMu.Lock()
	c.wsConns = make(map[string]*websocket.Conn)
	c.wsMu.Unlock()

	if err := c.authenticate(ctx, conn); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	if err := c.register(ctx, conn); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	log.Printf("tunnel established!")
	for sub, m := range c.Mappings {
		label := fmt.Sprintf("localhost:%d", m.HTTP)
		if m.WS != 0 {
			label += fmt.Sprintf(" (ws: localhost:%d)", m.WS)
		}
		if c.Domain != "" {
			log.Printf("  %s.%s -> %s", sub, c.Domain, label)
		} else {
			log.Printf("  %s -> %s", sub, label)
		}
	}

	return c.readLoop(ctx, conn)
}

func (c *Client) authenticate(ctx context.Context, conn *websocket.Conn) error {
	msg, _ := protocol.Wrap(protocol.TypeAuth, protocol.AuthMsg{Token: c.Token})
	if err := c.writeMsg(ctx, conn, msg); err != nil {
		return err
	}

	_, raw, err := conn.Read(ctx)
	if err != nil {
		return err
	}

	env, err := protocol.Unwrap(raw)
	if err != nil {
		return err
	}

	var resp protocol.AuthRespMsg
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("authentication failed: %s", resp.Error)
	}
	return nil
}

func (c *Client) register(ctx context.Context, conn *websocket.Conn) error {
	// Convert to protocol format (server only needs subdomain names; port is informational)
	subs := make(map[string]int, len(c.Mappings))
	for sub, m := range c.Mappings {
		subs[sub] = m.HTTP
	}
	msg, _ := protocol.Wrap(protocol.TypeRegister, protocol.RegisterMsg{Subdomains: subs})
	if err := c.writeMsg(ctx, conn, msg); err != nil {
		return err
	}

	_, raw, err := conn.Read(ctx)
	if err != nil {
		return err
	}

	env, err := protocol.Unwrap(raw)
	if err != nil {
		return err
	}

	var resp protocol.RegisterAckMsg
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		log.Printf("warning: %s", resp.Error)
	}
	if len(resp.Active) == 0 {
		return fmt.Errorf("no subdomains were registered")
	}
	return nil
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return err
		}

		env, err := protocol.Unwrap(raw)
		if err != nil {
			log.Printf("bad message: %v", err)
			continue
		}

		switch env.Type {
		case protocol.TypeHTTPReq:
			var req protocol.HTTPReqMsg
			if err := json.Unmarshal(env.Data, &req); err != nil {
				log.Printf("bad http_req: %v", err)
				continue
			}
			go c.handleRequest(ctx, conn, &req)

		case protocol.TypeWSOpen:
			var open protocol.WSOpenMsg
			if err := json.Unmarshal(env.Data, &open); err != nil {
				log.Printf("bad ws_open: %v", err)
				continue
			}
			go c.handleWSOpen(ctx, conn, &open)

		case protocol.TypeWSFrame:
			var frame protocol.WSFrameMsg
			if err := json.Unmarshal(env.Data, &frame); err != nil {
				log.Printf("bad ws_frame: %v", err)
				continue
			}
			go c.handleWSFrame(ctx, &frame)

		case protocol.TypeWSClose:
			var closeMsg protocol.WSCloseMsg
			if err := json.Unmarshal(env.Data, &closeMsg); err != nil {
				continue
			}
			c.handleWSClose(&closeMsg)

		case protocol.TypePing:
			msg, _ := protocol.Wrap(protocol.TypePong, nil)
			if err := c.writeMsg(ctx, conn, msg); err != nil {
				log.Printf("pong write error: %v", err)
			}

		default:
			log.Printf("unexpected message type: %s", env.Type)
		}
	}
}

func (c *Client) handleRequest(ctx context.Context, conn *websocket.Conn, req *protocol.HTTPReqMsg) {
	subdomain := extractSubdomain(req.Host, c.Domain)
	m, ok := c.Mappings[subdomain]
	if !ok {
		log.Printf("unknown subdomain %q in request, no mapping found", subdomain)
		return
	}

	log.Printf("%s %s -> localhost:%d", req.Method, req.Path, m.HTTP)

	if err := checkAndForward(ctx, conn, req, m.HTTP, c.writeMsg); err != nil {
		log.Printf("forward error: %v", err)
	}
}

func (c *Client) handleWSOpen(ctx context.Context, tunnelConn *websocket.Conn, open *protocol.WSOpenMsg) {
	subdomain := extractSubdomain(open.Host, c.Domain)
	m, ok := c.Mappings[subdomain]
	if !ok {
		log.Printf("ws: unknown subdomain %q", subdomain)
		resp, _ := protocol.Wrap(protocol.TypeWSOpenResp, protocol.WSOpenRespMsg{
			ID: open.ID, OK: false, Error: "unknown subdomain",
		})
		_ = c.writeMsg(ctx, tunnelConn, resp)
		return
	}

	// Connect to local WebSocket (use WS port if configured, otherwise HTTP port)
	port := m.WSPort()
	localURL := fmt.Sprintf("ws://localhost:%d%s", port, open.Path)
	log.Printf("ws: opening %s -> %s", open.ID, localURL)

	// Build dial options: forward auth headers and subprotocols to local service.
	origHeaders := http.Header(open.Headers)
	dialHeaders := make(http.Header)
	if v := origHeaders.Get("Cookie"); v != "" {
		dialHeaders.Set("Cookie", v)
	}
	if v := origHeaders.Get("Authorization"); v != "" {
		dialHeaders.Set("Authorization", v)
	}

	// Extract subprotocols (e.g. vite-hmr) and pass via DialOptions, not headers
	var subprotocols []string
	if vals := origHeaders.Values("Sec-Websocket-Protocol"); len(vals) > 0 {
		subprotocols = vals
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()

	localConn, _, err := websocket.Dial(dialCtx, localURL, &websocket.DialOptions{
		HTTPHeader:   dialHeaders,
		Subprotocols: subprotocols,
	})
	if err != nil {
		log.Printf("ws: local dial error: %v", err)
		resp, _ := protocol.Wrap(protocol.TypeWSOpenResp, protocol.WSOpenRespMsg{
			ID: open.ID, OK: false, Error: err.Error(),
		})
		_ = c.writeMsg(ctx, tunnelConn, resp)
		return
	}

	localConn.SetReadLimit(16 << 20)

	// Store the connection
	c.wsMu.Lock()
	c.wsConns[open.ID] = localConn
	c.wsMu.Unlock()

	// Send success response
	resp, _ := protocol.Wrap(protocol.TypeWSOpenResp, protocol.WSOpenRespMsg{
		ID: open.ID, OK: true,
	})
	_ = c.writeMsg(ctx, tunnelConn, resp)

	log.Printf("ws: connected %s -> localhost:%d%s", open.ID, port, open.Path)

	// Read from local WebSocket and forward frames to tunnel
	go func() {
		defer func() {
			c.wsMu.Lock()
			delete(c.wsConns, open.ID)
			c.wsMu.Unlock()
			localConn.Close(websocket.StatusNormalClosure, "")

			closeMsg, _ := protocol.Wrap(protocol.TypeWSClose, protocol.WSCloseMsg{ID: open.ID})
			c.writeMsg(ctx, tunnelConn, closeMsg)
			log.Printf("ws: closed %s", open.ID)
		}()

		for {
			msgType, data, err := localConn.Read(ctx)
			if err != nil {
				return
			}

			frame, _ := protocol.Wrap(protocol.TypeWSFrame, protocol.WSFrameMsg{
				ID:     open.ID,
				IsText: msgType == websocket.MessageText,
				Data:   data,
			})
			if err := c.writeMsg(ctx, tunnelConn, frame); err != nil {
				return
			}
		}
	}()
}

func (c *Client) handleWSFrame(ctx context.Context, frame *protocol.WSFrameMsg) {
	c.wsMu.Lock()
	localConn, ok := c.wsConns[frame.ID]
	c.wsMu.Unlock()
	if !ok {
		return
	}

	msgType := websocket.MessageBinary
	if frame.IsText {
		msgType = websocket.MessageText
	}
	_ = localConn.Write(ctx, msgType, frame.Data)
}

func (c *Client) handleWSClose(closeMsg *protocol.WSCloseMsg) {
	c.wsMu.Lock()
	localConn, ok := c.wsConns[closeMsg.ID]
	if ok {
		delete(c.wsConns, closeMsg.ID)
	}
	c.wsMu.Unlock()
	if ok {
		localConn.Close(websocket.StatusNormalClosure, "")
		log.Printf("ws: closed %s (server initiated)", closeMsg.ID)
	}
}

func extractSubdomain(host, domain string) string {
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	suffix := "." + domain
	if strings.HasSuffix(host, suffix) {
		return strings.TrimSuffix(host, suffix)
	}
	if idx := strings.Index(host, "."); idx != -1 {
		return host[:idx]
	}
	return host
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
