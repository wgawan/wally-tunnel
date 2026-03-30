package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wgawan/wally-tunnel/internal/client"
	"gopkg.in/yaml.v3"
)

func TestYAMLMappingUnmarshalLegacyPort(t *testing.T) {
	var got yamlMapping
	if err := yaml.Unmarshal([]byte("5173\n"), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.HTTP != 5173 {
		t.Fatalf("HTTP = %d, want 5173", got.HTTP)
	}
	if got.WS != 0 {
		t.Fatalf("WS = %d, want 0", got.WS)
	}
	if got.Protect.Enabled() {
		t.Fatal("expected no protection")
	}
}

func TestYAMLMappingUnmarshalProtection(t *testing.T) {
	var got yamlMapping
	data := []byte(`
http: 3000
ws: 64999
protect:
  basic_auth:
    username: demo
    password: secret
  expires_in: 2h
`)
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.HTTP != 3000 || got.WS != 64999 {
		t.Fatalf("ports = %+v, want http=3000 ws=64999", got.Mapping)
	}
	if got.Protect.BasicAuth == nil {
		t.Fatal("expected basic auth to be set")
	}
	if got.Protect.BasicAuth.Username != "demo" || got.Protect.BasicAuth.Password != "secret" {
		t.Fatalf("basic auth = %+v", got.Protect.BasicAuth)
	}
	if got.Protect.ExpiresIn != 2*time.Hour {
		t.Fatalf("expires_in = %s, want 2h", got.Protect.ExpiresIn)
	}
}

func TestLoadConfigExpandsEnvironmentVariables(t *testing.T) {
	t.Setenv("APP_DEMO_PASSWORD", "from-env")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
server: tunnel.example.dev
token: test-token
domain: tunnel.example.dev
mappings:
  app:
    http: 3000
    basic_auth:
      username: demo
      password: ${APP_DEMO_PASSWORD}
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := loadConfig(path)
	m := cfg.Mappings["app"].Mapping
	if m.Protect.BasicAuth == nil {
		t.Fatal("expected basic auth to be configured")
	}
	if m.Protect.BasicAuth.Password != "from-env" {
		t.Fatalf("password = %q, want from-env", m.Protect.BasicAuth.Password)
	}
}

func TestParseMappingsToYAMLLeavesProtectionEmpty(t *testing.T) {
	got := parseMappingsToYAML([]string{"app:3000"})
	m := got["app"].Mapping
	if m != (client.Mapping{HTTP: 3000}) {
		t.Fatalf("mapping = %+v, want http-only mapping", m)
	}
}
