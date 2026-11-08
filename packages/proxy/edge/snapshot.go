// Package edge is everything a proxy serves, as one snapshot: the API reads
// it from its database and hands it to a proxy that has none, on an edge
// whose brain is elsewhere. A proxy beside its database reads the parts
// itself, as before.
//
// The snapshot holds no secret: routes, TCP ports, who may open an internal
// route, brand names and which services were stopped. A proxy that serves it
// needs neither the database's address nor its encryption key.
package edge

import (
	"context"
	"net"
	"strings"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/proxy/cache"
	"github.com/meshploy/packages/proxy/meshgate"
	"github.com/meshploy/packages/proxy/tcp"
	"gorm.io/gorm"
)

// Versions are the edge versions a snapshot was read at (db.EdgeRoutes,
// db.EdgeMesh). A proxy sends back the ones it holds, and is answered only
// when the API's have moved.
type Versions struct {
	Routes int64 `json:"routes"`
	Mesh   int64 `json:"mesh"`
}

// Snapshot is what an edge serves.
type Snapshot struct {
	Versions Versions           `json:"versions"`
	Routes   cache.State        `json:"routes"`
	TCP      []db.TCPRoute      `json:"tcp"`
	Mesh     *meshgate.Snapshot `json:"mesh"`
	Pages    *Pages             `json:"pages"`
}

// ReadVersions is where the edge's parts stand in the database.
func ReadVersions(ctx context.Context, gdb *gorm.DB) (Versions, error) {
	conn := gdb.WithContext(ctx)
	r, err := db.EdgeVersionOf(conn, db.EdgeRoutes)
	if err != nil {
		return Versions{}, err
	}
	m, err := db.EdgeVersionOf(conn, db.EdgeMesh)
	if err != nil {
		return Versions{}, err
	}
	return Versions{Routes: r, Mesh: m}, nil
}

// Load reads a snapshot from the database. The versions are read first, so a
// change made while the parts are read moves them past what the snapshot
// says, and the proxy asks again.
func Load(ctx context.Context, gdb *gorm.DB) (*Snapshot, error) {
	v, err := ReadVersions(ctx, gdb)
	if err != nil {
		return nil, err
	}
	conn := gdb.WithContext(ctx)
	routes, err := cache.LoadState(conn)
	if err != nil {
		return nil, err
	}
	ports, err := tcp.LoadRoutes(conn)
	if err != nil {
		return nil, err
	}
	mesh, err := meshgate.Load(conn)
	if err != nil {
		return nil, err
	}
	pages, err := LoadPages(ctx, conn)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Versions: v, Routes: routes, TCP: ports, Mesh: mesh, Pages: pages}, nil
}

// Pages answers the edge's own pages from a snapshot.
type Pages struct {
	// Brands holds the name of each organisation whose brand is not
	// Meshploy's.
	Brands map[uuid.UUID]string `json:"brands,omitempty"`
	// Domains are the verified base domains and whose they are, for a host
	// with no route.
	Domains map[string]uuid.UUID `json:"domains,omitempty"`
	// Stopped are the services stopped on purpose.
	StoppedServices map[uuid.UUID]bool `json:"stopped,omitempty"`
}

// LoadPages reads what the edge's pages need from the database.
func LoadPages(ctx context.Context, gdb *gorm.DB) (*Pages, error) {
	p := &Pages{Brands: map[uuid.UUID]string{}, Domains: map[string]uuid.UUID{}, StoppedServices: map[uuid.UUID]bool{}}
	var orgs []uuid.UUID
	if err := gdb.Model(&db.Organization{}).Pluck("id", &orgs).Error; err != nil {
		return nil, err
	}
	for _, id := range orgs {
		if b := db.BrandOf(ctx, gdb, id); b.Custom {
			p.Brands[id] = b.Name
		}
	}
	var domains []db.Domain
	if err := gdb.Select("base_domain", "organization_id").Where("verified = ?", true).Find(&domains).Error; err != nil {
		return nil, err
	}
	for _, d := range domains {
		p.Domains[strings.ToLower(d.BaseDomain)] = d.OrganizationID
	}
	var stopped []uuid.UUID
	if err := gdb.Model(&db.Service{}).Where("status = ?", db.ServiceStopped).Pluck("id", &stopped).Error; err != nil {
		return nil, err
	}
	for _, id := range stopped {
		p.StoppedServices[id] = true
	}
	return p, nil
}

// BrandName is the brand of the route's organisation, or, for a host with
// none, of the organisation whose domain it sits under; empty is Meshploy's.
func (p *Pages) BrandName(_ context.Context, host string, orgID uuid.UUID) string {
	if p == nil {
		return ""
	}
	if orgID == uuid.Nil {
		orgID = p.orgOfHost(host)
	}
	return p.Brands[orgID]
}

// Stopped is whether a service was stopped on purpose.
func (p *Pages) Stopped(_ context.Context, serviceID uuid.UUID) bool {
	return p != nil && p.StoppedServices[serviceID]
}

// orgOfHost is whose verified domain host sits under, the longest winning, as
// db.BaseDomainFor decides it.
func (p *Pages) orgOfHost(host string) uuid.UUID {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	best, org := "", uuid.Nil
	for base, id := range p.Domains {
		if strings.HasSuffix(host, "."+base) && len(base) > len(best) {
			best, org = base, id
		}
	}
	return org
}
