package protocol

import (
	"encoding/json"
	"testing"
)

func TestWrapUnwrap_RoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		msgType MessageType
		data    any
	}{
		{
			name:    "auth message",
			msgType: TypeAuth,
			data:    AuthMsg{Token: "secret-token"},
		},
		{
			name:    "auth response ok",
			msgType: TypeAuthResp,
			data:    AuthRespMsg{OK: true},
		},
		{
			name:    "auth response error",
			msgType: TypeAuthResp,
			data:    AuthRespMsg{OK: false, Error: "invalid token"},
		},
		{
			name:    "register message",
			msgType: TypeRegister,
			data: RegisterMsg{
				Subdomains: map[string]int{"app": 5173, "api": 3000},
				Options: map[string]TunnelOptions{
					"app": {
						BasicAuth:        &BasicAuthConfig{Username: "demo", Password: "secret"},
						ExpiresInSeconds: 3600,
					},
				},
			},
		},
		{
			name:    "register ack",
			msgType: TypeRegisterAck,
			data:    RegisterAckMsg{OK: true, Active: []string{"app", "api"}},
		},
		{
			name:    "http request",
			msgType: TypeHTTPReq,
			data: HTTPReqMsg{
				ID:      "req-123",
				Method:  "POST",
				Path:    "/api/data?key=val",
				Host:    "app.example.dev",
				Headers: map[string][]string{"Content-Type": {"application/json"}},
				Body:    []byte(`{"hello":"world"}`),
			},
		},
		{
			name:    "http response",
			msgType: TypeHTTPResp,
			data: HTTPRespMsg{
				ID:         "req-123",
				StatusCode: 200,
				Headers:    map[string][]string{"Content-Type": {"text/plain"}},
				Body:       []byte("OK"),
			},
		},
		{
			name:    "http resp head (streaming)",
			msgType: TypeHTTPRespHead,
			data: HTTPRespHeadMsg{
				ID:         "req-456",
				StatusCode: 200,
				Headers:    map[string][]string{"Content-Type": {"text/event-stream"}},
			},
		},
		{
			name:    "http resp body chunk",
			msgType: TypeHTTPRespBody,
			data:    HTTPRespBodyMsg{ID: "req-456", Data: []byte("data: hello\n\n")},
		},
		{
			name:    "http resp end",
			msgType: TypeHTTPRespEnd,
			data:    HTTPRespEndMsg{ID: "req-456"},
		},
		{
			name:    "ws open",
			msgType: TypeWSOpen,
			data: WSOpenMsg{
				ID:      "ws-1",
				Path:    "/ws",
				Host:    "app.example.dev",
				Headers: map[string][]string{"Sec-Websocket-Protocol": {"vite-hmr"}},
			},
		},
		{
			name:    "ws open response",
			msgType: TypeWSOpenResp,
			data:    WSOpenRespMsg{ID: "ws-1", OK: true},
		},
		{
			name:    "ws frame text",
			msgType: TypeWSFrame,
			data:    WSFrameMsg{ID: "ws-1", IsText: true, Data: []byte("hello")},
		},
		{
			name:    "ws frame binary",
			msgType: TypeWSFrame,
			data:    WSFrameMsg{ID: "ws-1", IsText: false, Data: []byte{0x00, 0x01, 0x02}},
		},
		{
			name:    "ws close",
			msgType: TypeWSClose,
			data:    WSCloseMsg{ID: "ws-1"},
		},
		{
			name:    "ping",
			msgType: TypePing,
			data:    nil,
		},
		{
			name:    "pong",
			msgType: TypePong,
			data:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := Wrap(tt.msgType, tt.data)
			if err != nil {
				t.Fatalf("Wrap() error: %v", err)
			}

			env, err := Unwrap(raw)
			if err != nil {
				t.Fatalf("Unwrap() error: %v", err)
			}

			if env.Type != tt.msgType {
				t.Errorf("Type = %q, want %q", env.Type, tt.msgType)
			}

			// Verify data round-trips correctly by re-marshaling the original and comparing
			if tt.data != nil {
				expectedJSON, _ := json.Marshal(tt.data)
				// Unmarshal both to interface{} for comparison
				var expected, actual any
				_ = json.Unmarshal(expectedJSON, &expected)
				_ = json.Unmarshal(env.Data, &actual)

				expectedNorm, _ := json.Marshal(expected)
				actualNorm, _ := json.Marshal(actual)
				if string(expectedNorm) != string(actualNorm) {
					t.Errorf("Data mismatch:\n  got:  %s\n  want: %s", actualNorm, expectedNorm)
				}
			}
		})
	}
}

func TestUnwrap_InvalidJSON(t *testing.T) {
	_, err := Unwrap([]byte("not json"))
	if err == nil {
		t.Error("Unwrap() should fail on invalid JSON")
	}
}

func TestWrap_EnvelopeStructure(t *testing.T) {
	raw, err := Wrap(TypePing, nil)
	if err != nil {
		t.Fatalf("Wrap() error: %v", err)
	}

	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if _, ok := env["type"]; !ok {
		t.Error("envelope missing 'type' field")
	}
	if _, ok := env["data"]; !ok {
		t.Error("envelope missing 'data' field")
	}
}
