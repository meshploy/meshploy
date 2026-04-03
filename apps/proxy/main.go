package main

import (
	"log"
	"net/http"
	"os"
	"time"

	dbpkg "github.com/meshploy/packages/db"
	"github.com/meshploy/apps/proxy/internal/cache"
	"github.com/meshploy/apps/proxy/internal/proxy"
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

	// ── HTTP server ───────────────────────────────────────────────────────────
	port := os.Getenv("PROXY_PORT")
	if port == "" {
		port = "8081"
	}

	handler := proxy.NewHandler(routeCache)

	log.Printf("proxy listening on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}
