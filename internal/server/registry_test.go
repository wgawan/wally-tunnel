package server

import (
	"sort"
	"testing"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	client := newTunnelClient(nil, map[string]int{"app": 5173, "api": 3000})

	active, taken := r.Register(client, map[string]int{"app": 5173, "api": 3000})

	if len(taken) != 0 {
		t.Errorf("expected no taken subdomains, got %v", taken)
	}
	if len(active) != 2 {
		t.Errorf("expected 2 active subdomains, got %d", len(active))
	}

	if got := r.Lookup("app"); got != client {
		t.Error("Lookup('app') should return the registered client")
	}
	if got := r.Lookup("api"); got != client {
		t.Error("Lookup('api') should return the registered client")
	}
	if got := r.Lookup("unknown"); got != nil {
		t.Error("Lookup('unknown') should return nil")
	}
}

func TestRegistry_RegisterConflict(t *testing.T) {
	r := NewRegistry()
	client1 := newTunnelClient(nil, map[string]int{"app": 5173})
	client2 := newTunnelClient(nil, map[string]int{"app": 3000, "api": 3000})

	r.Register(client1, map[string]int{"app": 5173})

	active, taken := r.Register(client2, map[string]int{"app": 3000, "api": 3000})

	if len(taken) != 1 || taken[0] != "app" {
		t.Errorf("expected taken=['app'], got %v", taken)
	}
	if len(active) != 1 || active[0] != "api" {
		t.Errorf("expected active=['api'], got %v", active)
	}

	// "app" should still belong to client1
	if got := r.Lookup("app"); got != client1 {
		t.Error("'app' should still belong to client1")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	client := newTunnelClient(nil, map[string]int{"app": 5173, "api": 3000})
	r.Register(client, map[string]int{"app": 5173, "api": 3000})

	r.Unregister(client)

	if got := r.Lookup("app"); got != nil {
		t.Error("Lookup('app') should return nil after unregister")
	}
	if got := r.Lookup("api"); got != nil {
		t.Error("Lookup('api') should return nil after unregister")
	}
}

func TestRegistry_UnregisterDoesNotAffectOthers(t *testing.T) {
	r := NewRegistry()
	client1 := newTunnelClient(nil, map[string]int{"app": 5173})
	client2 := newTunnelClient(nil, map[string]int{"api": 3000})

	r.Register(client1, map[string]int{"app": 5173})
	r.Register(client2, map[string]int{"api": 3000})

	r.Unregister(client1)

	if got := r.Lookup("app"); got != nil {
		t.Error("'app' should be gone")
	}
	if got := r.Lookup("api"); got != client2 {
		t.Error("'api' should still belong to client2")
	}
}

func TestRegistry_ActiveSubdomains(t *testing.T) {
	r := NewRegistry()
	client := newTunnelClient(nil, map[string]int{"app": 5173, "api": 3000})
	r.Register(client, map[string]int{"app": 5173, "api": 3000})

	subs := r.ActiveSubdomains()
	sort.Strings(subs)

	if len(subs) != 2 || subs[0] != "api" || subs[1] != "app" {
		t.Errorf("expected [api app], got %v", subs)
	}
}

func TestRegistry_ReregisterSameClient(t *testing.T) {
	r := NewRegistry()
	client := newTunnelClient(nil, map[string]int{"app": 5173})

	r.Register(client, map[string]int{"app": 5173})
	// Same client re-registering should not conflict
	active, taken := r.Register(client, map[string]int{"app": 5173})

	if len(taken) != 0 {
		t.Errorf("same client re-registering should not conflict, got taken=%v", taken)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active, got %d", len(active))
	}
}
