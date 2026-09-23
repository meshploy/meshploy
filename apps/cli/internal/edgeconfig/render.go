// Package edgeconfig renders the gateway's Caddy and CoreDNS configuration
// from an edge snapshot.
//
// It replaces a scheme where the configuration was a set of files shipped in
// the deploy tarball and edited in place: `caddy/Caddyfile` was both the
// delegation template and the live file, so selecting the on-demand variant
// destroyed the template and a backup copy existed to put it back. That cannot
// express two base domains arranged differently, because there was one file for
// the whole server.
//
// Here the files are output. Nothing is edited, so nothing needs restoring: the
// configuration is rendered again from the snapshot, which is the only input.
// An operator's own configuration lives in conf.d/*.caddy, which is imported
// and never written.
package edgeconfig

import (
	"bytes"
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
	"text/template"

	"github.com/meshploy/packages/hostagent"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// Files is what a render produced: a relative path under the install directory
// to the file's contents.
type Files map[string]string

// Paths returns the files in a stable order, for printing and diffing.
func (f Files) Paths() []string {
	out := make([]string, 0, len(f))
	for p := range f {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// SnapshotPath is named in every generated file's header, so somebody reading
// one on a server can find what produced it.
const SnapshotPath = "/var/lib/meshploy/host/" + hostagent.EdgeSnapshotFile

// zoneSerial is the SOA serial written into every generated zone.
//
// Fixed, not derived from the clock. CoreDNS re-reads these files on a timer
// and is the only consumer; nothing does a zone transfer, where the serial
// would decide whether a secondary refreshes. A timestamp would instead make
// every render differ from the last, which would rewrite the files and reload
// the edge on a change that is not one.
const zoneSerial = "2026040201"

type caddyData struct {
	SnapshotPath string
	PublicIP     string
	MeshIP       string
	Domains      []hostagent.EdgeDomain
	Primary      hostagent.EdgeDomain
	MeshDomain   string
}

// HeadscaleConfig is the one generated file that is not reloaded in place:
// Headscale reads its configuration at start, so a change to it means a
// restart. Named so the apply can tell.
const HeadscaleConfig = "headscale/config/config.yaml"

// zoneFile is one generated zone: the name it is served under, which is also
// its $ORIGIN and its filename, and the template that produces it.
type zoneFile struct {
	name string
	tmpl string
}

type zoneData struct {
	BaseDomain        string
	InternalSubdomain string
	PublicIP          string
	MeshIP            string
	Origin            string
	Serial            string
}

// Render produces every file the edge needs. It writes nothing: the caller
// decides what to compare, validate and install.
func Render(snap hostagent.EdgeSnapshot) (Files, error) {
	if err := snap.Validate(); err != nil {
		return nil, err
	}
	tmpl, err := template.ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		return nil, err
	}

	domains := snap.Sorted()
	out := Files{}

	data := caddyData{
		SnapshotPath: SnapshotPath, PublicIP: snap.PublicIP, MeshIP: snap.MeshIP,
		Domains: domains, Primary: *snap.Primary(), MeshDomain: snap.MeshDomainOrDefault(),
	}
	caddyfile, err := execute(tmpl, "Caddyfile.tmpl", data)
	if err != nil {
		return nil, err
	}
	out["caddy/Caddyfile"] = caddyfile

	corefile, err := execute(tmpl, "Corefile.tmpl", data)
	if err != nil {
		return nil, err
	}
	out["coredns/Corefile"] = corefile

	headscale, err := execute(tmpl, "headscale.yaml.tmpl", data)
	if err != nil {
		return nil, err
	}
	out[HeadscaleConfig] = headscale

	for _, d := range domains {
		internal := d.InternalSubdomain + "." + d.BaseDomain
		z := zoneData{
			BaseDomain:        d.BaseDomain,
			InternalSubdomain: d.InternalSubdomain,
			PublicIP:          snap.PublicIP,
			MeshIP:            snap.MeshIP,
			Serial:            zoneSerial,
		}
		// The mesh zone is served whatever the mode: Headscale pushes split DNS
		// for it to every node, so CoreDNS has to answer it on the mesh address
		// or no internal name resolves.
		zones := []zoneFile{{name: internal, tmpl: "zone_internal.tmpl"}}
		// The rest are delegation-only. On a domain whose DNS stays with the
		// operator's provider nothing delegates here, and nothing references
		// these files: the Caddyfile uses force_automate, on_demand and tls
		// internal, never a zone_file_path. Serving them anyway would make the
		// gateway authoritative for a domain it does not hold, so a resolver
		// pointed straight at it would answer differently from the operator's
		// real provider - a disagreement that is unpleasant to find. They are
		// not kept against a later mode switch either: that switch re-renders,
		// and they appear then.
		if d.DNSMode == hostagent.EdgeDNSDelegation {
			zones = append(zones,
				zoneFile{name: d.BaseDomain, tmpl: "zone_public.tmpl"},
				zoneFile{name: "_acme-challenge." + d.BaseDomain, tmpl: "zone_acme.tmpl"},
				zoneFile{name: "_acme-challenge." + internal, tmpl: "zone_acme.tmpl"},
			)
		}
		for _, zf := range zones {
			z.Origin = zf.name
			body, err := execute(tmpl, zf.tmpl, z)
			if err != nil {
				return nil, err
			}
			out[path.Join("coredns/zones", zf.name)] = body
		}
	}
	return out, nil
}

func execute(t *template.Template, name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	// Templates that branch leave ragged blank lines behind, and a file whose
	// whitespace wanders is a file whose diffs are unreadable. Collapsed here
	// rather than fought with {{- -}} in every branch.
	return tidy(buf.String()), nil
}

// tidy normalises generated whitespace: no trailing spaces, at most one blank
// line in a row, exactly one newline at the end.
func tidy(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			if blank || len(out) == 0 {
				continue
			}
			blank = true
			out = append(out, l)
			continue
		}
		blank = false
		out = append(out, l)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n") + "\n"
}
