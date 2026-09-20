package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/meshploy/apps/proxy/internal/cache"
	"github.com/meshploy/apps/proxy/internal/proxy"
	"github.com/meshploy/apps/proxy/internal/tcp"
	dbpkg "github.com/meshploy/packages/db"
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
	// must not be published with them: PROXY_BIND keeps it off every interface.
	// It takes a comma-separated list, because one address is not enough: Caddy
	// reaches the proxy on loopback, while anything else on this host that is
	// not in the host's network namespace - a container, which during a
	// migration is the edge still holding 443 - cannot use loopback at all.
	// Such a caller reaches it on the node's mesh address, which containers on
	// this host can route to and the internet cannot.
	binds := bindAddrs(os.Getenv("PROXY_BIND"), os.Getenv("MESH_IP"))
	handler := proxy.NewHandler(routeCache)

	errs := make(chan error, len(binds))
	for _, b := range binds {
		addr := net.JoinHostPort(b, port)
		log.Printf("proxy listening on %s", addr)
		go func(addr string) { errs <- http.ListenAndServe(addr, handler) }(addr)
	}
	// One listener failing is fatal: a proxy answering on some of the addresses
	// it was told to serve is a half-working gateway, and silence about it is
	// worse than stopping.
	log.Fatalf("server: %v", <-errs)
}

// bindAddrs is where the proxy listens: what PROXY_BIND asks for, plus the
// node's mesh address when it is not already in the list. Duplicates and blank
// entries are dropped, and an empty PROXY_BIND means every interface - which
// already covers the mesh, so the mesh address is not added to it.
func bindAddrs(bind, meshIP string) []string {
	var out []string
	seen := map[string]bool{}
	everyInterface := false
	for _, b := range strings.Split(bind, ",") {
		b = strings.TrimSpace(b)
		if b == "" {
			everyInterface = true
			continue
		}
		if seen[b] {
			continue
		}
		seen[b] = true
		out = append(out, b)
	}
	if len(out) == 0 || everyInterface {
		return []string{""}
	}
	if meshIP = strings.TrimSpace(meshIP); meshIP != "" && !seen[meshIP] {
		out = append(out, meshIP)
	}
	return out
}
