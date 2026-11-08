package proxy

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	dbpkg "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/proxy/cache"
	"github.com/meshploy/packages/proxy/edge"
	"github.com/meshploy/packages/proxy/meshgate"
	"github.com/meshploy/packages/proxy/saved"
	"github.com/meshploy/packages/proxy/tcp"
)

// Main runs the proxy: the route cache, the TCP routes and the HTTP server,
// configured from the environment. The proxy image is this and nothing else;
// an edition that adds to the proxy calls it after registering what it adds.
//
// With PROXY_API_URL set the proxy has no database: it serves the snapshot
// the API hands the edge (package edge), as the node PROXY_NODE_ID with the
// secret in PROXY_NODE_SECRET_FILE. Otherwise it reads the database beside it.
func Main() {
	if os.Getenv("PROXY_API_URL") != "" {
		mainFromAPI()
		return
	}
	// ── Database ──────────────────────────────────────────────────────────────
	// Opened without reaching it: the edge serves what it saved while the
	// database is down, so it must start without one.
	database, err := dbpkg.FromEnvLazily()
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	// PROXY_STATE_DIR keeps the last routes, mesh grants and TCP ports loaded;
	// unset, a proxy that starts without the database serves nothing.
	keep, err := saved.Open(os.Getenv("PROXY_STATE_DIR"))
	if err != nil {
		log.Printf("saved state: %v; serving only what the database gives", err)
		keep = nil
	}

	encKey := os.Getenv("ENCRYPTION_KEY")
	if encKey != "" {
		dbpkg.SetEncryptionKey(encKey)
	}

	// ── Route cache ───────────────────────────────────────────────────────────
	routeCache := cache.New(database, 30*time.Second)
	routeCache.Keep(keep)
	routeCache.Start()

	// ── TCP routes ────────────────────────────────────────────────────────────
	// Each published port is a listener of its own, so they are opened here
	// rather than served by the HTTP handler below.
	forwarder := tcp.New(database, os.Getenv("TCP_BIND"), 30*time.Second)
	forwarder.Keep(keep)
	forwarder.Start()

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
	handler := NewHandler(routeCache)
	// Internal routes answer only the machines whose owners may use what they
	// lead to, once the mesh policy is enforced.
	mesh := meshgate.New(database)
	mesh.Keep(keep)
	mesh.Start(30 * time.Second)
	handler.CheckInternal(mesh)
	handler.Brand(database)
	serve(binds, port, handler)
}

// serve listens on every address in binds until one fails.
func serve(binds []string, port string, handler http.Handler) {
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

// mainFromAPI runs the proxy from the API's snapshot, with no database.
func mainFromAPI() {
	client, err := edgeClient(os.Getenv("PROXY_API_URL"), os.Getenv("PROXY_NODE_ID"), os.Getenv("PROXY_NODE_SECRET_FILE"))
	if err != nil {
		log.Fatalf("edge: %v", err)
	}
	keep, err := saved.Open(os.Getenv("PROXY_STATE_DIR"))
	if err != nil {
		log.Printf("saved state: %v; serving only what the API gives", err)
		keep = nil
	}

	handler, follower := fromAPI(client, keep, os.Getenv("TCP_BIND"))
	port := os.Getenv("PROXY_PORT")
	if port == "" {
		port = "8081"
	}
	go follower.Run(context.Background())
	log.Printf("proxy: serving the snapshot from %s, as node %s", client.Base, client.NodeID)
	serve(bindAddrs(os.Getenv("PROXY_BIND"), os.Getenv("MESH_IP")), port, handler)
}

// fromAPI is a proxy whose routes, ports, mesh gate and pages are the API's
// snapshot, served once the follower runs.
func fromAPI(client *edge.Client, keep *saved.Dir, tcpBind string) (*Handler, *edge.Follower) {
	routeCache := cache.New(nil, 0)
	routeCache.Keep(keep)
	forwarder := tcp.New(nil, tcpBind, 0)
	forwarder.Keep(keep)
	forwarder.Report(func(r dbpkg.TCPRoute, status dbpkg.TCPRouteStatus, lastErr string) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// Not reported is reported again: the next snapshot still holds the
			// old status, so this one is told again when it is served.
			if err := client.ReportTCP(ctx, []edge.TCPStatus{{ID: r.ID, Status: status, LastError: lastErr}}); err != nil {
				log.Printf("tcp: reporting :%d as %s: %v", r.GatewayPort, status, err)
			}
		}()
	})
	mesh := meshgate.New(nil)
	mesh.Keep(keep)
	pages := &edge.PagesHolder{}

	handler := NewHandler(routeCache)
	handler.CheckInternal(mesh)
	handler.AnswerPages(pages)

	follower := &edge.Follower{Client: client, Parts: edge.Parts{
		Serve: func(s *edge.Snapshot) {
			routeCache.Serve(s.Routes)
			forwarder.Serve(s.TCP)
			if s.Mesh != nil {
				mesh.Serve(s.Mesh)
			}
			pages.Set(s.Pages)
			if err := keep.Save(savedPages, s.Pages); err != nil {
				log.Printf("edge: saving the pages: %v", err)
			}
		},
		Restore: func() {
			routeCache.Restore()
			forwarder.Restore()
			mesh.Restore()
			var p edge.Pages
			if _, err := keep.Load(savedPages, &p); err == nil {
				pages.Set(&p)
			}
		},
		Close: forwarder.Stop,
	}}
	return handler, follower
}

// savedPages is the edge pages' file in the saved directory.
const savedPages = "pages"

// edgeClient is the edge's client for the API, from its environment.
func edgeClient(base, nodeID, secretFile string) (*edge.Client, error) {
	id, err := uuid.Parse(strings.TrimSpace(nodeID))
	if err != nil {
		return nil, errors.New("PROXY_NODE_ID is not a node ID")
	}
	if secretFile == "" {
		return nil, errors.New("PROXY_NODE_SECRET_FILE is not set")
	}
	raw, err := os.ReadFile(secretFile)
	if err != nil {
		return nil, err
	}
	secret := strings.TrimSpace(string(raw))
	if secret == "" {
		return nil, errors.New("the node secret file is empty")
	}
	return &edge.Client{Base: base, NodeID: id, Secret: secret}, nil
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
