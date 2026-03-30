package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wgawan/wally-tunnel/internal/protocol"
)

func TestExtractSubdomain(t *testing.T) {
	s := New("token", "example.dev")

	tests := []struct {
		host string
		want string
	}{
		{"app.example.dev", "app"},
		{"api.example.dev", "api"},
		{"app.example.dev:443", "app"},
		{"example.dev", ""},
		{"other.com", ""},
		{"deep.sub.example.dev", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := s.extractSubdomain(tt.host)
			if got != tt.want {
				t.Errorf("extractSubdomain(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestIsWebSocketUpgrade(t *testing.T) {
	tests := []struct {
		name    string
		upgrade string
		want    bool
	}{
		{"websocket lowercase", "websocket", true},
		{"WebSocket mixed case", "WebSocket", true},
		{"WEBSOCKET uppercase", "WEBSOCKET", true},
		{"empty", "", false},
		{"other value", "h2c", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tt.upgrade != "" {
				r.Header.Set("Upgrade", tt.upgrade)
			}
			if got := isWebSocketUpgrade(r); got != tt.want {
				t.Errorf("isWebSocketUpgrade() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTernary(t *testing.T) {
	if got := ternary(true, "yes", "no"); got != "yes" {
		t.Errorf("ternary(true) = %q, want yes", got)
	}
	if got := ternary(false, "yes", "no"); got != "no" {
		t.Errorf("ternary(false) = %q, want no", got)
	}
}

func TestKeys(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	got := keys(m)
	if len(got) != 3 {
		t.Errorf("keys() returned %d items, want 3", len(got))
	}
	seen := make(map[string]bool)
	for _, k := range got {
		seen[k] = true
	}
	for k := range m {
		if !seen[k] {
			t.Errorf("keys() missing key %q", k)
		}
	}
}

func TestKeysEmpty(t *testing.T) {
	got := keys(map[string]int{})
	if len(got) != 0 {
		t.Errorf("keys(empty) returned %d items, want 0", len(got))
	}
}

func TestServeHTTP_TunnelCheck(t *testing.T) {
	s := New("token", "example.dev")
	client := newTunnelClient(nil, map[string]int{"app": 3000})
	s.registry.Register(client, map[string]int{"app": 3000}, nil)

	// Valid subdomain check (from localhost)
	r := httptest.NewRequest("GET", "/_tunnel/check?domain=app.example.dev", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("tunnel check valid subdomain: status = %d, want 200", w.Code)
	}

	// Invalid domain check (from localhost)
	r = httptest.NewRequest("GET", "/_tunnel/check?domain=other.com", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("tunnel check invalid domain: status = %d, want 404", w.Code)
	}

	// Bare domain (no subdomain) check (from localhost)
	r = httptest.NewRequest("GET", "/_tunnel/check?domain=example.dev", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("tunnel check bare domain: status = %d, want 404", w.Code)
	}

	// Tunnel server's own hostname should be approved
	r = httptest.NewRequest("GET", "/_tunnel/check?domain=tunnel.example.dev", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("tunnel check server hostname: status = %d, want 200", w.Code)
	}

	// Reject non-loopback requests
	r = httptest.NewRequest("GET", "/_tunnel/check?domain=app.example.dev", nil)
	r.RemoteAddr = "203.0.113.1:1234"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("tunnel check from external IP: status = %d, want 403", w.Code)
	}
}

func TestServeHTTP_BasicAuthRequired(t *testing.T) {
	s := New("token", "example.dev")
	client := newTunnelClient(nil, map[string]int{"app": 3000})
	s.registry.Register(client, map[string]int{"app": 3000}, map[string]protocol.TunnelOptions{
		"app": {
			BasicAuth: &protocol.BasicAuthConfig{Username: "demo", Password: "secret"},
		},
	})

	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "app.example.dev"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("missing WWW-Authenticate header")
	}
}

func TestServeHTTP_BasicAuthAllowsRequest(t *testing.T) {
	s := New("token", "example.dev")
	ts := httptest.NewServer(s)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + ts.URL[len("http"):] + "/_tunnel/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authMsg, _ := protocol.Wrap(protocol.TypeAuth, protocol.AuthMsg{Token: "token"})
	if err := conn.Write(ctx, websocket.MessageText, authMsg); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read auth resp: %v", err)
	}

	regMsg, _ := protocol.Wrap(protocol.TypeRegister, protocol.RegisterMsg{
		Subdomains: map[string]int{"app": 3000},
		Options: map[string]protocol.TunnelOptions{
			"app": {
				BasicAuth: &protocol.BasicAuthConfig{Username: "demo", Password: "secret"},
			},
		},
	})
	if err := conn.Write(ctx, websocket.MessageText, regMsg); err != nil {
		t.Fatalf("write register: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read register ack: %v", err)
	}

	proxied := make(chan string, 1)
	go func() {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		env, _ := protocol.Unwrap(raw)
		if env.Type != protocol.TypeHTTPReq {
			return
		}
		var req protocol.HTTPReqMsg
		if err := json.Unmarshal(env.Data, &req); err != nil {
			return
		}
		proxied <- req.Path
		resp, _ := protocol.Wrap(protocol.TypeHTTPResp, protocol.HTTPRespMsg{
			ID:         req.ID,
			StatusCode: 200,
			Headers:    map[string][]string{"Content-Type": {"text/plain"}},
			Body:       []byte("ok"),
		})
		_ = conn.Write(ctx, websocket.MessageText, resp)
	}()

	req, _ := http.NewRequest("GET", ts.URL+"/demo", nil)
	req.Host = "app.example.dev"
	req.SetBasicAuth("demo", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	select {
	case path := <-proxied:
		if path != "/demo" {
			t.Fatalf("proxied path = %q, want /demo", path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for proxied request")
	}
}

func TestServeHTTP_ExpiredTunnelReturnsGone(t *testing.T) {
	s := New("token", "example.dev")
	client := newTunnelClient(nil, map[string]int{"app": 3000})
	s.registry.Register(client, map[string]int{"app": 3000}, map[string]protocol.TunnelOptions{
		"app": {ExpiresInSeconds: 1},
	})

	time.Sleep(1100 * time.Millisecond)

	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "app.example.dev"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)

	if w.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410", w.Code)
	}
}

func TestServeHTTP_NoSubdomain(t *testing.T) {
	s := New("token", "example.dev")

	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "example.dev"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("no subdomain: status = %d, want 502", w.Code)
	}
}

func TestServeHTTP_UnknownSubdomain(t *testing.T) {
	s := New("token", "example.dev")

	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "unknown.example.dev"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)

	// No tunnel client registered, should get 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("unknown subdomain: status = %d, want 502", w.Code)
	}
}
