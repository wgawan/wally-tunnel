package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/wgawan/wally-tunnel/internal/server"
)

func main() {
	listenAddr := flag.String("listen", ":8080", "listen address")
	flag.Parse()

	token := os.Getenv("WALLY_TUNNEL_TOKEN")
	if token == "" {
		log.Fatal("WALLY_TUNNEL_TOKEN environment variable is required")
	}

	domain := os.Getenv("WALLY_TUNNEL_DOMAIN")
	if domain == "" {
		log.Fatal("WALLY_TUNNEL_DOMAIN environment variable is required")
	}

	srv := server.New(token, domain)

	log.Printf("wally-tunnel-server listening on %s (domain: %s)", *listenAddr, domain)
	log.Fatal(http.ListenAndServe(*listenAddr, srv))
}
