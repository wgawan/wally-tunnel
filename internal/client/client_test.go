package client

import "testing"

func TestMapping_WSPort(t *testing.T) {
	tests := []struct {
		name string
		m    Mapping
		want int
	}{
		{
			name: "http only",
			m:    Mapping{HTTP: 3000},
			want: 3000,
		},
		{
			name: "separate ws port",
			m:    Mapping{HTTP: 3000, WS: 64999},
			want: 64999,
		},
		{
			name: "ws port zero falls back to http",
			m:    Mapping{HTTP: 5173, WS: 0},
			want: 5173,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.WSPort(); got != tt.want {
				t.Errorf("WSPort() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtractSubdomain(t *testing.T) {
	tests := []struct {
		host   string
		domain string
		want   string
	}{
		{"app.example.dev", "example.dev", "app"},
		{"api.example.dev", "example.dev", "api"},
		{"app.example.dev:443", "example.dev", "app"},
		{"example.dev", "example.dev", "example"},
		{"deep.sub.example.dev", "example.dev", "deep.sub"},
		{"other.com", "example.dev", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := extractSubdomain(tt.host, tt.domain)
			if got != tt.want {
				t.Errorf("extractSubdomain(%q, %q) = %q, want %q", tt.host, tt.domain, got, tt.want)
			}
		})
	}
}
