package service

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/meshploy/packages/db"
)

// EdgeSubdomain is the gateway's edge name under its primary domain:
// edge.<primary>. A custom hostname is pointed at it with a CNAME, so the
// hostname follows the gateway when it moves to another machine, rather than
// an A record that pins it to this one's address. The primary's wildcard
// already resolves it, on a delegated domain and an on-demand one alike.
const EdgeSubdomain = "edge"

// Edge is the name a custom hostname points at, and whether pointing there
// works today.
type Edge struct {
	// Name is edge.<primary domain>; empty with no domain to build it from.
	Name string `json:"name"`
	// PublicIP is the gateway's public address, which an A record names.
	PublicIP string `json:"public_ip"`
	// Resolves is whether Name resolves to PublicIP in public DNS now. Until
	// it does, a CNAME to it would not reach this gateway, and the console
	// asks for the A record instead.
	Resolves bool `json:"resolves"`
}

// edgeCheckEvery is how long a lookup of the edge name is trusted: it changes
// only when the primary does or DNS is edited, and the console asks per page.
const edgeCheckEvery = time.Minute

// lookupEdge resolves the edge name; tests replace it.
var lookupEdge = net.DefaultResolver.LookupHost

var edgeChecked struct {
	sync.Mutex
	name, ip string
	ok       bool
	at       time.Time
}

// Edge is the gateway's edge name and whether it resolves here.
func (s *DomainService) Edge(ctx context.Context) Edge {
	out := Edge{PublicIP: s.publicIP}
	var d db.Domain
	if err := s.db.WithContext(ctx).Where("is_primary").Order("created_at ASC").First(&d).Error; err == nil {
		out.Name = EdgeSubdomain + "." + d.BaseDomain
	} else if s.installDomain != "" {
		out.Name = EdgeSubdomain + "." + s.installDomain
	}
	if out.PublicIP == "" {
		var gw db.Node
		if s.db.WithContext(ctx).Where("k3s_role = ?", db.K3sRoleServer).Order("created_at ASC").Limit(1).Find(&gw).Error == nil {
			out.PublicIP = gw.PublicIP
		}
	}
	if out.Name == "" || out.PublicIP == "" {
		return out
	}
	out.Resolves = edgeResolves(ctx, out.Name, out.PublicIP)
	return out
}

func edgeResolves(ctx context.Context, name, ip string) bool {
	edgeChecked.Lock()
	defer edgeChecked.Unlock()
	if edgeChecked.name == name && edgeChecked.ip == ip && time.Since(edgeChecked.at) < edgeCheckEvery {
		return edgeChecked.ok
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := lookupEdge(ctx, name)
	ok := false
	for _, a := range addrs {
		if a == ip {
			ok = true
			break
		}
	}
	if err == nil || ok {
		edgeChecked.name, edgeChecked.ip, edgeChecked.ok, edgeChecked.at = name, ip, ok, time.Now()
	}
	return ok
}
