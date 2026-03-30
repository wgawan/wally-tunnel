package protocol

import "encoding/json"

type MessageType string

const (
	TypeAuth         MessageType = "auth"
	TypeAuthResp     MessageType = "auth_resp"
	TypeRegister     MessageType = "register"
	TypeRegisterAck  MessageType = "register_ack"
	TypeHTTPReq      MessageType = "http_req"
	TypeHTTPResp     MessageType = "http_resp"
	TypeHTTPRespHead MessageType = "http_resp_head"
	TypeHTTPRespBody MessageType = "http_resp_body"
	TypeHTTPRespEnd  MessageType = "http_resp_end"
	TypeWSOpen       MessageType = "ws_open"
	TypeWSOpenResp   MessageType = "ws_open_resp"
	TypeWSFrame      MessageType = "ws_frame"
	TypeWSClose      MessageType = "ws_close"
	TypePing         MessageType = "ping"
	TypePong         MessageType = "pong"
)

// Envelope wraps all messages sent over the WebSocket.
type Envelope struct {
	Type MessageType     `json:"type"`
	Data json.RawMessage `json:"data"`
}

type AuthMsg struct {
	Token string `json:"token"`
}

type AuthRespMsg struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type BasicAuthConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type TunnelOptions struct {
	BasicAuth        *BasicAuthConfig `json:"basic_auth,omitempty"`
	ExpiresInSeconds int64            `json:"expires_in_seconds,omitempty"`
}

type RegisterMsg struct {
	Subdomains map[string]int           `json:"subdomains"`        // subdomain -> local port
	Options    map[string]TunnelOptions `json:"options,omitempty"` // subdomain -> edge protection options
}

type RegisterAckMsg struct {
	OK     bool     `json:"ok"`
	Active []string `json:"active"`
	Error  string   `json:"error,omitempty"`
}

type HTTPReqMsg struct {
	ID      string              `json:"id"`
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Host    string              `json:"host"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
}

type HTTPRespMsg struct {
	ID         string              `json:"id"`
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
}

// HTTPRespHeadMsg sends response headers for a streaming response.
type HTTPRespHeadMsg struct {
	ID         string              `json:"id"`
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
}

// HTTPRespBodyMsg sends a chunk of response body.
type HTTPRespBodyMsg struct {
	ID   string `json:"id"`
	Data []byte `json:"data"`
}

// HTTPRespEndMsg signals the end of a streaming response.
type HTTPRespEndMsg struct {
	ID string `json:"id"`
}

// WSOpenMsg tells the client to open a WebSocket to the local service.
type WSOpenMsg struct {
	ID      string              `json:"id"`
	Path    string              `json:"path"`
	Host    string              `json:"host"`
	Headers map[string][]string `json:"headers"`
}

// WSOpenRespMsg confirms the local WebSocket connection was established.
type WSOpenRespMsg struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// WSFrameMsg carries a single WebSocket frame in either direction.
type WSFrameMsg struct {
	ID     string `json:"id"`
	IsText bool   `json:"is_text"`
	Data   []byte `json:"data"`
}

// WSCloseMsg signals that a proxied WebSocket connection has closed.
type WSCloseMsg struct {
	ID string `json:"id"`
}

// Wrap marshals a message into an Envelope.
func Wrap(msgType MessageType, data any) ([]byte, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{Type: msgType, Data: raw})
}

// Unwrap unmarshals an Envelope from raw bytes.
func Unwrap(raw []byte) (Envelope, error) {
	var env Envelope
	err := json.Unmarshal(raw, &env)
	return env, err
}
