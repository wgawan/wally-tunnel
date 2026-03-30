package server

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/wgawan/wally-tunnel/internal/protocol"
)

func TestRequestToMsg(t *testing.T) {
	body := bytes.NewBufferString(`{"key":"value"}`)
	r := httptest.NewRequest("POST", "http://app.example.dev/api/data?foo=bar", body)
	r.Host = "app.example.dev"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer token123")

	msg, err := requestToMsg(r)
	if err != nil {
		t.Fatalf("requestToMsg() error: %v", err)
	}

	if msg.Method != "POST" {
		t.Errorf("Method = %q, want POST", msg.Method)
	}
	if msg.Path != "/api/data?foo=bar" {
		t.Errorf("Path = %q, want /api/data?foo=bar", msg.Path)
	}
	if msg.Host != "app.example.dev" {
		t.Errorf("Host = %q, want app.example.dev", msg.Host)
	}
	if string(msg.Body) != `{"key":"value"}` {
		t.Errorf("Body = %q, want {\"key\":\"value\"}", msg.Body)
	}
	if vals := msg.Headers["Content-Type"]; len(vals) == 0 || vals[0] != "application/json" {
		t.Error("Content-Type header not preserved")
	}
	if msg.ID == "" {
		t.Error("ID should be a non-empty UUID")
	}
}

func TestRequestToMsg_NoBody(t *testing.T) {
	r := httptest.NewRequest("GET", "http://app.example.dev/", nil)
	r.Host = "app.example.dev"

	msg, err := requestToMsg(r)
	if err != nil {
		t.Fatalf("requestToMsg() error: %v", err)
	}

	if msg.Method != "GET" {
		t.Errorf("Method = %q, want GET", msg.Method)
	}
	if msg.Path != "/" {
		t.Errorf("Path = %q, want /", msg.Path)
	}
	if len(msg.Body) != 0 {
		t.Errorf("Body should be empty, got %d bytes", len(msg.Body))
	}
}

func TestRequestToMsg_PathWithoutQuery(t *testing.T) {
	r := httptest.NewRequest("GET", "http://app.example.dev/static/main.js", nil)
	msg, err := requestToMsg(r)
	if err != nil {
		t.Fatalf("requestToMsg() error: %v", err)
	}
	if msg.Path != "/static/main.js" {
		t.Errorf("Path = %q, want /static/main.js", msg.Path)
	}
}

func TestWriteResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		headers    map[string][]string
		body       []byte
	}{
		{
			name:       "200 OK with JSON body",
			statusCode: 200,
			headers:    map[string][]string{"Content-Type": {"application/json"}},
			body:       []byte(`{"ok":true}`),
		},
		{
			name:       "404 not found",
			statusCode: 404,
			headers:    map[string][]string{"Content-Type": {"text/plain"}},
			body:       []byte("not found"),
		},
		{
			name:       "empty body",
			statusCode: 204,
			headers:    map[string][]string{},
			body:       nil,
		},
		{
			name:       "multiple header values",
			statusCode: 200,
			headers:    map[string][]string{"Set-Cookie": {"a=1", "b=2"}},
			body:       []byte("ok"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			resp := &protocol.HTTPRespMsg{
				StatusCode: tt.statusCode,
				Headers:    tt.headers,
				Body:       tt.body,
			}
			writeResponse(w, resp)

			if w.Code != tt.statusCode {
				t.Errorf("status = %d, want %d", w.Code, tt.statusCode)
			}
			if tt.body != nil && w.Body.String() != string(tt.body) {
				t.Errorf("body = %q, want %q", w.Body.String(), string(tt.body))
			}
			for k, vals := range tt.headers {
				got := w.Header().Values(k)
				if len(got) != len(vals) {
					t.Errorf("header %s: got %d values, want %d", k, len(got), len(vals))
				}
			}
		})
	}
}
