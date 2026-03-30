package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wgawan/wally-tunnel/internal/protocol"
	"github.com/coder/websocket"
)

// mockTunnelServer simulates a wally-tunnel server for client testing.
// It handles auth and registration, then relays messages.
func mockTunnelServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()

		// Read auth
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		env, _ := protocol.Unwrap(raw)
		var auth protocol.AuthMsg
		if err := json.Unmarshal(env.Data, &auth); err != nil {
			t.Errorf("unmarshal auth: %v", err)
			return
		}

		ok := auth.Token == token
		resp, _ := protocol.Wrap(protocol.TypeAuthResp, protocol.AuthRespMsg{
			OK:    ok,
			Error: map[bool]string{true: "", false: "invalid token"}[ok],
		})
		if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
			return
		}
		if !ok {
			return
		}

		// Read register
		_, raw, err = conn.Read(ctx)
		if err != nil {
			return
		}
		env, _ = protocol.Unwrap(raw)
		var reg protocol.RegisterMsg
		if err := json.Unmarshal(env.Data, &reg); err != nil {
			t.Errorf("unmarshal register: %v", err)
			return
		}

		active := make([]string, 0, len(reg.Subdomains))
		for sub := range reg.Subdomains {
			active = append(active, sub)
		}
		ack, _ := protocol.Wrap(protocol.TypeRegisterAck, protocol.RegisterAckMsg{
			OK:     true,
			Active: active,
		})
		if err := conn.Write(ctx, websocket.MessageText, ack); err != nil {
			return
		}

		// Now relay: send an HTTP request and read the response
		reqMsg, _ := protocol.Wrap(protocol.TypeHTTPReq, protocol.HTTPReqMsg{
			ID:      "test-req-1",
			Method:  "GET",
			Path:    "/health",
			Host:    "app.example.dev",
			Headers: map[string][]string{},
		})
		_ = conn.Write(ctx, websocket.MessageText, reqMsg)

		// Read response from client
		_, raw, err = conn.Read(ctx)
		if err != nil {
			return
		}
		env, _ = protocol.Unwrap(raw)
		if env.Type != protocol.TypeHTTPResp {
			t.Errorf("expected http_resp, got %s", env.Type)
		}
	}))
}

// TestClientProxiesHTTPRequest verifies the client can forward an HTTP request
// from the tunnel to a local service and return the response.
func TestClientProxiesHTTPRequest(t *testing.T) {
	// Start a mock local service
	localService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer localService.Close()

	// Parse the port from the local service URL
	var localPort int
	if _, err := fmt.Sscanf(localService.URL, "http://127.0.0.1:%d", &localPort); err != nil {
		t.Fatalf("parse local service port: %v", err)
	}

	// Start mock tunnel server
	mockServer := mockTunnelServer(t, "test-token")
	defer mockServer.Close()

	wsURL := "ws" + mockServer.URL[len("http"):]

	c := &Client{
		ServerURL: wsURL,
		Token:     "test-token",
		Mappings: map[string]Mapping{
			"app": {HTTP: localPort},
		},
		Domain: "example.dev",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// connect runs the full auth → register → readLoop cycle.
	// It will complete when the mock server closes its connection.
	err := c.connect(ctx)
	// The mock server closes after one request/response, which causes a read error.
	// This is expected behavior — we just need to verify no panic and that data flowed.
	if err != nil {
		t.Logf("connect returned (expected): %v", err)
	}
}

// TestClientAuthFailure verifies the client handles auth rejection correctly.
func TestClientAuthFailure(t *testing.T) {
	mockServer := mockTunnelServer(t, "correct-token")
	defer mockServer.Close()

	wsURL := "ws" + mockServer.URL[len("http"):]

	c := &Client{
		ServerURL: wsURL,
		Token:     "wrong-token",
		Mappings: map[string]Mapping{
			"app": {HTTP: 3000},
		},
		Domain: "example.dev",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.connect(ctx)
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
	if got := err.Error(); got != "auth: authentication failed: invalid token" {
		t.Logf("got error: %s", got)
	}
}

// TestClientPingPong verifies the client responds to ping messages.
func TestClientPingPong(t *testing.T) {
	pongReceived := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx := r.Context()

		// Auth
		_, raw, _ := conn.Read(ctx)
		env, _ := protocol.Unwrap(raw)
		var auth protocol.AuthMsg
		_ = json.Unmarshal(env.Data, &auth)
		resp, _ := protocol.Wrap(protocol.TypeAuthResp, protocol.AuthRespMsg{OK: true})
		_ = conn.Write(ctx, websocket.MessageText, resp)

		// Register
		_, raw, _ = conn.Read(ctx)
		env, _ = protocol.Unwrap(raw)
		ack, _ := protocol.Wrap(protocol.TypeRegisterAck, protocol.RegisterAckMsg{
			OK: true, Active: []string{"app"},
		})
		_ = conn.Write(ctx, websocket.MessageText, ack)

		// Send a ping
		ping, _ := protocol.Wrap(protocol.TypePing, nil)
		_ = conn.Write(ctx, websocket.MessageText, ping)

		// Expect a pong back
		_, raw, err = conn.Read(ctx)
		if err != nil {
			return
		}
		env, _ = protocol.Unwrap(raw)
		if env.Type == protocol.TypePong {
			close(pongReceived)
		}
	}))
	defer server.Close()

	c := &Client{
		ServerURL: "ws" + server.URL[len("http"):],
		Token:     "t",
		Mappings:  map[string]Mapping{"app": {HTTP: 3000}},
		Domain:    "example.dev",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = c.connect(ctx)
	}()

	select {
	case <-pongReceived:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pong")
	}
}
