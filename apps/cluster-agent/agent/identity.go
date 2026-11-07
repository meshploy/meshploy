package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	appk8s "github.com/meshploy/packages/server/k8s"
)

// TokenPrefix marks the token the API presents to a cluster agent.
const TokenPrefix = "mcag-"

// Identity is what the agent is known by: its certificate, which the API
// pins, and the token the API presents. Both are made on the machine; the
// key never leaves it.
type Identity struct {
	Dir string
}

func (id Identity) KeyPath() string   { return filepath.Join(id.Dir, "tls.key") }
func (id Identity) CertPath() string  { return filepath.Join(id.Dir, "tls.crt") }
func (id Identity) TokenPath() string { return filepath.Join(id.Dir, "token") }

// Ensure makes the identity if any part of it is missing, and leaves a
// complete one as it is. A half-made identity (a crash between files) is made
// again whole, so the certificate always matches the key.
func (id Identity) Ensure() (created bool, err error) {
	have := 0
	for _, p := range []string{id.KeyPath(), id.CertPath(), id.TokenPath()} {
		if _, err := os.Stat(p); err == nil {
			have++
		}
	}
	if have == 3 {
		if _, err := id.TLSCertificate(); err != nil {
			return false, fmt.Errorf("the identity in %s is unreadable: %w", id.Dir, err)
		}
		return false, nil
	}
	if err := os.MkdirAll(id.Dir, 0o700); err != nil {
		return false, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return false, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	if err != nil {
		return false, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: appk8s.ClusterAgentServerName},
		DNSNames:              []string{appk8s.ClusterAgentServerName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // it is its own trust anchor
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return false, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return false, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return false, err
	}
	files := []struct {
		path string
		data []byte
	}{
		{id.KeyPath(), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})},
		{id.CertPath(), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})},
		{id.TokenPath(), []byte(TokenPrefix + hex.EncodeToString(raw) + "\n")},
	}
	for _, f := range files {
		if err := writeFile(f.path, f.data); err != nil {
			return false, err
		}
	}
	return true, nil
}

// writeFile writes through a temporary file, so a crash never leaves half a
// key, readable by root alone.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// TLSCertificate is the agent's serving certificate.
func (id Identity) TLSCertificate() (tls.Certificate, error) {
	return tls.LoadX509KeyPair(id.CertPath(), id.KeyPath())
}

// CertPEM is the certificate the API pins.
func (id Identity) CertPEM() ([]byte, error) { return os.ReadFile(id.CertPath()) }

// Token is what the API presents.
func (id Identity) Token() (string, error) {
	raw, err := os.ReadFile(id.TokenPath())
	if err != nil {
		return "", err
	}
	t := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(t, TokenPrefix) || len(t) != len(TokenPrefix)+64 {
		return "", errors.New("the token file does not hold a cluster agent token")
	}
	return t, nil
}

// TokenMatches compares a presented token with the agent's in constant time,
// by digest, so its length leaks nothing either.
func TokenMatches(presented, want string) bool {
	a := sha256.Sum256([]byte(presented))
	b := sha256.Sum256([]byte(want))
	return want != "" && subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
