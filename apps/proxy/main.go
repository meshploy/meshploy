package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"time"

	dbpkg "github.com/meshploy/packages/db"
	"github.com/meshploy/apps/proxy/internal/cache"
	"github.com/meshploy/apps/proxy/internal/proxy"
	"github.com/meshploy/apps/proxy/internal/tcp"
)

func main() {
	// ── Database ──────────────────────────────────────────────────────────────
	database, err := dbpkg.FromEnv()
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	encKey := os.Getenv("ENCRYPTION_KEY")
	if encKey != "" {
		dbpkg.SetEncryptionKey(encKey)
	}

	// ── Route cache ───────────────────────────────────────────────────────────
	routeCache := cache.New(database, 30*time.Second)
	routeCache.Start()

	// ── TCP routes ────────────────────────────────────────────────────────────
	// Each published port is a listener of its own, so they are opened here
	// rather than served by the HTTP handler below.
	tcp.New(database, os.Getenv("TCP_BIND"), 30*time.Second).Start()

	// ── HTTP server ───────────────────────────────────────────────────────────
	port := os.Getenv("PROXY_PORT")
	if port == "" {
		port = "8081"
	}
	// On host networking the proxy publishes TCP routes itself, so its own port
	// must not be published with them: PROXY_BIND keeps it on loopback, where
	// Caddy reaches it.
	addr := net.JoinHostPort(os.Getenv("PROXY_BIND"), port)

	handler := proxy.NewHandler(routeCache)

	log.Printf("proxy listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}
