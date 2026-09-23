package hostagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The edge snapshot is what the gateway's Caddy and CoreDNS configuration is
// generated from: the set of base domains this server answers for, and the two
// addresses they are served on.
//
// It exists as a file rather than a query because the generator must not need
// the database. It runs at install, before Postgres exists, and during
// `server-upgrade`, while Postgres is restarting -- and the host agent has
// never held a database connection by design. The API writes a snapshot
// whenever the set changes; everything downstream renders from the last one on
// disk.

// EdgeDomain is one base domain, as the edge configuration needs it.
type EdgeDomain struct {
	BaseDomain string `json:"base_domain"`
	// InternalSubdomain is the label the mesh-only zone hangs off, "internal"
	// unless the domain says otherwise.
	InternalSubdomain string `json:"internal_subdomain"`
	// DNSMode is "delegation" or "ondemand". It decides how this domain's
	// certificates are obtained, and it is per domain: a gateway can serve one
	// of each.
	DNSMode string `json:"dns_mode"`
	// Primary marks the domain whose platform subdomains actually serve -
	// console, api, headscale. Exactly one is expected.
	Primary bool `json:"primary"`
	// ServesPlatform keeps the platform subdomains serving on a domain that is
	// no longer primary. When the primary moves, the old one's console, api
	// and headscale names keep answering: somebody is using that console right
	// now, and every worker joined the mesh through that headscale name. They
	// stop only when the domain is retired and removed.
	ServesPlatform bool `json:"serves_platform,omitempty"`
}

// Platform reports whether the platform's own hostnames are served on this
// domain: always on the primary, and on a former primary until it goes.
func (d EdgeDomain) Platform() bool { return d.Primary || d.ServesPlatform }

// DNS modes a domain can be arranged in. The strings match db.DNSMode; this
// package has no database dependency, so they are repeated rather than imported.
const (
	EdgeDNSDelegation = "delegation"
	EdgeDNSOnDemand   = "ondemand"
)

// EdgeSnapshot is the generator's only input.
type EdgeSnapshot struct {
	Domains []EdgeDomain `json:"domains"`
	// PublicIP is what the public zones answer with and what CoreDNS binds for
	// them. MeshIP is the gateway's address on the WireGuard mesh.
	PublicIP  string    `json:"public_ip"`
	MeshIP    string    `json:"mesh_ip"`
	WrittenAt time.Time `json:"written_at"`
	// MeshDomain is the MagicDNS domain nodes are named under, <node>.<mesh
	// domain>. Pinned to the install's domain rather than following the
	// primary: moving it would rename every node in the mesh for no gain, and
	// MagicDNS names are answered by the node itself, so the domain does not
	// have to stay registered to keep working.
	MeshDomain string `json:"mesh_domain,omitempty"`
}

// MeshDomainOrDefault is MeshDomain, or mesh.<primary> for a snapshot written
// before it was recorded.
func (s EdgeSnapshot) MeshDomainOrDefault() string {
	if s.MeshDomain != "" {
		return s.MeshDomain
	}
	if p := s.Primary(); p != nil {
		return "mesh." + p.BaseDomain
	}
	return ""
}

// Two snapshots, and the direction of writing is what separates them.
//
//   - EdgeDesiredFile is what the API wants served. It is in inbox/, the one
//     place the API can write, and leaving one there changes nothing by itself:
//     a domain.apply request is what asks for it to be put in service.
//   - EdgeSnapshotFile is what is actually installed, written by whoever
//     applied it. It is in state/, which the API mounts read-only, so the API
//     can see what the gateway is serving but cannot claim it served something
//     else.
//
// install.sh and `server-upgrade` read the installed one, because they run
// where no database is reachable and it is the record of what this gateway was
// last told to serve.
const (
	EdgeDesiredFile  = "inbox/edge.json"
	EdgeSnapshotFile = "state/edge.json"
)

// MaxEdgeSnapshotBytes bounds the file. A gateway has a handful of base
// domains; anything approaching this is not a snapshot.
const MaxEdgeSnapshotBytes = 256 << 10

// Primary returns the domain the platform's own hostnames are served on, or
// nil when the snapshot names none.
func (s EdgeSnapshot) Primary() *EdgeDomain {
	for i := range s.Domains {
		if s.Domains[i].Primary {
			return &s.Domains[i]
		}
	}
	return nil
}

// Validate rejects a snapshot that could not produce a working edge.
//
// The agent trusts nothing about a file the API wrote beyond its shape: a
// domain name that is really a Caddy directive, or a second primary, would be
// rendered straight into the configuration of the thing that serves this
// machine.
func (s EdgeSnapshot) Validate() error {
	if len(s.Domains) == 0 {
		return errors.New("edge snapshot names no domains")
	}
	if s.PublicIP == "" || s.MeshIP == "" {
		return errors.New("edge snapshot is missing the public or mesh address")
	}
	primaries := 0
	seen := map[string]bool{}
	for _, d := range s.Domains {
		if err := validEdgeName(d.BaseDomain); err != nil {
			return fmt.Errorf("base domain %q: %w", d.BaseDomain, err)
		}
		// A single label cannot be delegated and cannot hold a public
		// certificate, so it is not a base domain whatever else it is.
		if !strings.Contains(d.BaseDomain, ".") {
			return fmt.Errorf("base domain %q has no dot in it", d.BaseDomain)
		}
		if err := validEdgeName(d.InternalSubdomain); err != nil {
			return fmt.Errorf("internal subdomain %q: %w", d.InternalSubdomain, err)
		}
		if strings.Contains(d.InternalSubdomain, ".") {
			return fmt.Errorf("internal subdomain %q must be a single label", d.InternalSubdomain)
		}
		if d.DNSMode != EdgeDNSDelegation && d.DNSMode != EdgeDNSOnDemand {
			return fmt.Errorf("domain %s: unknown DNS mode %q", d.BaseDomain, d.DNSMode)
		}
		if seen[d.BaseDomain] {
			return fmt.Errorf("domain %s appears twice", d.BaseDomain)
		}
		seen[d.BaseDomain] = true
		if d.Primary {
			primaries++
		}
	}
	if primaries != 1 {
		return fmt.Errorf("edge snapshot names %d primary domains, want exactly 1", primaries)
	}
	if s.MeshDomain != "" {
		if err := validEdgeName(s.MeshDomain); err != nil {
			return fmt.Errorf("mesh domain %q: %w", s.MeshDomain, err)
		}
	}
	return nil
}

// validEdgeName allows only what a hostname may hold, label by label.
//
// Everything that reads a generated file - Caddy, CoreDNS, a zone parser -
// takes whitespace, braces, semicolons and newlines as syntax, so a name
// carrying any of them is refused rather than escaped. Escaping would mean
// knowing three quoting rules and getting all of them right; refusing needs
// only this, and no legitimate domain is turned away by it.
func validEdgeName(name string) error {
	if name == "" {
		return errors.New("is empty")
	}
	if len(name) > 253 {
		return errors.New("is too long")
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return errors.New("has an empty label")
		}
		if len(label) > 63 {
			return errors.New("has a label over 63 characters")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("has a label %q that starts or ends with a hyphen", label)
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			default:
				return fmt.Errorf("contains %q, which is not allowed in a hostname", r)
			}
		}
	}
	return nil
}

// Sorted returns the domains in the order they should be rendered: the primary
// first, then alphabetically.
//
// Generated output has to be stable, because the generator compares what it
// rendered with what is on disk and does nothing when they match. Ordering that
// followed the database's row order would rewrite the files, and reload Caddy,
// for no change at all.
func (s EdgeSnapshot) Sorted() []EdgeDomain {
	out := append([]EdgeDomain(nil), s.Domains...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Primary != out[j].Primary {
			return out[i].Primary
		}
		return out[i].BaseDomain < out[j].BaseDomain
	})
	return out
}

// ReadDesiredEdgeSnapshot loads what the API asked to be served, or reports
// os.ErrNotExist when it has asked for nothing.
func ReadDesiredEdgeSnapshot(hostDir string) (*EdgeSnapshot, error) {
	return readEdgeSnapshotAt(filepath.Join(hostDir, EdgeDesiredFile))
}

// WriteDesiredEdgeSnapshot records what should be served. Used by the API; it
// installs nothing on its own.
func WriteDesiredEdgeSnapshot(hostDir string, snap EdgeSnapshot) error {
	return writeEdgeSnapshotAt(filepath.Join(hostDir, EdgeDesiredFile), snap)
}

// ReadEdgeSnapshot loads the snapshot from a host directory. A missing file is
// reported as os.ErrNotExist, which callers treat as "nothing has written one
// yet" rather than as a failure.
func ReadEdgeSnapshot(hostDir string) (*EdgeSnapshot, error) {
	return readEdgeSnapshotAt(filepath.Join(hostDir, EdgeSnapshotFile))
}

func readEdgeSnapshotAt(path string) (*EdgeSnapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxEdgeSnapshotBytes {
		return nil, fmt.Errorf("edge snapshot is %d bytes, over the %d limit", info.Size(), MaxEdgeSnapshotBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap EdgeSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("edge snapshot is not readable: %w", err)
	}
	return &snap, nil
}

// WriteEdgeSnapshot replaces the snapshot, validating it first so an unusable
// one never reaches the file the generator reads. Written through a temporary
// file so a reader never sees half of one.
func WriteEdgeSnapshot(hostDir string, snap EdgeSnapshot) error {
	return writeEdgeSnapshotAt(filepath.Join(hostDir, EdgeSnapshotFile), snap)
}

func writeEdgeSnapshotAt(path string, snap EdgeSnapshot) error {
	if err := snap.Validate(); err != nil {
		return err
	}
	if snap.WrittenAt.IsZero() {
		snap.WrittenAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".edge-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
