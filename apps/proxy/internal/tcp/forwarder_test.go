package tcp

import (
	"io"
	"net"
	"testing"
	"time"

	db "github.com/meshploy/packages/db"
)

// echoBackend stands in for a NodePort on the mesh. It echoes what it is sent
// and then closes its side, which is what a half-close has to survive.
func echoBackend(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func routeTo(t *testing.T, backend net.Listener, allowed ...string) db.TCPRoute {
	t.Helper()
	host, portStr, err := net.SplitHostPort(backend.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}
	return db.TCPRoute{
		GatewayPort:  0, // the test opens the listener itself
		TargetIP:     host,
		TargetPort:   port,
		AllowedCIDRs: allowed,
	}
}

// A connection through a published port reaches the target and carries bytes
// both ways, with the client's half-close not cutting the reply short.
func TestForwardsBothWays(t *testing.T) {
	backend := echoBackend(t)
	l, err := listen("127.0.0.1", routeTo(t, backend))
	if err != nil {
		t.Fatal(err)
	}
	defer l.close()

	conn, err := net.Dial("tcp", l.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("select 1")); err != nil {
		t.Fatal(err)
	}
	// Say "that is all I have" — the echo has not been read yet, and dropping
	// the whole connection here is what loses a server's last reply.
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "select 1" {
		t.Errorf("got %q, want %q", got, "select 1")
	}
}

// An allow-list is the difference between a port for one operator and a port
// for the internet, so a connection from outside it gets nothing.
func TestAllowListRefusesOthers(t *testing.T) {
	backend := echoBackend(t)
	l, err := listen("127.0.0.1", routeTo(t, backend, "203.0.113.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.close()

	conn, err := net.Dial("tcp", l.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(make([]byte, 8))
	if err == nil {
		t.Fatalf("read %d bytes from a refused connection", n)
	}
}

func TestPermitted(t *testing.T) {
	_, mesh, _ := net.ParseCIDR("100.64.0.0/10")
	addr := &net.TCPAddr{IP: net.ParseIP("100.64.0.9"), Port: 5000}
	elsewhere := &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 5000}

	if !permitted(addr, nil) {
		t.Error("no allow-list means anyone")
	}
	if !permitted(addr, []*net.IPNet{mesh}) {
		t.Error("an address inside the range should be allowed")
	}
	if permitted(elsewhere, []*net.IPNet{mesh}) {
		t.Error("an address outside the range should not be")
	}
}
