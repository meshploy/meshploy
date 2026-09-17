// Package tcp publishes a port on the gateway and forwards it over the mesh.
//
// The HTTP path reads a hostname and picks a backend per request. A raw TCP
// connection carries nothing to read, so the port itself is the address: one
// listener per route, opened when the route appears and closed when it goes.
package tcp

import (
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// dialTimeout bounds reaching the target over the mesh. Short: the target is a
// NodePort on the same WireGuard network, and a client waiting on a backend
// that is not there learns nothing from waiting longer.
const dialTimeout = 10 * time.Second

type Forwarder struct {
	db      *gorm.DB
	bind    string // address to listen on; empty means every interface
	refresh time.Duration

	mu        sync.Mutex
	listeners map[int]*listener
}

func New(database *gorm.DB, bind string, refresh time.Duration) *Forwarder {
	return &Forwarder{
		db:        database,
		bind:      bind,
		refresh:   refresh,
		listeners: map[int]*listener{},
	}
}

func (f *Forwarder) Start() {
	f.sync()
	go func() {
		t := time.NewTicker(f.refresh)
		defer t.Stop()
		for range t.C {
			f.sync()
		}
	}()
}

// sync brings the open listeners in line with the stored routes: it opens what
// is new, closes what is gone, and updates the target of what stayed, since a
// deploy moves a service to a different NodePort.
func (f *Forwarder) sync() {
	var routes []db.TCPRoute
	if err := f.db.Find(&routes).Error; err != nil {
		log.Printf("tcp: load routes: %v", err)
		return
	}

	wanted := make(map[int]db.TCPRoute, len(routes))
	var paused []db.TCPRoute
	for _, r := range routes {
		if !r.Published {
			paused = append(paused, r) // its listener, if any, closes below
			continue
		}
		if r.TargetIP == "" || r.TargetPort == 0 {
			continue // unresolved: the API reports why, and there is nothing to forward to
		}
		wanted[r.GatewayPort] = r
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	for port, l := range f.listeners {
		if _, ok := wanted[port]; !ok {
			l.close()
			delete(f.listeners, port)
			log.Printf("tcp: closed :%d", port)
		}
	}
	for _, r := range paused {
		f.setStatus(r, db.TCPRoutePaused, "")
	}

	for port, route := range wanted {
		if l, ok := f.listeners[port]; ok {
			l.retarget(route)
			f.setStatus(route, db.TCPRouteOpen, "")
			continue
		}
		l, err := listen(f.bind, route)
		if err != nil {
			log.Printf("tcp: listen :%d: %v", port, err)
			f.setStatus(route, db.TCPRouteFailed, err.Error())
			continue
		}
		f.listeners[port] = l
		f.setStatus(route, db.TCPRouteOpen, "")
		log.Printf("tcp: listening on :%d → %s:%d", port, route.TargetIP, route.TargetPort)
	}
}

// setStatus records what the gateway managed to do, so a port it could not bind
// is visible in the console rather than only in this log. Written only on a
// change: the alternative is an update per route every refresh, for ever.
func (f *Forwarder) setStatus(route db.TCPRoute, status db.TCPRouteStatus, lastErr string) {
	if route.Status == status && route.LastError == lastErr {
		return
	}
	if err := f.db.Model(&db.TCPRoute{}).Where("id = ?", route.ID).
		Updates(map[string]any{"status": status, "last_error": lastErr}).Error; err != nil {
		log.Printf("tcp: record status for :%d: %v", route.GatewayPort, err)
	}
}

// ── One published port ────────────────────────────────────────────────────────

type listener struct {
	ln net.Listener

	mu         sync.RWMutex
	targetIP   string
	targetPort int
	allowed    []*net.IPNet
}

func listen(bind string, route db.TCPRoute) (*listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(route.GatewayPort)))
	if err != nil {
		return nil, err
	}
	l := &listener{ln: ln}
	l.retarget(route)
	go l.accept()
	return l, nil
}

func (l *listener) retarget(route db.TCPRoute) {
	allowed := make([]*net.IPNet, 0, len(route.AllowedCIDRs))
	for _, c := range route.AllowedCIDRs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			allowed = append(allowed, n)
		}
	}
	l.mu.Lock()
	l.targetIP, l.targetPort, l.allowed = route.TargetIP, route.TargetPort, allowed
	l.mu.Unlock()
}

func (l *listener) close() { _ = l.ln.Close() }

func (l *listener) accept() {
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			return // the listener was closed, or is no longer usable
		}
		go l.handle(conn)
	}
}

func (l *listener) handle(client net.Conn) {
	defer client.Close()

	l.mu.RLock()
	target := net.JoinHostPort(l.targetIP, strconv.Itoa(l.targetPort))
	allowed := l.allowed
	l.mu.RUnlock()

	if !permitted(client.RemoteAddr(), allowed) {
		return // closing without a byte is what a firewall would do
	}

	backend, err := net.DialTimeout("tcp", target, dialTimeout)
	if err != nil {
		log.Printf("tcp: dial %s: %v", target, err)
		return
	}
	defer backend.Close()

	splice(client, backend)
}

// permitted reports whether a connection may proceed. No allow-list means
// anyone, which on the gateway is the internet: the caller said so explicitly.
func permitted(addr net.Addr, allowed []*net.IPNet) bool {
	if len(allowed) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range allowed {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// splice copies in both directions until each side is done.
//
// Each direction ends with a half-close rather than tearing the whole
// connection down, because protocols that matter here rely on it: a Postgres
// client sends its terminate message and waits for the server to finish, and
// dropping the socket on the first EOF loses whatever was still coming back.
func splice(client, backend net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(backend, client)
		closeWrite(backend)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, backend)
		closeWrite(client)
	}()
	wg.Wait()
}

func closeWrite(c net.Conn) {
	type writeCloser interface{ CloseWrite() error }
	if wc, ok := c.(writeCloser); ok {
		_ = wc.CloseWrite()
		return
	}
	_ = c.Close()
}
