package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wgawan/wally-tunnel/internal/protocol"
	"nhooyr.io/websocket"
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
	json.Unmarshal(env.Data, &authResp)
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
	json.Unmarshal(env.Data, &regResp)
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
		json.Unmarshal(env.Data, &req)

		// Send response back
		resp, _ := protocol.Wrap(protocol.TypeHTTPResp, protocol.HTTPRespMsg{
			ID:         req.ID,
			StatusCode: 200,
			Headers:    map[string][]string{"Content-Type": {"text/plain"}},
			Body:       []byte("hello from tunnel"),
		})
		conn.Write(ctx, websocket.MessageText, resp)
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
	conn.Write(ctx, websocket.MessageText, authMsg)

	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	env, _ := protocol.Unwrap(raw)
	var resp protocol.AuthRespMsg
	json.Unmarshal(env.Data, &resp)

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
		conn.Write(ctx, websocket.MessageText, authMsg)
		conn.Read(ctx) // consume auth resp
		return conn
	}

	// Client 1 registers "app"
	c1 := connectClient()
	defer c1.Close(websocket.StatusNormalClosure, "")

	reg1, _ := protocol.Wrap(protocol.TypeRegister, protocol.RegisterMsg{
		Subdomains: map[string]int{"app": 3000},
	})
	c1.Write(ctx, websocket.MessageText, reg1)
	_, raw, _ := c1.Read(ctx)
	env, _ := protocol.Unwrap(raw)
	var ack1 protocol.RegisterAckMsg
	json.Unmarshal(env.Data, &ack1)
	if len(ack1.Active) != 1 || ack1.Active[0] != "app" {
		t.Fatalf("client1 should own 'app', got active=%v", ack1.Active)
	}

	// Client 2 tries to register "app" (conflict) and "svc" (ok)
	c2 := connectClient()
	defer c2.Close(websocket.StatusNormalClosure, "")

	reg2, _ := protocol.Wrap(protocol.TypeRegister, protocol.RegisterMsg{
		Subdomains: map[string]int{"app": 4000, "svc": 5000},
	})
	c2.Write(ctx, websocket.MessageText, reg2)
	_, raw, _ = c2.Read(ctx)
	env, _ = protocol.Unwrap(raw)
	var ack2 protocol.RegisterAckMsg
	json.Unmarshal(env.Data, &ack2)

	if len(ack2.Active) != 1 || ack2.Active[0] != "svc" {
		t.Errorf("client2 should only get 'svc', got active=%v", ack2.Active)
	}
	if ack2.Error == "" {
		t.Error("should have error about 'app' being taken")
	}
}
