package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wgawan/wally-tunnel/internal/client"
	"gopkg.in/yaml.v3"
)

type mapFlag []string

func (m *mapFlag) String() string { return strings.Join(*m, ", ") }
func (m *mapFlag) Set(val string) error {
	*m = append(*m, val)
	return nil
}

// yamlMapping supports both "subdomain: port" and "subdomain: {http: port, ws: port}" in YAML.
type yamlMapping struct {
	client.Mapping
}

func (m *yamlMapping) UnmarshalYAML(value *yaml.Node) error {
	// Try as plain integer first (e.g., "app: 5173")
	var port int
	if value.Decode(&port) == nil {
		m.HTTP = port
		return nil
	}

	type protectYAML struct {
		BasicAuth *client.BasicAuth `yaml:"basic_auth"`
		ExpiresIn string            `yaml:"expires_in"`
	}

	// Try as object (e.g., "app: {http: 3000, ws: 64999}")
	var obj struct {
		HTTP      int               `yaml:"http"`
		WS        int               `yaml:"ws"`
		BasicAuth *client.BasicAuth `yaml:"basic_auth"`
		ExpiresIn string            `yaml:"expires_in"`
		Protect   protectYAML       `yaml:"protect"`
	}
	if err := value.Decode(&obj); err != nil {
		return fmt.Errorf("mapping must be a port number or {http: PORT, ws: PORT}")
	}
	if obj.HTTP == 0 {
		return fmt.Errorf("mapping object requires 'http' port")
	}
	basicAuth := obj.BasicAuth
	if basicAuth != nil && obj.Protect.BasicAuth != nil {
		return fmt.Errorf("mapping cannot define both basic_auth and protect.basic_auth")
	}
	if basicAuth == nil {
		basicAuth = obj.Protect.BasicAuth
	}

	expiresIn := obj.ExpiresIn
	if expiresIn != "" && obj.Protect.ExpiresIn != "" {
		return fmt.Errorf("mapping cannot define both expires_in and protect.expires_in")
	}
	if expiresIn == "" {
		expiresIn = obj.Protect.ExpiresIn
	}

	m.HTTP = obj.HTTP
	m.WS = obj.WS
	if basicAuth != nil {
		if basicAuth.Username == "" || basicAuth.Password == "" {
			return fmt.Errorf("basic_auth requires username and password")
		}
		m.Protect.BasicAuth = basicAuth
	}
	if expiresIn != "" {
		dur, err := time.ParseDuration(expiresIn)
		if err != nil {
			return fmt.Errorf("invalid expires_in duration %q: %w", expiresIn, err)
		}
		if dur <= 0 {
			return fmt.Errorf("expires_in must be greater than zero")
		}
		m.Protect.ExpiresIn = dur
	}
	return nil
}

type config struct {
	Server   string                 `yaml:"server"`
	Token    string                 `yaml:"token"`
	Domain   string                 `yaml:"domain"`
	Mappings map[string]yamlMapping `yaml:"mappings"`
}

func main() {
	var maps mapFlag
	serverAddr := flag.String("server", "", "tunnel server address (e.g., tunnel.yourdomain.dev)")
	token := flag.String("token", "", "authentication token")
	domain := flag.String("domain", "", "base domain (e.g., yourdomain.dev)")
	configFile := flag.String("config", "", "config file path (default: ~/.wally-tunnel.yaml)")
	flag.Var(&maps, "map", "subdomain:port mapping (can be repeated, e.g., -map rm:5173)")
	flag.Parse()

	cfg := loadConfig(*configFile)

	// CLI flags override config
	if *serverAddr != "" {
		cfg.Server = *serverAddr
	}
	if *token != "" {
		cfg.Token = *token
		log.Println("WARNING: token passed via -token flag is visible in process listings; prefer WALLY_TUNNEL_TOKEN env var or config file")
	}
	if *domain != "" {
		cfg.Domain = *domain
	}
	if len(maps) > 0 {
		cfg.Mappings = parseMappingsToYAML(maps)
	}

	// Also check env vars
	if cfg.Token == "" {
		cfg.Token = os.Getenv("WALLY_TUNNEL_TOKEN")
	}

	if cfg.Server == "" {
		log.Fatal("server address is required (-server or config file)")
	}
	if cfg.Token == "" {
		log.Fatal("token is required (-token, WALLY_TUNNEL_TOKEN, or config file)")
	}
	if len(cfg.Mappings) == 0 {
		log.Fatal("at least one mapping is required (-map subdomain:port or config file)")
	}

	// Ensure server URL has scheme
	serverURL := cfg.Server
	if !strings.HasPrefix(serverURL, "ws://") && !strings.HasPrefix(serverURL, "wss://") {
		serverURL = "wss://" + serverURL
	}

	// Convert yaml mappings to client mappings
	mappings := make(map[string]client.Mapping, len(cfg.Mappings))
	for sub, ym := range cfg.Mappings {
		mappings[sub] = ym.Mapping
	}

	c := &client.Client{
		ServerURL: serverURL,
		Token:     cfg.Token,
		Mappings:  mappings,
		Domain:    cfg.Domain,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		cancel()
	}()

	if err := c.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("tunnel error: %v", err)
	}
}

func loadConfig(path string) config {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return config{}
		}
		path = home + "/.wally-tunnel.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return config{}
	}

	data = []byte(os.ExpandEnv(string(data)))

	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Printf("warning: failed to parse config file %s: %v", path, err)
		return config{}
	}
	return cfg
}

func parseMappingsToYAML(maps []string) map[string]yamlMapping {
	result := make(map[string]yamlMapping)
	for _, m := range maps {
		parts := strings.SplitN(m, ":", 2)
		if len(parts) != 2 {
			log.Fatalf("invalid mapping %q: expected subdomain:port", m)
		}
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			log.Fatalf("invalid port in mapping %q: %v", m, err)
		}
		result[parts[0]] = yamlMapping{client.Mapping{HTTP: port}}
	}
	if len(result) == 0 {
		fmt.Println("Usage: wally-tunnel -server SERVER -token TOKEN -map SUBDOMAIN:PORT [-map ...]")
		os.Exit(1)
	}
	return result
}
