package server

import (
	"sort"
	"testing"
	"time"

	"github.com/wgawan/wally-tunnel/internal/protocol"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	client := newTunnelClient(nil, map[string]int{"app": 5173, "myapi": 3000})

	active, taken, invalid := r.Register(client, map[string]int{"app": 5173, "myapi": 3000}, nil)

	if len(taken) != 0 {
		t.Errorf("expected no taken subdomains, got %v", taken)
	}
	if len(invalid) != 0 {
		t.Errorf("expected no invalid subdomains, got %v", invalid)
	}
	if len(active) != 2 {
		t.Errorf("expected 2 active subdomains, got %d", len(active))
	}

	if got := r.Lookup("app"); got != client {
		t.Error("Lookup('app') should return the registered client")
	}
	if got := r.Lookup("myapi"); got != client {
		t.Error("Lookup('myapi') should return the registered client")
	}
	if got := r.Lookup("unknown"); got != nil {
		t.Error("Lookup('unknown') should return nil")
	}
}

func TestRegistry_RegisterConflict(t *testing.T) {
	r := NewRegistry()
	client1 := newTunnelClient(nil, map[string]int{"app": 5173})
	client2 := newTunnelClient(nil, map[string]int{"app": 3000, "other": 3000})

	r.Register(client1, map[string]int{"app": 5173}, nil)

	active, taken, _ := r.Register(client2, map[string]int{"app": 3000, "other": 3000}, nil)

	if len(taken) != 1 || taken[0] != "app" {
		t.Errorf("expected taken=['app'], got %v", taken)
	}
	if len(active) != 1 || active[0] != "other" {
		t.Errorf("expected active=['other'], got %v", active)
	}

	// "app" should still belong to client1
	if got := r.Lookup("app"); got != client1 {
		t.Error("'app' should still belong to client1")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	client := newTunnelClient(nil, map[string]int{"app": 5173, "svc": 3000})
	r.Register(client, map[string]int{"app": 5173, "svc": 3000}, nil)

	r.Unregister(client)

	if got := r.Lookup("app"); got != nil {
		t.Error("Lookup('app') should return nil after unregister")
	}
	if got := r.Lookup("svc"); got != nil {
		t.Error("Lookup('svc') should return nil after unregister")
	}
}

func TestRegistry_UnregisterDoesNotAffectOthers(t *testing.T) {
	r := NewRegistry()
	client1 := newTunnelClient(nil, map[string]int{"app": 5173})
	client2 := newTunnelClient(nil, map[string]int{"svc": 3000})

	r.Register(client1, map[string]int{"app": 5173}, nil)
	r.Register(client2, map[string]int{"svc": 3000}, nil)

	r.Unregister(client1)

	if got := r.Lookup("app"); got != nil {
		t.Error("'app' should be gone")
	}
	if got := r.Lookup("svc"); got != client2 {
		t.Error("'svc' should still belong to client2")
	}
}

func TestRegistry_ActiveSubdomains(t *testing.T) {
	r := NewRegistry()
	client := newTunnelClient(nil, map[string]int{"app": 5173, "svc": 3000})
	r.Register(client, map[string]int{"app": 5173, "svc": 3000}, nil)

	subs := r.ActiveSubdomains()
	sort.Strings(subs)

	if len(subs) != 2 || subs[0] != "app" || subs[1] != "svc" {
		t.Errorf("expected [app svc], got %v", subs)
	}
}

func TestRegistry_ReregisterSameClient(t *testing.T) {
	r := NewRegistry()
	client := newTunnelClient(nil, map[string]int{"app": 5173})

	r.Register(client, map[string]int{"app": 5173}, nil)
	// Same client re-registering should not conflict
	active, taken, _ := r.Register(client, map[string]int{"app": 5173}, nil)

	if len(taken) != 0 {
		t.Errorf("same client re-registering should not conflict, got taken=%v", taken)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active, got %d", len(active))
	}
}

func TestRegistry_RejectsReservedSubdomains(t *testing.T) {
	r := NewRegistry()
	client := newTunnelClient(nil, map[string]int{"www": 8080, "app": 8080})

	active, _, invalid := r.Register(client, map[string]int{"www": 8080, "app": 8080}, nil)

	if len(invalid) != 1 || invalid[0] != "www" {
		t.Errorf("expected invalid=['www'], got %v", invalid)
	}
	if len(active) != 1 || active[0] != "app" {
		t.Errorf("expected active=['app'], got %v", active)
	}
}

func TestRegistry_RejectsInvalidSubdomains(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		name string
		sub  string
	}{
		{"uppercase", "App"},
		{"starts with hyphen", "-app"},
		{"ends with hyphen", "app-"},
		{"contains dot", "my.app"},
		{"too long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, // 64 chars
		{"empty", ""},
		{"underscore prefix", "_tunnel"},
		{"spaces", "my app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTunnelClient(nil, map[string]int{tt.sub: 8080})
			active, _, invalid := r.Register(client, map[string]int{tt.sub: 8080}, nil)
			if len(invalid) != 1 {
				t.Errorf("expected %q to be invalid, got active=%v invalid=%v", tt.sub, active, invalid)
			}
		})
	}
}

func TestRegistry_AcceptsValidSubdomains(t *testing.T) {
	r := NewRegistry()

	valid := []string{"app", "my-app", "a", "test123", "a-b-c"}
	for _, sub := range valid {
		client := newTunnelClient(nil, map[string]int{sub: 8080})
		active, _, invalid := r.Register(client, map[string]int{sub: 8080}, nil)
		if len(invalid) != 0 {
			t.Errorf("expected %q to be valid, got invalid=%v", sub, invalid)
		}
		if len(active) != 1 {
			t.Errorf("expected %q to be active", sub)
		}
		r.Unregister(client)
	}
}

func TestRegistry_ResolveRemovesExpiredRoute(t *testing.T) {
	r := NewRegistry()
	client := newTunnelClient(nil, map[string]int{"app": 8080})

	r.Register(client, map[string]int{"app": 8080}, map[string]protocol.TunnelOptions{
		"app": {ExpiresInSeconds: 1},
	})

	time.Sleep(1100 * time.Millisecond)

	entry, expired := r.Resolve("app")
	if entry != nil {
		t.Fatal("expected expired route to be removed")
	}
	if !expired {
		t.Fatal("expected expired=true")
	}
	if got := r.Lookup("app"); got != nil {
		t.Fatal("expected lookup to return nil after expiry")
	}
}
