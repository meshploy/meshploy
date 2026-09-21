package dokploy

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"path"
	"strings"
	"time"
)

// Carrying certificates from Traefik to Caddy.
//
// At cutover Meshploy's Caddy takes ports 80 and 443. Without this it would
// hold every moved domain with no certificate and ask Let's Encrypt for all of
// them at once - which on a server with seventy domains means rate limits and
// minutes of failed handshakes, in the one window where the operator is
// watching.
//
// So Traefik's acme.json is read and each usable certificate is written where
// Caddy looks for one. Anything that cannot be read, or expires too soon to be
// worth carrying, is simply left out: Caddy issues those itself, which is the
// normal path and always works.

// CaddyIssuer is the storage directory CertMagic uses for Let's Encrypt's
// production directory. A certificate written under a different issuer's
// directory is ignored, so this has to match what Caddy will look under.
const CaddyIssuer = "acme-v02.api.letsencrypt.org-directory"

// MinCertLife is how long a certificate must have left to be worth carrying.
// Below this Caddy would renew it almost immediately, so importing it only
// risks serving something stale in the first minutes after cutover.
const MinCertLife = 14 * 24 * time.Hour

// AcmeStore is Traefik's acme.json.
type AcmeStore map[string]struct {
	Certificates []AcmeCertificate `json:"Certificates"`
}

// AcmeCertificate is one stored certificate.
type AcmeCertificate struct {
	Domain struct {
		Main string   `json:"main"`
		SANs []string `json:"sans"`
	} `json:"domain"`
	// Certificate and Key are base64-encoded PEM.
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
}

// CertFile is one file to write into Caddy's storage.
type CertFile struct {
	// Path is relative to Caddy's data directory, e.g.
	// "certificates/<issuer>/example.com/example.com.crt".
	Path    string
	Content []byte
	// Mode is 0600 for a key, 0644 otherwise.
	Mode uint32
}

// CertImport is what the import found.
type CertImport struct {
	Files []CertFile
	// Imported and Skipped name domains, so the operator can see which will be
	// served at once and which Caddy will have to issue.
	Imported []string
	Skipped  map[string]string
}

// ImportCertificates turns Traefik's acme.json into files for Caddy's storage.
//
// wanted limits it to the domains that moved; a domain Meshploy does not serve
// has no business having its certificate copied. An empty set means all of them.
func ImportCertificates(acme []byte, wanted map[string]bool, now time.Time) (CertImport, error) {
	out := CertImport{Skipped: map[string]string{}}
	var store AcmeStore
	if err := json.Unmarshal(acme, &store); err != nil {
		return out, fmt.Errorf("read acme.json: %w", err)
	}

	for _, resolver := range store {
		for _, c := range resolver.Certificates {
			for _, name := range append([]string{c.Domain.Main}, c.Domain.SANs...) {
				if name == "" {
					continue
				}
				if len(wanted) > 0 && !wanted[name] {
					continue
				}
				if _, done := out.Skipped[name]; done {
					continue
				}
				files, reason := certFilesFor(name, c, now)
				if reason != "" {
					out.Skipped[name] = reason
					continue
				}
				out.Files = append(out.Files, files...)
				out.Imported = append(out.Imported, name)
			}
		}
	}
	return out, nil
}

// certFilesFor builds the three files CertMagic expects for one name, or says
// why this certificate is not worth carrying.
func certFilesFor(name string, c AcmeCertificate, now time.Time) ([]CertFile, string) {
	chain, err := base64.StdEncoding.DecodeString(c.Certificate)
	if err != nil || len(chain) == 0 {
		return nil, "its certificate could not be read"
	}
	key, err := base64.StdEncoding.DecodeString(c.Key)
	if err != nil || len(key) == 0 {
		return nil, "its private key could not be read"
	}
	leaf, err := leafOf(chain)
	if err != nil {
		return nil, err.Error()
	}
	if !leaf.NotAfter.After(now.Add(MinCertLife)) {
		return nil, fmt.Sprintf("it expires %s, too soon to carry", leaf.NotAfter.Format("2006-01-02"))
	}
	if err := leaf.VerifyHostname(name); err != nil {
		return nil, "it is not valid for this name"
	}

	dir := path.Join("certificates", CaddyIssuer, name)
	// The metadata Caddy keeps beside a certificate, plus where this one came
	// from.
	//
	// issuer_data is empty on purpose: Caddy caches the ACME order URL and the
	// renewal hint there, and this certificate was ordered by somebody else.
	// Renewal does not depend on it - Caddy renews on the certificate's own
	// expiry, with its own account, completing the challenge on the ports it
	// now holds. An empty one costs a full ACME order instead of a shortcut,
	// once, and the certificate is Caddy's own from then on. Nothing of the
	// old platform's account is carried, and nothing of it is needed.
	meta, err := json.Marshal(map[string]any{
		"sans":        append([]string{c.Domain.Main}, c.Domain.SANs...),
		"issuer_data": map[string]any{},
		"imported_by": "meshploy-migration",
		"imported_at": now.UTC().Format(time.RFC3339),
		"not_after":   leaf.NotAfter.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, "its metadata could not be written"
	}
	return []CertFile{
		{Path: path.Join(dir, name+".crt"), Content: chain, Mode: 0o644},
		{Path: path.Join(dir, name+".key"), Content: key, Mode: 0o600},
		{Path: path.Join(dir, name+".json"), Content: meta, Mode: 0o644},
	}, ""
}

// leafOf parses the first certificate of a PEM chain, which is the leaf.
func leafOf(chain []byte) (*x509.Certificate, error) {
	rest := chain
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, fmt.Errorf("its certificate held no PEM block")
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("its certificate could not be parsed")
		}
		return leaf, nil
	}
}

// WantedDomains is the set of hostnames a plan moves, for limiting the import.
func WantedDomains(plan Plan) map[string]bool {
	out := map[string]bool{}
	for _, it := range plan.Items {
		if it.Kind == "domain" && it.Verdict == Moves {
			out[splitHostPath(it.Name)] = true
		}
	}
	return out
}

// CaddyDataDir is where Meshploy's Caddy keeps its storage inside its volume.
// The certificates live under <volume>/caddy, which is CertMagic's default
// beneath XDG_DATA_HOME.
func CaddyDataDir(volumeMount string) string {
	return path.Join(strings.TrimRight(volumeMount, "/"), "caddy")
}
