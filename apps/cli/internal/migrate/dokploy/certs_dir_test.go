package dokploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// write puts a file in a directory, making the directory on the way.
func write(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func imported(imp CertImport) string {
	return strings.Join(imp.Imported, ",")
}

// The three layouts a host actually has, read from one directory.
func TestImportCertificateDirReadsEachLayout(t *testing.T) {
	now := time.Date(2026, time.September, 21, 0, 0, 0, 0, time.UTC)
	good := now.Add(60 * 24 * time.Hour)
	dir := t.TempDir()

	// certbot: fullchain.pem and privkey.pem in a per-name directory.
	crt, key := issue(t, "shop.example.com", good)
	write(t, filepath.Join(dir, "shop.example.com", "fullchain.pem"), crt)
	write(t, filepath.Join(dir, "shop.example.com", "privkey.pem"), key)

	// nginx and Caddy's storage: <name>.crt and <name>.key.
	crt, key = issue(t, "api.example.com", good)
	write(t, filepath.Join(dir, "api.example.com.crt"), crt)
	write(t, filepath.Join(dir, "api.example.com.key"), key)

	// HAProxy: both in one file.
	crt, key = issue(t, "blog.example.com", good)
	write(t, filepath.Join(dir, "blog.example.com.pem"), append(append([]byte{}, crt...), key...))

	imp, err := ImportCertificateDir(dir, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := imported(imp); got != "api.example.com,blog.example.com,shop.example.com" {
		t.Fatalf("imported %q, skipped %v", got, imp.Skipped)
	}
	// Three files per name, as CertMagic wants them.
	if len(imp.Files) != 9 {
		t.Fatalf("got %d files", len(imp.Files))
	}
	for _, f := range imp.Files {
		if !strings.HasPrefix(f.Path, "certificates/"+CaddyIssuer+"/") {
			t.Fatalf("written somewhere Caddy will not look: %s", f.Path)
		}
		if strings.HasSuffix(f.Path, ".key") && f.Mode != 0o600 {
			t.Fatalf("a key written readable: %s %o", f.Path, f.Mode)
		}
	}
}

// Only the domains that moved: a certificate for a site Meshploy does not serve
// has no business being copied.
func TestImportCertificateDirTakesOnlyWhatMoved(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	for _, name := range []string{"moved.example.com", "stayed.example.com"} {
		crt, key := issue(t, name, now.Add(60*24*time.Hour))
		write(t, filepath.Join(dir, name+".crt"), crt)
		write(t, filepath.Join(dir, name+".key"), key)
	}
	imp, err := ImportCertificateDir(dir, map[string]bool{"moved.example.com": true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := imported(imp); got != "moved.example.com" {
		t.Fatalf("imported %q", got)
	}
}

// A certificate about to expire is left for Caddy, and one with no key beside
// it is named rather than passed over silently.
func TestImportCertificateDirSaysWhyItPassedSomethingOver(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()

	crt, key := issue(t, "expiring.example.com", now.Add(3*24*time.Hour))
	write(t, filepath.Join(dir, "expiring.example.com.crt"), crt)
	write(t, filepath.Join(dir, "expiring.example.com.key"), key)

	crt, _ = issue(t, "keyless.example.com", now.Add(60*24*time.Hour))
	write(t, filepath.Join(dir, "keyless.example.com.crt"), crt)

	imp, err := ImportCertificateDir(dir, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Imported) != 0 {
		t.Fatalf("imported %v", imp.Imported)
	}
	if !strings.Contains(imp.Skipped["expiring.example.com"], "too soon") {
		t.Fatalf("expiring: %q", imp.Skipped["expiring.example.com"])
	}
	if !strings.Contains(imp.Skipped["keyless.example.com"], "private key") {
		t.Fatalf("keyless: %q", imp.Skipped["keyless.example.com"])
	}
}

// Years of renewals leave several certificates for one host in a directory. The
// usable one wins, whichever order the walk finds them in.
func TestImportCertificateDirPrefersAUsableCopy(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	name := "shop.example.com"

	crt, key := issue(t, name, now.Add(2*24*time.Hour))
	write(t, filepath.Join(dir, "a-old.crt"), crt)
	write(t, filepath.Join(dir, "a-old.key"), key)

	crt, key = issue(t, name, now.Add(80*24*time.Hour))
	write(t, filepath.Join(dir, "z-new.crt"), crt)
	write(t, filepath.Join(dir, "z-new.key"), key)

	imp, err := ImportCertificateDir(dir, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := imported(imp); got != name {
		t.Fatalf("imported %q", got)
	}
	if reason, skipped := imp.Skipped[name]; skipped {
		t.Fatalf("still reported as skipped: %q", reason)
	}
}

// Anything that is not a certificate is walked past, including a directory that
// cannot be read.
func TestImportCertificateDirIgnoresWhatIsNotACertificate(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "README"), []byte("nothing here"))
	write(t, filepath.Join(dir, "dhparam.pem"), []byte("-----BEGIN DH PARAMETERS-----\n"))
	imp, err := ImportCertificateDir(dir, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Imported) != 0 || len(imp.Files) != 0 {
		t.Fatalf("got %+v", imp)
	}
}

func TestImportCertificateDirWithNoDirectoryIsNotAnError(t *testing.T) {
	imp, err := ImportCertificateDir("", nil, time.Now())
	if err != nil || len(imp.Imported) != 0 {
		t.Fatalf("%v %+v", err, imp)
	}
}
