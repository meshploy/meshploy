package dokploy

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Carrying certificates from an edge that is not Traefik.
//
// acme.json is Traefik's shape and nobody else's. Everyone else keeps PEM on
// disk: certbot writes fullchain.pem and privkey.pem, nginx and apache are
// pointed at a pair of files by their config, HAProxy wants both in one file,
// and Caddy's own storage is already the layout Meshploy's Caddy reads.
//
// So the import also reads a directory. It does not read anybody's
// configuration to find out which certificate belongs to which site - the
// certificate says so itself, in its names - which is what makes this work for
// an edge nothing here understands.

// certDirCandidates are where a host usually keeps them. The first that exists
// is read; a directory the operator names beats all of them.
var certDirCandidates = []string{
	"/etc/letsencrypt/live",
	"/var/lib/caddy/.local/share/caddy/certificates",
	"/root/.local/share/caddy/certificates",
	"/etc/nginx/ssl",
	"/etc/ssl/private",
}

// FindCertificateDir is the first well-known certificate directory on this
// host, or empty. The system CA store is deliberately not among them: it is
// thousands of certificates nobody here has a key for.
func FindCertificateDir() string {
	for _, dir := range certDirCandidates {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
	}
	return ""
}

// maxCertFileBytes is a size past which a file is not a certificate. Keeps the
// walk from reading a database somebody left in /etc/ssl.
const maxCertFileBytes = 512 << 10

// ImportCertificateDir turns a directory of PEM certificates and keys into
// files for Caddy's storage, under the same rules as the acme.json path: only
// the domains that moved, only what is valid for the name it would serve, and
// only what has enough life left to be worth carrying.
//
// wanted limits it to the domains that moved. An empty set means all of them.
func ImportCertificateDir(dir string, wanted map[string]bool, now time.Time) (CertImport, error) {
	out := CertImport{Skipped: map[string]string{}}
	if dir == "" {
		return out, nil
	}
	pairs, err := certPairsIn(dir)
	if err != nil {
		return out, err
	}
	imported := map[string]bool{}
	for _, p := range pairs {
		leaf, err := leafOf(p.chain)
		if err != nil {
			continue
		}
		for _, name := range namesOf(leaf) {
			if len(wanted) > 0 && !wanted[name] {
				continue
			}
			if imported[name] {
				continue
			}
			if len(p.key) == 0 {
				keepSkip(out.Skipped, name, "its private key was not found beside it")
				continue
			}
			files, reason := certFiles(name, p.chain, p.key, namesOf(leaf), now)
			if reason != "" {
				keepSkip(out.Skipped, name, reason)
				continue
			}
			out.Files = append(out.Files, files...)
			out.Imported = append(out.Imported, name)
			imported[name] = true
			// A name that a worse copy was skipped for is not skipped after
			// all: two certificates for one host is ordinary on a server that
			// has been renewing for years.
			delete(out.Skipped, name)
		}
	}
	sort.Strings(out.Imported)
	return out, nil
}

// keepSkip records the first reason a name was passed over, so a later, worse
// copy does not rewrite a clearer explanation.
func keepSkip(skipped map[string]string, name, reason string) {
	if _, have := skipped[name]; !have {
		skipped[name] = reason
	}
}

// namesOf is every hostname a certificate is for.
func namesOf(leaf *x509.Certificate) []string {
	names := append([]string{}, leaf.DNSNames...)
	if len(names) == 0 && leaf.Subject.CommonName != "" {
		names = append(names, leaf.Subject.CommonName)
	}
	return names
}

// certPair is one certificate chain with the key that goes with it.
type certPair struct {
	path  string
	chain []byte
	key   []byte
}

// certPairsIn walks a directory for certificates and pairs each with its key.
//
// Three layouts, which between them are what hosts actually have:
//   - the key in the same file (HAProxy, and anyone who concatenated them),
//   - privkey.pem beside fullchain.pem or cert.pem (certbot, and Let's Encrypt's
//     own layout),
//   - <name>.key beside <name>.crt (nginx, apache, and Caddy's storage).
func certPairsIn(dir string) ([]certPair, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// A directory that cannot be read is not a reason to import
			// nothing: /etc/letsencrypt/archive is root-only on some hosts.
			return nil
		}
		if info.IsDir() || info.Size() == 0 || info.Size() > maxCertFileBytes {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".pem", ".crt", ".cert", ".cer":
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	var out []certPair
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(body), "BEGIN CERTIFICATE") {
			continue
		}
		out = append(out, certPair{path: path, chain: body, key: keyFor(path, body)})
	}
	return out, nil
}

// keyFor is the private key for a certificate file: inside it, or beside it.
func keyFor(path string, body []byte) []byte {
	if isPrivateKey(body) {
		return body
	}
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	candidates := []string{
		filepath.Join(dir, base+".key"),
		filepath.Join(dir, base+".pem.key"),
		filepath.Join(dir, "privkey.pem"),
		filepath.Join(dir, base+"-key.pem"),
		filepath.Join(dir, base+".key.pem"),
	}
	for _, c := range candidates {
		if c == path {
			continue
		}
		key, err := os.ReadFile(c)
		if err == nil && isPrivateKey(key) {
			return key
		}
	}
	return nil
}

// isPrivateKey reports whether these bytes hold a PEM private key, in any of
// the encodings openssl and friends produce.
func isPrivateKey(b []byte) bool {
	return strings.Contains(string(b), "PRIVATE KEY-----")
}
