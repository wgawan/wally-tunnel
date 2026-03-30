package client

import (
	"net/http"
	"testing"
	"time"
)

func TestIsStreamingResponse(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{"SSE stream", "text/event-stream", true},
		{"SSE with charset", "text/event-stream; charset=utf-8", true},
		{"JSON", "application/json", false},
		{"HTML", "text/html", false},
		{"empty", "", false},
		{"text/plain", "text/plain", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			if tt.contentType != "" {
				resp.Header.Set("Content-Type", tt.contentType)
			}
			if got := isStreamingResponse(resp); got != tt.want {
				t.Errorf("isStreamingResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		name string
		a, b int64 // using int64 to construct time.Duration
		want int64
	}{
		{"a smaller", 1, 5, 1},
		{"b smaller", 10, 3, 3},
		{"equal", 7, 7, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := min(time.Duration(tt.a), time.Duration(tt.b))
			if got != time.Duration(tt.want) {
				t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
