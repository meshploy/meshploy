package edgeconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/meshploy/packages/hostagent"
)

// SecretsDir holds each DNS provider's token, one file per provider ID, under
// the install directory. It is inside caddy/, which Caddy mounts read-only at
// /etc/caddy, so the Caddyfile names a path and never a token: Caddy reads the
// file through a {file.*} placeholder each time it loads its configuration.
// Root only, like the tokens in it.
const SecretsDir = "caddy/secrets"

// caddySecretsDir is SecretsDir as Caddy sees it.
const caddySecretsDir = "/etc/caddy/secrets"

// credentialPlaceholder is what the Caddyfile holds in place of a provider's
// token.
func credentialPlaceholder(p *hostagent.EdgeDNSProvider) string {
	return "{file." + caddySecretsDir + "/" + p.ID + "}"
}

// providers returns the DNS providers a snapshot names, by ID.
func providers(snap hostagent.EdgeSnapshot) map[string]bool {
	out := map[string]bool{}
	for _, d := range snap.Domains {
		if d.DNSProvider != nil {
			out[d.DNSProvider.ID] = true
		}
	}
	return out
}

// InstallSecrets writes the token of every provider the snapshot names, from
// creds where it has one, and reports whether any file changed: Caddy reads
// the tokens only when it loads its configuration, so a changed one needs a
// reload even when the Caddyfile did not change.
//
// A provider with no token in creds keeps the file already here. One with
// neither is refused before anything is written: Caddy would read an empty
// token and fail every certificate for the domain.
func InstallSecrets(snap hostagent.EdgeSnapshot, creds hostagent.DNSCredentials, dir string) (bool, error) {
	secrets := filepath.Join(dir, SecretsDir)
	want := providers(snap)
	var missing []string
	for id := range want {
		if strings.TrimSpace(creds[id]) != "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(secrets, id)); err != nil {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return false, fmt.Errorf("no token for DNS provider %s: connect it again from the console", strings.Join(missing, ", "))
	}
	if len(want) == 0 {
		return false, nil
	}
	if err := os.MkdirAll(secrets, 0o700); err != nil {
		return false, err
	}
	if err := os.Chmod(secrets, 0o700); err != nil {
		return false, err
	}
	changed := false
	for id := range want {
		token := strings.TrimSpace(creds[id])
		if token == "" {
			continue
		}
		path := filepath.Join(secrets, id)
		if before, err := os.ReadFile(path); err == nil && string(before) == token {
			continue
		}
		if err := writeSecret(path, token); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// RemoveStaleSecrets deletes the tokens of providers the snapshot no longer
// names. Run only once the configuration that stopped naming them is serving:
// until then the previous one, which a failed reload puts back, still reads
// them.
func RemoveStaleSecrets(snap hostagent.EdgeSnapshot, dir string) error {
	entries, err := os.ReadDir(filepath.Join(dir, SecretsDir))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	want := providers(snap)
	var errs []error
	for _, e := range entries {
		if e.IsDir() || want[e.Name()] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, SecretsDir, e.Name())); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Redact replaces every token kept under the install directory, and every one
// in creds, with a marker. Caddy's DNS modules quote a token they reject in
// their error, and that error ends up in the apply's result, which the API and
// the console read.
func Redact(s string, creds hostagent.DNSCredentials, dir string) string {
	var tokens []string
	for _, t := range creds {
		tokens = append(tokens, strings.TrimSpace(t))
	}
	if entries, err := os.ReadDir(filepath.Join(dir, SecretsDir)); err == nil {
		for _, e := range entries {
			if b, err := os.ReadFile(filepath.Join(dir, SecretsDir, e.Name())); err == nil {
				tokens = append(tokens, strings.TrimSpace(string(b)))
			}
		}
	}
	for _, t := range tokens {
		if t != "" {
			s = strings.ReplaceAll(s, t, "[token]")
		}
	}
	return s
}

func writeSecret(path, token string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(token); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
