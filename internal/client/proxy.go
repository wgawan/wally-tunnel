package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wgawan/wally-tunnel/internal/protocol"
	"nhooyr.io/websocket"
)

// writeFunc serializes a message over the tunnel WebSocket.
type writeFunc func(ctx context.Context, conn *websocket.Conn, data []byte) error

var httpClient = &http.Client{
	// No timeout — streaming responses (SSE) can last indefinitely
}

func isStreamingResponse(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "text/event-stream")
}

// checkAndForward makes the local HTTP request and sends the response back through
// the tunnel. Automatically detects streaming responses (SSE) and handles them.
func checkAndForward(ctx context.Context, tunnelConn *websocket.Conn, req *protocol.HTTPReqMsg, localPort int, write writeFunc) error {
	url := fmt.Sprintf("http://localhost:%d%s", localPort, req.Path)

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, bytes.NewReader(req.Body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	for k, vals := range req.Headers {
		for _, v := range vals {
			httpReq.Header.Add(k, v)
		}
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		errMsg, _ := protocol.Wrap(protocol.TypeHTTPResp, protocol.HTTPRespMsg{
			ID:         req.ID,
			StatusCode: http.StatusBadGateway,
			Headers:    map[string][]string{"Content-Type": {"text/plain"}},
			Body:       []byte(fmt.Sprintf("Local service error: %v", err)),
		})
		return write(ctx, tunnelConn, errMsg)
	}
	defer resp.Body.Close()

	if isStreamingResponse(resp) {
		return streamResponse(ctx, tunnelConn, req.ID, resp, write)
	}

	// Normal response — read full body and send as single message
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	msg, _ := protocol.Wrap(protocol.TypeHTTPResp, protocol.HTTPRespMsg{
		ID:         req.ID,
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       body,
	})
	return write(ctx, tunnelConn, msg)
}

// streamResponse sends headers first, then streams body chunks for SSE/streaming responses.
func streamResponse(ctx context.Context, tunnelConn *websocket.Conn, reqID string, resp *http.Response, write writeFunc) error {
	// Send headers
	head, _ := protocol.Wrap(protocol.TypeHTTPRespHead, protocol.HTTPRespHeadMsg{
		ID:         reqID,
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
	})
	if err := write(ctx, tunnelConn, head); err != nil {
		return err
	}

	// Stream body chunks
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			chunk, _ := protocol.Wrap(protocol.TypeHTTPRespBody, protocol.HTTPRespBodyMsg{
				ID:   reqID,
				Data: buf[:n],
			})
			if err := write(ctx, tunnelConn, chunk); err != nil {
				return err
			}
		}
		if readErr != nil {
			break
		}
	}

	// Signal end of stream
	end, _ := protocol.Wrap(protocol.TypeHTTPRespEnd, protocol.HTTPRespEndMsg{ID: reqID})
	return write(ctx, tunnelConn, end)
}
