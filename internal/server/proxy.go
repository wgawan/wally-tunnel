package server

import (
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/wgawan/wally-tunnel/internal/protocol"
)

// requestToMsg converts an incoming HTTP request into a protocol message.
func requestToMsg(r *http.Request) (*protocol.HTTPReqMsg, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, err
	}

	path := r.URL.Path
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}

	return &protocol.HTTPReqMsg{
		ID:      uuid.New().String(),
		Method:  r.Method,
		Path:    path,
		Host:    r.Host,
		Headers: r.Header,
		Body:    body,
	}, nil
}

// writeResponse writes an HTTPRespMsg back to the original HTTP caller.
func writeResponse(w http.ResponseWriter, resp *protocol.HTTPRespMsg) {
	for k, vals := range resp.Headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(resp.Body)
}
