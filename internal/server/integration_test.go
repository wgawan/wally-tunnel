package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wgawan/wally-tunnel/internal/protocol"
	"github.com/coder/websocket"
)

// TestFullHTTPProxy exercises the complete HTTP proxy path:
// browser → server → tunnel client → local service → back.
func TestFullHTTPProxy(t *testing.T) {
	s := New("test-token", "example.dev")
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Connect a tunnel client via WebSocket
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + ts.URL[len("http"):] + "/_tunnel/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Authenticate
	authMsg, _ := protocol.Wrap(protocol.TypeAuth, protocol.AuthMsg{Token: "test-token"})
	if err := conn.Write(ctx, websocket.MessageText, authMsg); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read auth resp: %v", err)
	}
	env, _ := protocol.Unwrap(raw)
	var authResp protocol.AuthRespMsg
	if err := json.Unmarshal(env.Data, &authResp); err != nil {
		t.Fatalf("unmarshal auth resp: %v", err)
	}
	if !authResp.OK {
		t.Fatalf("auth failed: %s", authResp.Error)
	}

	// Register subdomain "app"
	regMsg, _ := protocol.Wrap(protocol.TypeRegister, protocol.RegisterMsg{
		Subdomains: map[string]int{"app": 3000},
	})
	if err := conn.Write(ctx, websocket.MessageText, regMsg); err != nil {
		t.Fatalf("write register: %v", err)
	}
	_, raw, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("read register ack: %v", err)
	}
	env, _ = protocol.Unwrap(raw)
	var regResp protocol.RegisterAckMsg
	if err := json.Unmarshal(env.Data, &regResp); err != nil {
		t.Fatalf("unmarshal register ack: %v", err)
	}
	if len(regResp.Active) == 0 {
		t.Fatalf("no subdomains registered: %s", regResp.Error)
	}

	// Start a goroutine to handle one proxied request
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, raw, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("read proxied request: %v", err)
			return
		}
		env, err := protocol.Unwrap(raw)
		if err != nil {
			t.Errorf("unwrap proxied request: %v", err)
			return
		}
		if env.Type != protocol.TypeHTTPReq {
			t.Errorf("expected http_req, got %s", env.Type)
			return
		}
		var req protocol.HTTPReqMsg
		if err := json.Unmarshal(env.Data, &req); err != nil {
			t.Errorf("unmarshal http req: %v", err)
			return
		}

		// Send response back
		resp, _ := protocol.Wrap(protocol.TypeHTTPResp, protocol.HTTPRespMsg{
			ID:         req.ID,
			StatusCode: 200,
			Headers:    map[string][]string{"Content-Type": {"text/plain"}},
			Body:       []byte("hello from tunnel"),
		})
		if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
			t.Errorf("write http resp: %v", err)
		}
	}()

	// Make an HTTP request to the server as if we're a browser hitting app.example.dev
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", ts.URL+"/hello", nil)
	req.Host = "app.example.dev"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	<-done
}

// TestAuthBadToken verifies that a bad token is rejected.
func TestAuthBadToken(t *testing.T) {
	s := New("correct-token", "example.dev")
	ts := httptest.NewServer(s)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + ts.URL[len("http"):] + "/_tunnel/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	authMsg, _ := protocol.Wrap(protocol.TypeAuth, protocol.AuthMsg{Token: "wrong-token"})
	if err := conn.Write(ctx, websocket.MessageText, authMsg); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	env, _ := protocol.Unwrap(raw)
	var resp protocol.AuthRespMsg
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("unmarshal auth resp: %v", err)
	}

	if resp.OK {
		t.Error("auth should have failed with wrong token")
	}
	if resp.Error == "" {
		t.Error("error message should be non-empty")
	}
}

// TestRegisterConflictViaTunnel verifies subdomain conflicts through the full tunnel path.
func TestRegisterConflictViaTunnel(t *testing.T) {
	s := New("token", "example.dev")
	ts := httptest.NewServer(s)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Helper to connect and authenticate
	connectClient := func() *websocket.Conn {
		wsURL := "ws" + ts.URL[len("http"):] + "/_tunnel/ws"
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		authMsg, _ := protocol.Wrap(protocol.TypeAuth, protocol.AuthMsg{Token: "token"})
		if err := conn.Write(ctx, websocket.MessageText, authMsg); err != nil {
			t.Fatalf("write auth: %v", err)
		}
		if _, _, err := conn.Read(ctx); err != nil { // consume auth resp
			t.Fatalf("read auth resp: %v", err)
		}
		return conn
	}

	// Client 1 registers "app"
	c1 := connectClient()
	defer c1.Close(websocket.StatusNormalClosure, "")

	reg1, _ := protocol.Wrap(protocol.TypeRegister, protocol.RegisterMsg{
		Subdomains: map[string]int{"app": 3000},
	})
	if err := c1.Write(ctx, websocket.MessageText, reg1); err != nil {
		t.Fatalf("write reg1: %v", err)
	}
	_, raw, err := c1.Read(ctx)
	if err != nil {
		t.Fatalf("read reg1 ack: %v", err)
	}
	env, _ := protocol.Unwrap(raw)
	var ack1 protocol.RegisterAckMsg
	if err := json.Unmarshal(env.Data, &ack1); err != nil {
		t.Fatalf("unmarshal ack1: %v", err)
	}
	if len(ack1.Active) != 1 || ack1.Active[0] != "app" {
		t.Fatalf("client1 should own 'app', got active=%v", ack1.Active)
	}

	// Client 2 tries to register "app" (conflict) and "svc" (ok)
	c2 := connectClient()
	defer c2.Close(websocket.StatusNormalClosure, "")

	reg2, _ := protocol.Wrap(protocol.TypeRegister, protocol.RegisterMsg{
		Subdomains: map[string]int{"app": 4000, "svc": 5000},
	})
	if err := c2.Write(ctx, websocket.MessageText, reg2); err != nil {
		t.Fatalf("write reg2: %v", err)
	}
	_, raw, err = c2.Read(ctx)
	if err != nil {
		t.Fatalf("read reg2 ack: %v", err)
	}
	env, _ = protocol.Unwrap(raw)
	var ack2 protocol.RegisterAckMsg
	if err := json.Unmarshal(env.Data, &ack2); err != nil {
		t.Fatalf("unmarshal ack2: %v", err)
	}

	if len(ack2.Active) != 1 || ack2.Active[0] != "svc" {
		t.Errorf("client2 should only get 'svc', got active=%v", ack2.Active)
	}
	if ack2.Error == "" {
		t.Error("should have error about 'app' being taken")
	}
}
