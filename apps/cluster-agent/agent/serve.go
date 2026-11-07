package agent

import (
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"time"
)

// Gate lets through only the Meshploy API: a request from one of its mesh
// addresses, presenting the agent's token. An address proves which machine;
// the token proves which program on it.
type Gate struct {
	// From is the API's mesh addresses. Empty lets no one through.
	From []string
	// Token is what the API presents.
	Token string
	Next  http.Handler
	Logf  func(format string, args ...any)
}

func (g Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	from := false
	for _, a := range g.From {
		if a != "" && net.ParseIP(a).Equal(net.ParseIP(host)) {
			from = true
		}
	}
	auth := r.Header.Get("Authorization")
	presented, bearer := strings.CutPrefix(auth, "Bearer ")
	if !from || !bearer || !TokenMatches(presented, g.Token) {
		if g.Logf != nil {
			g.Logf("DENY  %s %s from %s: not the Meshploy API", r.Method, r.URL.Path, host)
		}
		http.Error(w, "not the Meshploy API", http.StatusUnauthorized)
		return
	}
	g.Next.ServeHTTP(w, r)
}

// Server is the agent's listener on the mesh. It speaks HTTP/1.1 only, so
// exec and port-forward can upgrade, over TLS with the agent's certificate.
func Server(addr string, cert tls.Certificate, h http.Handler) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: h,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
			NextProtos:   []string{"http/1.1"},
		},
		TLSNextProto:      map[string]func(*http.Server, *tls.Conn, http.Handler){},
		ReadHeaderTimeout: 10 * time.Second,
	}
}
