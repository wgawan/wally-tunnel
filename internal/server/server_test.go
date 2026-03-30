package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
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

	// Reject non-loopback requests
	r = httptest.NewRequest("GET", "/_tunnel/check?domain=app.example.dev", nil)
	r.RemoteAddr = "203.0.113.1:1234"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("tunnel check from external IP: status = %d, want 403", w.Code)
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
